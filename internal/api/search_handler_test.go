package api

import (
	"testing"

	"noc-api/internal/middleware"
	"noc-api/internal/model"

	"github.com/google/uuid"
)

func TestResolveSearchTenantIDsDefaultsToOwnTenant(t *testing.T) {
	own := uuid.New()
	claims := &middleware.JWTClaims{TenantID: own, GlobalRole: model.RoleOperator}

	got := resolveSearchTenantIDs(claims, "", func(uuid.UUID) bool { t.Fatal("isMember should not be called"); return false })

	if len(got) != 1 || got[0] != own {
		t.Errorf("expected [own tenant], got %v", got)
	}
}

func TestResolveSearchTenantIDsOwnTenantAlwaysAccepted(t *testing.T) {
	own := uuid.New()
	claims := &middleware.JWTClaims{TenantID: own, GlobalRole: model.RoleOperator}

	got := resolveSearchTenantIDs(claims, own.String(), func(uuid.UUID) bool { t.Fatal("isMember should not be called for own tenant"); return false })

	if len(got) != 1 || got[0] != own {
		t.Errorf("expected [own tenant], got %v", got)
	}
}

func TestResolveSearchTenantIDsGlobalAdminBypassesMembership(t *testing.T) {
	own := uuid.New()
	other := uuid.New()
	claims := &middleware.JWTClaims{TenantID: own, GlobalRole: model.RoleAdmin}

	got := resolveSearchTenantIDs(claims, other.String(), func(uuid.UUID) bool { t.Fatal("isMember should not be called for a global admin"); return false })

	if len(got) != 1 || got[0] != other {
		t.Errorf("expected [other tenant] via admin bypass, got %v", got)
	}
}

func TestResolveSearchTenantIDsUnauthorizedIsSilentlyDropped(t *testing.T) {
	own := uuid.New()
	authorized := uuid.New()
	unauthorized := uuid.New()
	claims := &middleware.JWTClaims{TenantID: own, GlobalRole: model.RoleOperator}

	got := resolveSearchTenantIDs(claims, authorized.String()+","+unauthorized.String(), func(id uuid.UUID) bool {
		return id == authorized
	})

	if len(got) != 1 || got[0] != authorized {
		t.Errorf("expected only the authorized tenant to survive, got %v", got)
	}
}

func TestResolveSearchTenantIDsAllUnauthorizedYieldsEmpty(t *testing.T) {
	own := uuid.New()
	unauthorized := uuid.New()
	claims := &middleware.JWTClaims{TenantID: own, GlobalRole: model.RoleOperator}

	got := resolveSearchTenantIDs(claims, unauthorized.String(), func(uuid.UUID) bool { return false })

	if len(got) != 0 {
		t.Errorf("expected empty result when no requested tenant is authorized, got %v", got)
	}
}
