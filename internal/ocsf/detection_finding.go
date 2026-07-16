// Package ocsf maps the platform's internal alerts to the Open Cybersecurity Schema Framework
// (OCSF) so findings can be exported to OCSF-native consumers (security data lakes, SIEMs, XDR).
//
// The platform already normalizes every vendor payload to an internal common model
// (model.UnifiedIncident -> model.Alert). OCSF is an ADDITIONAL, industry-standard representation
// layered on top for interoperability — it does not replace the internal model. This package
// implements the OCSF "Detection Finding" class (class_uid 2004, category Findings), the modern
// class for security detections/alerts, at schema version 1.1.0. Mapping is pure and unit-tested.
package ocsf

import (
	"time"

	"noc-api/internal/model"
)

const (
	CategoryUID   = 2      // Findings
	ClassUID      = 2004   // Detection Finding
	SchemaVersion = "1.1.0"
	ProductName   = "NOC/SOC SaaS"
	VendorName    = "noc-soc-saas"
)

// Product identifies the emitting product in OCSF metadata.
type Product struct {
	Name       string `json:"name"`
	VendorName string `json:"vendor_name"`
}

// Metadata is the OCSF metadata object (schema version + product).
type Metadata struct {
	Version string  `json:"version"`
	Product Product `json:"product"`
}

// FindingInfo is the OCSF finding_info object describing the finding itself.
type FindingInfo struct {
	UID         string   `json:"uid"`
	Title       string   `json:"title"`
	Desc        string   `json:"desc,omitempty"`
	CreatedTime int64    `json:"created_time"`
	Types       []string `json:"types,omitempty"`
}

// Observable is an OCSF observable — an IOC extracted from the finding (source IP, domain, file
// hash). Observables let OCSF-native consumers pivot/correlate on the raw indicators. type_id follows
// the OCSF Observable Type ID enum (1 Hostname, 2 IP Address, 8 Hash).
type Observable struct {
	Name   string `json:"name"`
	Type   string `json:"type"`
	TypeID int    `json:"type_id"`
	Value  string `json:"value"`
}

// Device is a minimal OCSF device object describing the asset the finding affects, populated from the
// platform's CMDB/topology when the alerting host resolves to a managed/discovered asset. risk_level
// mirrors the asset's business criticality.
type Device struct {
	Hostname    string `json:"hostname,omitempty"`
	IP          string `json:"ip,omitempty"`
	Name        string `json:"name,omitempty"`
	Type        string `json:"type,omitempty"`
	RiskLevel   string `json:"risk_level,omitempty"`
	RiskLevelID int    `json:"risk_level_id,omitempty"`
}

// DetectionFinding is an OCSF Detection Finding (class_uid 2004) event.
type DetectionFinding struct {
	ActivityID   int                    `json:"activity_id"`
	ActivityName string                 `json:"activity_name"`
	CategoryUID  int                    `json:"category_uid"`
	CategoryName string                 `json:"category_name"`
	ClassUID     int                    `json:"class_uid"`
	ClassName    string                 `json:"class_name"`
	TypeUID      int                    `json:"type_uid"`
	Time         int64                  `json:"time"`
	SeverityID   int                    `json:"severity_id"`
	Severity     string                 `json:"severity"`
	StatusID     int                    `json:"status_id"`
	Status       string                 `json:"status"`
	Message      string                 `json:"message"`
	Metadata     Metadata               `json:"metadata"`
	FindingInfo  FindingInfo            `json:"finding_info"`
	Observables  []Observable           `json:"observables,omitempty"`
	Device       *Device                `json:"device,omitempty"`
	Unmapped     map[string]interface{} `json:"unmapped,omitempty"`
}

// Enrichment carries the per-tenant context (Backlog B3) layered onto a base finding: IOCs extracted
// from the event as OCSF observables, the affected CMDB asset as a device object with its business
// criticality, and cross-tenant threat-intel corroboration. Every field is optional — a zero
// Enrichment yields exactly the base finding (this is what the plain FromAlert produces). Geo
// enrichment is intentionally left out: it needs an external GeoIP database not present in-platform.
type Enrichment struct {
	Observables      []Observable
	Device           *Device
	AssetCriticality string // assets.business_criticality: low/medium/high/critical
	FleetSightings   int    // number of OTHER opted-in tenants that have seen this event's indicators
}

// observableTypeIDs maps the platform's internal indicator type strings (see internal/threatintel)
// to the OCSF Observable Type ID + label. Kept here so the OCSF mapping stays a pure leaf package
// (no threatintel import); callers translate their indicators through ObservableFromIndicator.
var observableTypeIDs = map[string]struct {
	id    int
	label string
}{
	"ip":     {2, "IP Address"},
	"domain": {1, "Hostname"},
	"hash":   {8, "Hash"},
}

// ObservableFromIndicator maps an internal (type, value) indicator to an OCSF Observable. Unknown
// types fall back to type_id 0 (Unknown) so nothing is silently dropped. Pure and unit-tested.
func ObservableFromIndicator(indType, value string) Observable {
	m := observableTypeIDs[indType]
	label := m.label
	if label == "" {
		label = "Unknown"
	}
	return Observable{Name: indType, Type: label, TypeID: m.id, Value: value}
}

