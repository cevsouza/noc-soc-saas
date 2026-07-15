// On-call scheduling (B5 slice 1). Mirrors internal/api/oncall.go's OncallSchedule / OncallShift.

export interface OncallSchedule {
  id: string;
  name: string;
  created_at: string;
  // The user covering NOW() (absent if nobody is on-call at this moment).
  oncall_user_id?: string;
  oncall_name?: string;
  oncall_email?: string;
  oncall_until?: string;
}

export interface OncallShift {
  id: string;
  user_id: string;
  user_name: string;
  user_email: string;
  starts_at: string;
  ends_at: string;
}
