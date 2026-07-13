package audit

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestMarshalArgs(t *testing.T) {
	// nil details -> empty JSON object, no IP -> nil pointer.
	dj, ip, err := marshalArgs(Entry{TenantID: uuid.New(), UserID: uuid.New(), Action: "a", Resource: "r"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if string(dj) != "{}" {
		t.Errorf("nil details = %q, want {}", string(dj))
	}
	if ip != nil {
		t.Errorf("empty IP should marshal to nil pointer, got %v", *ip)
	}

	// populated details round-trips; IP becomes a non-nil pointer.
	dj, ip, err = marshalArgs(Entry{Details: map[string]interface{}{"k": "v"}, IPAddress: "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var back map[string]interface{}
	if err := json.Unmarshal(dj, &back); err != nil {
		t.Fatalf("details not valid JSON: %v", err)
	}
	if back["k"] != "v" {
		t.Errorf("details round-trip failed: %v", back)
	}
	if ip == nil || *ip != "10.0.0.1" {
		t.Errorf("IP pointer = %v, want 10.0.0.1", ip)
	}
}
