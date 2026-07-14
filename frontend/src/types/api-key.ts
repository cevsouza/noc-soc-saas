// Self-service ingestion API keys. Mirrors internal/api/api_keys.go.
// The plaintext `api_key` is present only in the create response, never when listing.

export interface ApiKeyInfo {
  id: string;
  name: string;
  created_at: string;
  expires_at?: string;
}

export interface CreatedApiKey extends ApiKeyInfo {
  api_key: string;
}
