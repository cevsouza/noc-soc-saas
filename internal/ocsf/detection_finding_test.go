package ocsf

import (
	"encoding/json"
	"testing"
	"time"

	"noc-api/internal/model"

	"github.com/google/uuid"
)

func TestFromAlertCoreMapping(t *testing.T) {
	created := time.Date(2026, 7, 13, 12, 0, 0, 0, time.UTC)
	incID := uuid.New()
	mitre := "TA0001"
	a := &model.Alert{
		ID:           uuid.New(),
		TenantID:     uuid.New(),
		EventType:    "cpu_high",
		Severity:     model.SeverityCritical,
		Status:       model.AlertTriggered,
		Summary:      "CPU acima de 95%",
		CreatedAt:    created,
		Fingerprint:  "abc123",
		IncidentID:   &incID,
		MitreTactics: &mitre,
	}
	f := FromAlert(a)

	if f.ClassUID != 2004 || f.CategoryUID != 2 {
		t.Fatalf("class/category = %d/%d, want 2004/2", f.ClassUID, f.CategoryUID)
	}
	if f.ActivityID != 1 || f.TypeUID != 2004*100+1 {
		t.Fatalf("activity/type = %d/%d, want 1/%d", f.ActivityID, f.TypeUID, 2004*100+1)
	}
	if f.SeverityID != 5 || f.Severity != "Critical" {
		t.Fatalf("severity = %d/%q, want 5/Critical", f.SeverityID, f.Severity)
	}
	if f.StatusID != 1 || f.Status != "New" {
		t.Fatalf("status = %d/%q, want 1/New", f.StatusID, f.Status)
	}
	if f.Time != created.UnixMilli() || f.FindingInfo.CreatedTime != created.UnixMilli() {
		t.Fatalf("time = %d, want %d", f.Time, created.UnixMilli())
	}
	if f.FindingInfo.UID != a.ID.String() || f.Message != "CPU acima de 95%" {
		t.Fatalf("finding_info.uid/message mismatch")
	}
	if f.Metadata.Version != "1.1.0" {
		t.Fatalf("metadata.version = %q", f.Metadata.Version)
	}
	if f.Unmapped["fingerprint"] != "abc123" || f.Unmapped["incident_id"] != incID.String() || f.Unmapped["mitre_tactics"] != "TA0001" {
		t.Fatalf("unmapped extras missing: %+v", f.Unmapped)
	}

	// Must serialize to valid JSON with the OCSF discriminators present.
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var round map[string]interface{}
	_ = json.Unmarshal(b, &round)
	for _, k := range []string{"class_uid", "category_uid", "type_uid", "activity_id", "severity_id", "status_id", "time", "metadata", "finding_info"} {
		if _, ok := round[k]; !ok {
			t.Fatalf("serialized OCSF finding missing field %q", k)
		}
	}
}

func TestFromAlertStatusAndActivity(t *testing.T) {
	cases := []struct {
		status       model.AlertStatus
		wantStatusID int
		wantActivity int
	}{
		{model.AlertTriggered, 1, 1},
		{model.AlertAcknowledged, 2, 2},
		{model.AlertResolved, 4, 3},
		{model.AlertSuppressed, 3, 2},
	}
	for _, c := range cases {
		a := &model.Alert{ID: uuid.New(), TenantID: uuid.New(), Severity: model.SeverityInfo, Status: c.status, CreatedAt: time.Now()}
		f := FromAlert(a)
		if f.StatusID != c.wantStatusID || f.ActivityID != c.wantActivity {
			t.Fatalf("status %q -> status_id=%d activity=%d, want %d/%d", c.status, f.StatusID, f.ActivityID, c.wantStatusID, c.wantActivity)
		}
	}
}

func TestFromAlertUnknownSeverity(t *testing.T) {
	a := &model.Alert{ID: uuid.New(), TenantID: uuid.New(), Severity: model.AlertSeverity("weird"), Status: model.AlertStatus("weird"), CreatedAt: time.Now()}
	f := FromAlert(a)
	if f.SeverityID != 0 || f.Severity != "Unknown" || f.StatusID != 0 || f.Status != "Unknown" {
		t.Fatalf("unknown mapping = sev %d/%q status %d/%q", f.SeverityID, f.Severity, f.StatusID, f.Status)
	}
}