// criticalityRiskLevel maps an asset's business criticality to the OCSF risk_level_id + label
// (0 Info, 1 Low, 2 Medium, 3 High, 4 Critical). Pure.
var criticalityRiskLevel = map[string]struct {
	id    int
	label string
}{
	"low":      {1, "Low"},
	"medium":   {2, "Medium"},
	"high":     {3, "High"},
	"critical": {4, "Critical"},
}

// RiskLevelForCriticality returns the OCSF risk_level_id/label for a business criticality (defaults to
// Info/0 when unknown or empty). Pure and unit-tested.
func RiskLevelForCriticality(criticality string) (int, string) {
	if m, ok := criticalityRiskLevel[criticality]; ok {
		return m.id, m.label
	}
	return 0, "Info"
}

// severityMap translates the platform's severities to OCSF severity_id + label. OCSF severity_id:
// 0 Unknown, 1 Informational, 2 Low, 3 Medium, 4 High, 5 Critical, 6 Fatal.
var severityMap = map[model.AlertSeverity]struct {
	id    int
	label string
}{
	model.SeverityInfo:     {1, "Informational"},
	model.SeverityWarning:  {3, "Medium"},
	model.SeverityCritical: {5, "Critical"},
	model.SeverityFatal:    {6, "Fatal"},
}

// statusMap translates the platform's alert status to OCSF finding status_id + label. OCSF finding
// status_id: 0 Unknown, 1 New, 2 In Progress, 3 Suppressed, 4 Resolved.
var statusMap = map[model.AlertStatus]struct {
	id    int
	label string
}{
	model.AlertTriggered:    {1, "New"},
	model.AlertAcknowledged: {2, "In Progress"},
	model.AlertResolved:     {4, "Resolved"},
	model.AlertSuppressed:   {3, "Suppressed"},
}

// activityFor derives the OCSF activity from the alert status: a new alert is a Create, an
// acknowledged/suppressed one an Update, a resolved one a Close. OCSF Detection Finding activity_id:
// 1 Create, 2 Update, 3 Close.
func activityFor(status model.AlertStatus) (int, string) {
	switch status {
	case model.AlertResolved:
		return 3, "Close"
	case model.AlertAcknowledged, model.AlertSuppressed:
		return 2, "Update"
	default:
		return 1, "Create"
	}
}

// FromAlert maps a platform alert to a base OCSF Detection Finding (no per-tenant enrichment). Pure
// and unit-tested. Equivalent to FromAlertEnriched with a zero Enrichment.
func FromAlert(a *model.Alert) DetectionFinding {
	return FromAlertEnriched(a, Enrichment{})
}

// FromAlertEnriched maps a platform alert to an OCSF Detection Finding, layering the given per-tenant
// Enrichment (observables, device, asset criticality, fleet corroboration) onto the base mapping.
// Pure and unit-tested.
func FromAlertEnriched(a *model.Alert, enr Enrichment) DetectionFinding {
	sev := severityMap[a.Severity]
	if sev.id == 0 {
		sev = struct {
			id    int
			label string
		}{0, "Unknown"}
	}
	st := statusMap[a.Status]
	if st.id == 0 {
		st = struct {
			id    int
			label string
		}{0, "Unknown"}
	}
	activityID, activityName := activityFor(a.Status)

	unmapped := map[string]interface{}{
		"tenant_id":  a.TenantID.String(),
		"event_type": a.EventType,
	}
	if a.Fingerprint != "" {
		unmapped["fingerprint"] = a.Fingerprint
	}
	if a.IncidentID != nil {
		unmapped["incident_id"] = a.IncidentID.String()
	}
	if a.MitreTactics != nil && *a.MitreTactics != "" {
		unmapped["mitre_tactics"] = *a.MitreTactics
	}
	// B3 per-tenant enrichment (all optional; absent when the base FromAlert path is used).
	if enr.AssetCriticality != "" {
		unmapped["asset_criticality"] = enr.AssetCriticality
	}
	if enr.FleetSightings > 0 {
		unmapped["fleet_tenant_sightings"] = enr.FleetSightings
	}

	createdMS := a.CreatedAt.UnixMilli()
	if a.CreatedAt.IsZero() {
		createdMS = time.Now().UnixMilli()
	}

	var types []string
	if a.EventType != "" {
		types = []string{a.EventType}
	}

	return DetectionFinding{
		ActivityID:   activityID,
		ActivityName: activityName,
		CategoryUID:  CategoryUID,
		CategoryName: "Findings",
		ClassUID:     ClassUID,
		ClassName:    "Detection Finding",
		TypeUID:      ClassUID*100 + activityID,
		Time:         createdMS,
		SeverityID:   sev.id,
		Severity:     sev.label,
		StatusID:     st.id,
		Status:       st.label,
		Message:      a.Summary,
		Metadata: Metadata{
			Version: SchemaVersion,
			Product: Product{Name: ProductName, VendorName: VendorName},
		},
		FindingInfo: FindingInfo{
			UID:         a.ID.String(),
			Title:       a.Summary,
			CreatedTime: createdMS,
			Types:       types,
		},
		Observables: enr.Observables,
		Device:      enr.Device,
		Unmapped:    unmapped,
	}
}
