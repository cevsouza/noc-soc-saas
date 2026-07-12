// Matches internal/api/vault_list_handler.go's VaultSecretMetadata — never the decrypted
// value itself, only metadata (the backend never returns plaintext secrets after creation).
export interface VaultSecret {
  id: string;
  secret_key: string;
  description: string;
  created_at: string;
  updated_at: string;
}
