package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/google/uuid"
	"golang.org/x/crypto/hkdf"
)

// tenantKeyInfo is the HKDF "info" label binding derived keys to this application/purpose, so
// the same master key used elsewhere would never produce the same derived key.
const tenantKeyInfo = "noc-vault-tenant-key-v1"

// GetMasterKey retrieves the 32-byte master key from the environment.
// For SRE environments, this must be securely injected via configuration providers.
func GetMasterKey() ([]byte, error) {
	keyStr := os.Getenv("VAULT_MASTER_KEY")
	if keyStr == "" {
		return nil, errors.New("environment variable VAULT_MASTER_KEY is not set")
	}

	key := []byte(keyStr)
	if len(key) != 32 {
		return nil, fmt.Errorf("VAULT_MASTER_KEY must be exactly 32 bytes (256 bits), current length: %d", len(key))
	}

	return key, nil
}

// DeriveTenantKey derives a 32-byte AES-256 key unique to a tenant from the global master key
// using HKDF-SHA256, with the tenant's UUID as the salt. A leak of one tenant's derived key
// therefore does not expose any other tenant's secrets, while the master key stays the single
// root of trust that never encrypts ciphertext directly.
func DeriveTenantKey(masterKey []byte, tenantID uuid.UUID) ([]byte, error) {
	if len(masterKey) != 32 {
		return nil, errors.New("master key must be exactly 32 bytes")
	}
	salt := tenantID[:] // 16 bytes of the UUID
	reader := hkdf.New(sha256.New, masterKey, salt, []byte(tenantKeyInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(reader, key); err != nil {
		return nil, fmt.Errorf("failed to derive tenant key: %w", err)
	}
	return key, nil
}

// EncryptForTenant encrypts a tenant secret with that tenant's derived key. This is the current
// scheme for all newly-written vault secrets.
func EncryptForTenant(plainText []byte, masterKey []byte, tenantID uuid.UUID) ([]byte, []byte, error) {
	tenantKey, err := DeriveTenantKey(masterKey, tenantID)
	if err != nil {
		return nil, nil, err
	}
	return Encrypt(plainText, tenantKey)
}

// DecryptForTenant decrypts a tenant secret, transparently supporting BOTH the current
// per-tenant-key scheme and legacy secrets encrypted directly with the raw master key. It tries
// the tenant-derived key first; because AES-GCM authenticates, a wrong key reliably fails, so
// falling back to the raw master key cleanly reads secrets written before per-tenant derivation
// existed — no migration, no schema change, no downtime. A legacy secret migrates to the new
// scheme automatically the next time it is re-encrypted.
func DecryptForTenant(cipherText []byte, nonce []byte, masterKey []byte, tenantID uuid.UUID) ([]byte, error) {
	if tenantKey, err := DeriveTenantKey(masterKey, tenantID); err == nil {
		if plain, derr := Decrypt(cipherText, nonce, tenantKey); derr == nil {
			return plain, nil
		}
	}
	// Legacy fallback: the secret predates per-tenant derivation (encrypted with the raw master key).
	return Decrypt(cipherText, nonce, masterKey)
}

// Encrypt encrypts plain text using AES-256-GCM and returns ciphertext and the generated 12-byte nonce.
func Encrypt(plainText []byte, key []byte) ([]byte, []byte, error) {
	if len(key) != 32 {
		return nil, nil, errors.New("key must be exactly 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cipher block: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	// 12-byte nonce is standard for AES-GCM
	nonce := make([]byte, aesGCM.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("failed to generate random nonce: %w", err)
	}

	cipherText := aesGCM.Seal(nil, nonce, plainText, nil)
	return cipherText, nonce, nil
}

// Decrypt decrypts AES-256-GCM encrypted ciphertext using the provided key and nonce.
func Decrypt(cipherText []byte, nonce []byte, key []byte) ([]byte, error) {
	if len(key) != 32 {
		return nil, errors.New("key must be exactly 32 bytes")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher block: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plainText, err := aesGCM.Open(nil, nonce, cipherText, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt ciphertext (possibly invalid key or corrupted data): %w", err)
	}

	return plainText, nil
}
