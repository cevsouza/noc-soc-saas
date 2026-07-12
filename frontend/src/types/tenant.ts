// Matches internal/api/tenant_handler.go's TenantResponse plus the logo/color fields the
// frontend already reads from the same tenant record (tenants table has logo_url/primary_color
// columns, set via HandleUpdateTenantStyle).
export interface Tenant {
  id: string;
  name: string;
  slug: string;
  logo_url?: string;
  primary_color?: string;
}

// One tenant a user is authorized on. Matches internal/api/access_handler.go's
// TenantAccessGrant (GET /api/v1/admin/access?user_id=). role is the user's role in that
// tenant (always 'operator' for grants created through the admin access screen).
export interface TenantAccessGrant {
  tenant_id: string;
  tenant_name: string;
  role: string;
}
