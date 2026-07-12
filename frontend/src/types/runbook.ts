// Matches internal/api/runbook_handler.go's RunbookResponse.
export interface Runbook {
  id: string;
  tenant_id: string;
  tenant_name: string;
  name: string;
  trigger_rule: string;
  script: string;
  vault_key_host: string;
  is_global: boolean;
  is_safe: boolean;
  created_at: string;
}

// Matches internal/api/runbook_handler.go's RunbookAuditResponse.
export interface RunbookAudit {
  id: string;
  runbook_name: string;
  operator_name: string;
  script: string;
  output: string;
  status: string;
  created_at: string;
}

// Matches internal/api/runbook_handler.go's RunbookApprovalResponse.
export interface RunbookApproval {
  id: string;
  runbook_id: string;
  runbook_name: string;
  incident_id: string;
  reason: string;
  status: 'pending' | 'approved' | 'rejected';
  requested_by: string;
  created_at: string;
}
