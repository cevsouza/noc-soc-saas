package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
)

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
