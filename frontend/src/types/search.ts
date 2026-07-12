export interface SearchAlertResult {
  id: string;
  summary: string;
  severity: string;
  tenant_id: string;
  created_at: string;
}

export interface SearchRunbookResult {
  id: string;
  name: string;
  tenant_id: string;
  is_global: boolean;
}

export interface SearchTenantResult {
  id: string;
  name: string;
}

// Matches internal/api/search_handler.go's GlobalSearchResponse.
export interface GlobalSearchResponse {
  alerts: SearchAlertResult[];
  runbooks: SearchRunbookResult[];
  tenants: SearchTenantResult[];
}
