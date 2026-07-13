package model

import "testing"

func TestTenantRoleHierarchy(t *testing.T) {
	// Ascending tenant privilege, with legacy aliases sharing ranks.
	if !TenantRoleAtLeast(RoleTenantAdmin, RoleAnalystL1) {
		t.Error("tenant_admin should outrank analyst_l1")
	}
	if !TenantRoleAtLeast(RoleAdmin, RoleOperator) {
		t.Error("legacy admin should outrank legacy operator")
	}
	if TenantRoleAtLeast(RoleReadOnly, RoleAnalystL2) {
		t.Error("read_only must NOT satisfy an analyst_l2 requirement")
	}
	if TenantRoleAtLeast(RoleAnalystL1, RoleAnalystL3) {
		t.Error("analyst_l1 must NOT satisfy an analyst_l3 requirement")
	}
	// Legacy aliases map onto the granular ranks.
	if TenantRoleRank(RoleViewer) != TenantRoleRank(RoleReadOnly) {
		t.Error("viewer and read_only must share a rank")
	}
	if TenantRoleRank(RoleOperator) != TenantRoleRank(RoleAnalystL2) {
		t.Error("operator and analyst_l2 must share a rank")
	}
}

func TestIsPlatformAdmin(t *testing.T) {
	// Only platform_admin and legacy admin get the all-tenants bypass.
	for _, r := range []UserRole{RolePlatformAdmin, RoleAdmin} {
		if !IsPlatformAdmin(r) {
			t.Errorf("%q should be a platform admin", r)
		}
	}
	// mssp_analyst is a multi-tenant analyst, NOT a platform admin (no implicit all-tenants).
	for _, r := range []UserRole{RoleMSSPAnalyst, RoleTenantAdmin, RoleAnalystL3, RoleOperator, RoleViewer, ""} {
		if IsPlatformAdmin(r) {
			t.Errorf("%q must NOT be treated as a platform admin", r)
		}
	}
}

func TestPlatformRoleHierarchy(t *testing.T) {
	// platform_admin outranks mssp_analyst; a tenant-only role has no platform privilege.
	if !PlatformRoleAtLeast(RolePlatformAdmin, RoleMSSPAnalyst) {
		t.Error("platform_admin should outrank mssp_analyst")
	}
	if PlatformRoleAtLeast(RoleMSSPAnalyst, RolePlatformAdmin) {
		t.Error("mssp_analyst must NOT satisfy a platform_admin requirement")
	}
	if PlatformRoleRank(RoleTenantAdmin) != 0 {
		t.Error("tenant_admin must carry no platform privilege")
	}
}

func TestIsValidTenantRole(t *testing.T) {
	valid := []UserRole{RoleReadOnly, RoleAnalystL1, RoleAnalystL2, RoleAnalystL3, RoleTenantAdmin, RoleAdmin, RoleOperator, RoleViewer}
	for _, r := range valid {
		if !IsValidTenantRole(r) {
			t.Errorf("%q should be a valid tenant role", r)
		}
	}
	// Platform-only roles and garbage are not assignable as tenant roles.
	for _, r := range []UserRole{RolePlatformAdmin, RoleMSSPAnalyst, "root", ""} {
		if IsValidTenantRole(r) {
			t.Errorf("%q must NOT be a valid tenant role", r)
		}
	}
}
