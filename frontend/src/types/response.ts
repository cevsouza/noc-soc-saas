// Mirrors internal/api/response_handler.go's ResponseActionResponse — one outbound containment
// request (block/unblock IP on a firewall, contain/lift a host on an EDR). These are created
// pending by an operator and only fire the vendor call on approval.
export interface ResponseAction {
  id: string;
  integration_type: string;
  action_type: string;
  target: string;
  incident_id?: string;
  status: 'pending' | 'approved' | 'failed' | 'rejected';
  reason: string;
  requested_by: string;
  output: string;
  created_at: string;
}
