// Matches the comment shape returned by GET /api/v1/incidents/comments
// (internal/api/chat_handler.go's HandleGetIncidentComments).
export interface IncidentComment {
  id: string;
  author: string;
  comment: string;
  created_at: string;
}

// Matches internal/api/incidents.go's Incident — the grouped-investigation view (Fase 3/3b).
export interface Incident {
  id: string;
  fingerprint: string;
  title: string;
  severity: string;
  status: string;
  risk_score: number;
  alert_count: number;
  first_seen: string;
  last_seen: string;
  created_at: string;
  resolved_at?: string;
}

// Matches internal/api/incidents.go's IncidentAlert (slim alert view within an incident).
export interface IncidentAlert {
  id: string;
  event_type: string;
  severity: string;
  status: string;
  summary: string;
  created_at: string;
}
