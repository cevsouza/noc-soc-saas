// Matches internal/ws/hub.go's ActiveUserDTO (GET /api/v1/ws/active_users).
export interface ActiveUser {
  session_id: string;
  user_id: string;
  email: string;
  name: string;
  role: string;
  connected_at: string;
}
