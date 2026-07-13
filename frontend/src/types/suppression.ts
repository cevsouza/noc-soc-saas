// Mirrors internal/api/suppression.go's SuppressionRule (Fase 3/3d).
export interface SuppressionRule {
  id: string;
  name: string;
  match_field: string;
  match_value: string;
  starts_at?: string;
  ends_at?: string;
  active: boolean;
  created_at: string;
}
