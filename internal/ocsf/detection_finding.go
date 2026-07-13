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
	Unmapped     map[string]interface{} `json:"unmapped,omitempty"`
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

// FromAlert maps a platform alert to an OCSF Detection Finding. Pure and unit-tested.
func FromAlert(a *model.Alert) DetectionFinding {
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
		Unmapped: unmapped,
	}
}
