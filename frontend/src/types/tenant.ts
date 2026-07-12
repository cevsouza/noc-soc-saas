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
