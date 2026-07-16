package ocsfsink

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"noc-api/internal/model"
	"noc-api/internal/ocsf"

	"github.com/google/uuid"
)

func sampleFinding() ocsf.DetectionFinding {
	a := &model.Alert{ID: uuid.New(), TenantID: uuid.New(), Severity: model.SeverityCritical, Status: model.AlertTriggered, Summary: "test"}
	return ocsf.FromAlert(a)
}

func TestEmitPostsFindingJSON(t *testing.T) {
	var gotBody map[string]interface{}
	var gotContentType, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotContentType = r.Header.Get("Content-Type")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	if err := NewEmitter().Emit(context.Background(), srv.URL, sampleFinding()); err != nil {
		t.Fatalf("Emit returned error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotContentType != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotContentType)
	}
	if v, _ := gotBody["class_uid"].(float64); int(v) != ocsf.ClassUID {
		t.Errorf("posted body class_uid = %v, want %d", gotBody["class_uid"], ocsf.ClassUID)
	}
}

func TestEmitNon2xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	if err := NewEmitter().Emit(context.Background(), srv.URL, sampleFinding()); err == nil {
		t.Fatal("expected an error on a non-2xx sink response, got nil")
	}
}

func TestEmitTransportErrorIsError(t *testing.T) {
	// An unreachable URL must surface as an error, not a silent success.
	if err := NewEmitter().Emit(context.Background(), "http://127.0.0.1:0", sampleFinding()); err == nil {
		t.Fatal("expected a transport error for an unreachable sink, got nil")
	}
}
