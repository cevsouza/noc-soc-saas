export type UserRole = 'admin' | 'operator' | 'viewer';

// The logged-in user, as stored in the JWT/localStorage session — role here is tenant-scoped
// (claims.Role on the backend), not the platform-wide claims.GlobalRole.
export interface SessionUser {
  id: string;
  email: string;
  name: string;
  role: UserRole;
  // Platform-wide (MSSP) role, distinct from the tenant-scoped `role`. Set from the login response;
  // used to gate control-plane surfaces (the MSSP usage dashboard) to platform admins. Optional so
  // sessions persisted before this field existed still typecheck (they re-login to populate it).
  global_role?: UserRole;
  // Landing-console preference (B9): where the user arrives after login. Convenience only, not an
  // access restriction. Optional for backward-compatible persisted sessions.
  default_console?: 'all' | 'noc' | 'soc';
}

// Matches internal/api/auth_handler.go's UserListResponse (GET /api/v1/admin/users) — a
// distinct shape from SessionUser: global_role instead of tenant-scoped role, plus
// verification status. Kept as a separate interface rather than merged with SessionUser,
// since the two shapes genuinely differ and forcing one would be lossy.
export interface AdminUser {
  id: string;
  email: string;
  name: string;
  global_role: UserRole;
  is_verified: boolean;
  created_at: string;
}
