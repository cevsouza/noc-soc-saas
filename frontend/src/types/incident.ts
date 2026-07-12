// Matches the comment shape returned by GET /api/v1/incidents/comments
// (internal/api/chat_handler.go's HandleGetIncidentComments).
export interface IncidentComment {
  id: string;
  author: string;
  comment: string;
  created_at: string;
}
