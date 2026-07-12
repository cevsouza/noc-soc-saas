// Matches internal/api/integration_handler.go's IntegrationResponse.
export interface Integration {
  id: string;
  tenant_id: string;
  name: string;
  type: string;
  status: 'active' | 'inactive';
  settings: Record<string, unknown>;
  created_at: string;
}

export interface ConnectorStatus {
  status: 'active' | 'offline' | 'inactive';
  last_seen: number;
  last_error: string;
  has_error: boolean;
}
