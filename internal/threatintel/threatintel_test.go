package threatintel

import (
	"testing"

	"github.com/google/uuid"
)

func TestExtractIndicatorsKeepsPublicIPsOnly(t *testing.T) {
	payload := map[string]interface{}{
		"src_ip":    "8.8.8.8",       // public → kept
		"client_ip": "10.0.0.5",      // RFC1918 → dropped
		"ip":        "192.168.1.1",   // RFC1918 → dropped
		"remote_addr": "1.1.1.1:443", // public with port → kept, port stripped
		"noise":     "not-an-ip",     // ignored
	}
	got := ExtractIndicators("127.0.0.1", payload) // loopback host → dropped

	values := map[string]bool{}
	for _, ind := range got {
		if ind.Type != IndicatorTypeIP {
			t.Errorf("unexpected indicator type %q", ind.Type)
		}
		values[ind.Value] = true
	}
	if !values["8.8.8.8"] || !values["1.1.1.1"] {
		t.Errorf("expected public IPs 8.8.8.8 and 1.1.1.1, got %+v", got)
	}
	if values["10.0.0.5"] || values["192.168.1.1"] || values["127.0.0.1"] {
		t.Errorf("private/loopback IPs must be dropped, got %+v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected exactly 2 indicators, got %d: %+v", len(got), got)
	}
}

func TestExtractIndicatorsDeduplicates(t *testing.T) {
	got := ExtractIndicators("9.9.9.9", map[string]interface{}{"src_ip": "9.9.9.9", "ip": "9.9.9.9"})
	if len(got) != 1 {
		t.Fatalf("expected 1 deduped indicator, got %d: %+v", len(got), got)
	}
}

func TestExtractIndicatorsEmpty(t *testing.T) {
	if got := ExtractIndicators("", nil); len(got) != 0 {
		t.Errorf("expected no indicators, got %+v", got)
	}
}

func TestTenantHashDeterministicAndOpaque(t *testing.T) {
	id := uuid.New()
	h1 := TenantHash(id)
	h2 := TenantHash(id)
	if h1 != h2 {
		t.Error("TenantHash must be deterministic for the same tenant")
	}
	if len(h1) != 64 {
		t.Errorf("expected 64-hex-char HMAC, got %d chars", len(h1))
	}
	if h1 == id.String() {
		t.Error("hash must not equal the raw tenant id")
	}
	if TenantHash(uuid.New()) == h1 {
		t.Error("different tenants must hash differently")
	}
}
