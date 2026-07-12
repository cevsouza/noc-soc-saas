package api

import (
	"testing"

	"github.com/google/uuid"
)

func TestParseGrantAccessRequestValid(t *testing.T) {
	userID := uuid.New()
	tenantID := uuid.New()

	gotUser, gotTenant, err := parseGrantAccessRequest(GrantAccessRequest{
		UserID:   userID.String(),
		TenantID: tenantID.String(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotUser != userID {
		t.Errorf("expected user %v, got %v", userID, gotUser)
	}
	if gotTenant != tenantID {
		t.Errorf("expected tenant %v, got %v", tenantID, gotTenant)
	}
}

func TestParseGrantAccessRequestInvalidUser(t *testing.T) {
	_, _, err := parseGrantAccessRequest(GrantAccessRequest{
		UserID:   "not-a-uuid",
		TenantID: uuid.New().String(),
	})
	if err == nil {
		t.Fatal("expected error for invalid user_id, got nil")
	}
}

func TestParseGrantAccessRequestInvalidTenant(t *testing.T) {
	_, _, err := parseGrantAccessRequest(GrantAccessRequest{
		UserID:   uuid.New().String(),
		TenantID: "",
	})
	if err == nil {
		t.Fatal("expected error for empty tenant_id, got nil")
	}
}

func TestParseTenantIDListDeduplicatesAndSkipsBlanks(t *testing.T) {
	a := uuid.New()
	b := uuid.New()
	got, err := parseTenantIDList([]string{a.String(), "  ", b.String(), a.String()})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 unique tenant IDs, got %d (%v)", len(got), got)
	}
	if got[0] != a || got[1] != b {
		t.Errorf("expected [%v %v] preserving order, got %v", a, b, got)
	}
}

func TestParseTenantIDListEmpty(t *testing.T) {
	got, err := parseTenantIDList(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

func TestParseTenantIDListRejectsInvalid(t *testing.T) {
	if _, err := parseTenantIDList([]string{uuid.New().String(), "not-a-uuid"}); err == nil {
		t.Fatal("expected error for invalid tenant_id, got nil")
	}
}

// grantedTenantRole must stay 'operator' — this slice deliberately never grants admin/viewer
// through the access endpoint. A change here is a real product decision, so pin it.
func TestGrantedTenantRoleIsOperator(t *testing.T) {
	if grantedTenantRole != "operator" {
		t.Errorf("grantedTenantRole must be 'operator', got %q", grantedTenantRole)
	}
}
