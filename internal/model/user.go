package model

import (
	"time"

	"github.com/google/uuid"
)

type UserRole string

const (
	// Legacy roles (kept as backward-compatible aliases in the rank tables below).
	RoleAdmin    UserRole = "admin"
	RoleOperator UserRole = "operator"
	RoleViewer   UserRole = "viewer"

	// Granular tenant-scoped roles (Fase 4b), ascending privilege.
	RoleReadOnly    UserRole = "read_only"
	RoleAnalystL1   UserRole = "analyst_l1"
	RoleAnalystL2   UserRole = "analyst_l2"
	RoleAnalystL3   UserRole = "analyst_l3"
	RoleTenantAdmin UserRole = "tenant_admin"

	// Platform-scoped roles (Fase 4b). mssp_analyst is a multi-tenant analyst that can act ONLY
	// on tenants it is an explicit member of (never implicit all-tenants); platform_admin is the
	// only role granted the all-tenants bypass.
	RoleMSSPAnalyst   UserRole = "mssp_analyst"
	RolePlatformAdmin UserRole = "platform_admin"
)

// RBAC is intentionally two-dimensional: a role's tenant-scope privilege and its platform-scope
// privilege are ranked independently, which is what lets the legacy string "admin" mean
// "tenant admin" (rank 50) when it appears in tenant_users.role and "platform admin" (rank 100)
// when it appears in users.global_role, without one leaking into the other. A rank of 0 means the
// role carries no privilege in that dimension.
var tenantRoleRank = map[UserRole]int{
	RoleReadOnly: 10, RoleViewer: 10,
	RoleAnalystL1: 20,
	RoleAnalystL2: 30, RoleOperator: 30,
	RoleAnalystL3:   40,
	RoleTenantAdmin: 50, RoleAdmin: 50,
}

var platformRoleRank = map[UserRole]int{
	RoleMSSPAnalyst:   60,
	RolePlatformAdmin: 100, RoleAdmin: 100,
}

// TenantRoleRank returns the tenant-scope privilege rank of a role (0 if it is not a tenant role).
func TenantRoleRank(r UserRole) int { return tenantRoleRank[r] }

// PlatformRoleRank returns the platform-scope privilege rank of a role (0 if none).
func PlatformRoleRank(r UserRole) int { return platformRoleRank[r] }

// TenantRoleAtLeast reports whether `have` meets or exceeds the tenant privilege of `min`.
func TenantRoleAtLeast(have, min UserRole) bool { return tenantRoleRank[have] >= tenantRoleRank[min] }

// PlatformRoleAtLeast reports whether `have` meets or exceeds the platform privilege of `min`.
func PlatformRoleAtLeast(have, min UserRole) bool { return platformRoleRank[have] >= platformRoleRank[min] }

// IsPlatformAdmin reports whether the platform role grants the all-tenants bypass. Only the legacy
// "admin" and the new "platform_admin" qualify — mssp_analyst deliberately does NOT.
func IsPlatformAdmin(r UserRole) bool { return platformRoleRank[r] >= 100 }

// IsValidTenantRole reports whether a role is assignable as a tenant-scoped role.
func IsValidTenantRole(r UserRole) bool {
	switch r {
	case RoleReadOnly, RoleAnalystL1, RoleAnalystL2, RoleAnalystL3, RoleTenantAdmin,
		RoleAdmin, RoleOperator, RoleViewer:
		return true
	}
	return false
}

type User struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type TenantUser struct {
	TenantID  uuid.UUID `json:"tenant_id"`
	UserID    uuid.UUID `json:"user_id"`
	Role      UserRole  `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}
