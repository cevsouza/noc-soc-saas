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

func TestExtractIndicatorsDomainAndHash(t *testing.T) {
	payload := map[string]interface{}{
		"domain": "Evil-Domain.COM",                                              // normalised to lowercase
		"url":    "https://bad.example.org/path?q=1",                             // host extracted
		"sha256": "AABBCCDDEEFF00112233445566778899AABBCCDDEEFF00112233445566778899", // 64-hex, lowercased
		"md5":    "d41d8cd98f00b204e9800998ecf8427e",                             // 32-hex
		"hash":   "notahash",                                                     // rejected
		"query":  "192.168.0.1",                                                  // an IP is not a domain
	}
	got := ExtractIndicators("", payload)
	byType := map[string][]string{}
	for _, ind := range got {
		byType[ind.Type] = append(byType[ind.Type], ind.Value)
	}
	domains := byType[IndicatorTypeDomain]
	if !contains(domains, "evil-domain.com") || !contains(domains, "bad.example.org") {
		t.Errorf("expected domains evil-domain.com + bad.example.org, got %+v", domains)
	}
	if contains(domains, "192.168.0.1") {
		t.Errorf("an IP must not be captured as a domain: %+v", domains)
	}
	hashes := byType[IndicatorTypeHash]
	if !contains(hashes, "aabbccddeeff00112233445566778899aabbccddeeff00112233445566778899") || !contains(hashes, "d41d8cd98f00b204e9800998ecf8427e") {
		t.Errorf("expected sha256+md5 hashes, got %+v", hashes)
	}
	if contains(hashes, "notahash") {
		t.Errorf("invalid hash must be rejected: %+v", hashes)
	}
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}

func TestFleetRiskBonus(t *testing.T) {
	cases := []struct {
		others int
		want   int
	}{{0, 0}, {1, 10}, {2, 10}, {3, 20}, {4, 20}, {5, 30}, {12, 30}}
	for _, c := range cases {
		if got := FleetRiskBonus(c.others); got != c.want {
			t.Errorf("FleetRiskBonus(%d)=%d want %d", c.others, got, c.want)
		}
	}
}

func TestSummarize(t *testing.T) {
	s := Summarize([]Indicator{{IndicatorTypeIP, "8.8.8.8"}, {IndicatorTypeDomain, "evil.com"}})
	if s != "IP 8.8.8.8, domínio evil.com" {
		t.Errorf("unexpected summary: %q", s)
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
