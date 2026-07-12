package security

import (
	"bytes"
	"testing"

	"github.com/google/uuid"
)

var testMasterKey = []byte("0123456789abcdef0123456789abcdef") // 32 bytes

func TestDeriveTenantKeyDeterministic(t *testing.T) {
	tenant := uuid.New()
	k1, err := DeriveTenantKey(testMasterKey, tenant)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	k2, err := DeriveTenantKey(testMasterKey, tenant)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !bytes.Equal(k1, k2) {
		t.Error("same master key + tenant must derive the same key")
	}
	if len(k1) != 32 {
		t.Errorf("derived key must be 32 bytes, got %d", len(k1))
	}
}

func TestDeriveTenantKeyDiffersPerTenant(t *testing.T) {
	a, _ := DeriveTenantKey(testMasterKey, uuid.New())
	b, _ := DeriveTenantKey(testMasterKey, uuid.New())
	if bytes.Equal(a, b) {
		t.Error("different tenants must derive different keys")
	}
}

func TestDeriveTenantKeyRejectsBadMasterKey(t *testing.T) {
	if _, err := DeriveTenantKey([]byte("too-short"), uuid.New()); err == nil {
		t.Error("expected error for non-32-byte master key")
	}
}

func TestEncryptForTenantRoundTrip(t *testing.T) {
	tenant := uuid.New()
	plain := []byte("super-secret-value")

	cipher, nonce, err := EncryptForTenant(plain, testMasterKey, tenant)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	got, err := DecryptForTenant(cipher, nonce, testMasterKey, tenant)
	if err != nil {
		t.Fatalf("decrypt error: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("round trip mismatch: got %q want %q", got, plain)
	}
}

func TestDecryptForTenantRejectsOtherTenant(t *testing.T) {
	tenantA := uuid.New()
	tenantB := uuid.New()
	plain := []byte("tenant-A-only")

	cipher, nonce, err := EncryptForTenant(plain, testMasterKey, tenantA)
	if err != nil {
		t.Fatalf("encrypt error: %v", err)
	}
	// Tenant B must NOT be able to decrypt tenant A's secret. Because DecryptForTenant falls back
	// to the raw master key, we assert it fails: the ciphertext was written with A's derived key,
	// so neither B's derived key nor the raw master key can open it.
	if _, err := DecryptForTenant(cipher, nonce, testMasterKey, tenantB); err == nil {
		t.Error("tenant B must not be able to decrypt tenant A's secret")
	}
}

func TestDecryptForTenantReadsLegacyMasterKeyCiphertext(t *testing.T) {
	tenant := uuid.New()
	plain := []byte("legacy-secret")

	// Simulate a secret written before per-tenant derivation existed: encrypted directly with the
	// raw master key.
	legacyCipher, legacyNonce, err := Encrypt(plain, testMasterKey)
	if err != nil {
		t.Fatalf("legacy encrypt error: %v", err)
	}
	// The tenant-aware decrypt must transparently fall back and read it.
	got, err := DecryptForTenant(legacyCipher, legacyNonce, testMasterKey, tenant)
	if err != nil {
		t.Fatalf("expected legacy fallback to succeed, got error: %v", err)
	}
	if !bytes.Equal(got, plain) {
		t.Errorf("legacy decrypt mismatch: got %q want %q", got, plain)
	}
}
