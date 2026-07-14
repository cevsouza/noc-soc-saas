// Package threatintel implements the cross-tenant threat-intel network effect (Backlog B6).
//
// Opted-in tenants contribute observed indicators (public IPs for now) to a global, anonymized
// aggregate; every opted-in tenant can then read the shared aggregate. No raw tenant id ever reaches
// the shared tables — distinct contributors are counted via a pseudonymous HMAC of the tenant id.
package threatintel

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Indicator types recorded in the shared aggregate.
const (
	IndicatorTypeIP     = "ip"
	IndicatorTypeDomain = "domain"
	IndicatorTypeHash   = "hash"
)

// Indicator is one extracted IOC.
type Indicator struct {
	Type  string `json:"indicator_type"`
	Value string `json:"indicator_value"`
}

// payloadIPKeys are the common payload fields that carry a source IP across the various connectors.
var payloadIPKeys = []string{"src_ip", "source_ip", "sourceip", "client_ip", "clientip", "ip", "remote_addr", "src", "source_address"}

// payloadDomainKeys carry a domain / FQDN across connectors (EDR/DNS/proxy sources).
var payloadDomainKeys = []string{"domain", "fqdn", "dns_query", "query", "hostname", "url"}

// payloadHashKeys carry a file hash (md5/sha1/sha256) across EDR/AV sources.
var payloadHashKeys = []string{"sha256", "sha1", "md5", "file_hash", "filehash", "hash"}

// hexHashRe matches an md5 (32), sha1 (40) or sha256 (64) hex digest.
var hexHashRe = regexp.MustCompile(`^[a-f0-9]{32}$|^[a-f0-9]{40}$|^[a-f0-9]{64}$`)

// domainRe is a loose domain/FQDN matcher: labels separated by dots ending in a 2+ char alpha TLD.
var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

// ExtractIndicators pulls shareable IOCs from an alert's host + raw payload: public IPs, domains and
// file hashes. Only PUBLIC IPs are returned (RFC1918 private, loopback, link-local and other
// non-global addresses are dropped so a tenant's internal topology is never shared). Pure and
// unit-testable; results are de-duplicated across (type, value).
func ExtractIndicators(host string, payload map[string]interface{}) []Indicator {
	seen := map[string]bool{}
	var out []Indicator
	add := func(typ, val string) {
		if val == "" {
			return
		}
		key := typ + ":" + val
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, Indicator{Type: typ, Value: val})
	}
	str := func(v interface{}) string {
		s, _ := v.(string)
		return s
	}

	add(IndicatorTypeIP, parsePublicIP(host))
	for _, k := range payloadIPKeys {
		if v, ok := payload[k]; ok {
			add(IndicatorTypeIP, parsePublicIP(str(v)))
		}
	}
	for _, k := range payloadDomainKeys {
		if v, ok := payload[k]; ok {
			add(IndicatorTypeDomain, parseDomain(str(v)))
		}
	}
	for _, k := range payloadHashKeys {
		if v, ok := payload[k]; ok {
			add(IndicatorTypeHash, parseHash(str(v)))
		}
	}
	return out
}

// parseDomain normalises s to a lowercase domain/FQDN, extracting the host from a URL when given one.
// Returns "" if s is empty, an IP, or not a plausible domain. Pure.
func parseDomain(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil && u.Hostname() != "" {
			s = u.Hostname()
		}
	}
	s = strings.TrimSuffix(s, ".")
	if net.ParseIP(s) != nil { // an IP is not a domain
		return ""
	}
	if len(s) > 253 || !domainRe.MatchString(s) {
		return ""
	}
	return s
}

// parseHash normalises s to a lowercase md5/sha1/sha256 hex digest, or "" if it isn't one. Pure.
func parseHash(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if !hexHashRe.MatchString(s) {
		return ""
	}
	return s
}

// parsePublicIP returns the canonical string form of s if it is a routable public IP, else "".
func parsePublicIP(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// Tolerate "ip:port" by trimming a trailing port when present.
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	ip := net.ParseIP(s)
	if ip == nil {
		return ""
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() {
		return ""
	}
	return ip.String()
}

// TenantHash returns the pseudonymous HMAC-SHA256 of a tenant id under the server's VAULT_MASTER_KEY.
// Used to count distinct contributors without persisting a raw tenant id in the shared tables.
func TenantHash(tenantID uuid.UUID) string {
	mac := hmac.New(sha256.New, []byte(os.Getenv("VAULT_MASTER_KEY")))
	mac.Write([]byte("threat-intel:" + tenantID.String()))
	return hex.EncodeToString(mac.Sum(nil))
}

// Record upserts the given indicators into the global aggregate for one contributing tenant (already
// hashed). Best-effort: partial failures are returned but callers treat contribution as non-critical.
// The contribution ledger's ON CONFLICT tells us whether this tenant is a NEW distinct contributor,
// so tenant_count only increments once per (indicator, tenant).
func Record(ctx context.Context, pool *pgxpool.Pool, tenantHash string, indicators []Indicator) error {
	for _, ind := range indicators {
		var isNewContributor bool
		err := pool.QueryRow(ctx, `
			INSERT INTO threat_intel_contributions (indicator_type, indicator_value, tenant_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT (indicator_type, indicator_value, tenant_hash) DO NOTHING
			RETURNING true
		`, ind.Type, ind.Value, tenantHash).Scan(&isNewContributor)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		inc := 0
		if isNewContributor {
			inc = 1
		}
		if _, err := pool.Exec(ctx, `
			INSERT INTO threat_intel_indicators (indicator_type, indicator_value, observation_count, tenant_count, first_seen, last_seen)
			VALUES ($1, $2, 1, $3, NOW(), NOW())
			ON CONFLICT (indicator_type, indicator_value)
			DO UPDATE SET observation_count = threat_intel_indicators.observation_count + 1,
			              tenant_count = threat_intel_indicators.tenant_count + $3,
			              last_seen = NOW()
		`, ind.Type, ind.Value, inc); err != nil {
			return err
		}
	}
	return nil
}

// SharedIndicator is one row of the aggregate exposed by the read API (no tenant identity).
type SharedIndicator struct {
	IndicatorType    string    `json:"indicator_type"`
	IndicatorValue   string    `json:"indicator_value"`
	ObservationCount int64     `json:"observation_count"`
	TenantCount      int       `json:"tenant_count"`
	FirstSeen        time.Time `json:"first_seen"`
	LastSeen         time.Time `json:"last_seen"`
}

// OtherTenantSightings returns how many OTHER opted-in tenants have seen this event's indicators —
// the cross-tenant corroboration signal that drives risk enrichment (B6 fatia 2). "Other" excludes
// the observing tenant itself (identified by its own tenant_hash), so a tenant's repeated sightings
// of its own indicator never inflate the signal. Returns the max across the event's indicators.
func OtherTenantSightings(ctx context.Context, pool *pgxpool.Pool, tenantHash string, indicators []Indicator) (int, error) {
	max := 0
	for _, ind := range indicators {
		var tenantCount int
		err := pool.QueryRow(ctx, `SELECT tenant_count FROM threat_intel_indicators WHERE indicator_type = $1 AND indicator_value = $2`, ind.Type, ind.Value).Scan(&tenantCount)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return 0, err
		}
		var mine int
		if err := pool.QueryRow(ctx, `SELECT count(*) FROM threat_intel_contributions WHERE indicator_type = $1 AND indicator_value = $2 AND tenant_hash = $3`, ind.Type, ind.Value, tenantHash).Scan(&mine); err != nil {
			return 0, err
		}
		if others := tenantCount - mine; others > max {
			max = others
		}
	}
	return max, nil
}

// indicatorLabels are the human labels used when summarizing IOCs in an investigation note.
var indicatorLabels = map[string]string{IndicatorTypeIP: "IP", IndicatorTypeDomain: "domínio", IndicatorTypeHash: "hash"}

// Summarize renders a compact human list of indicators for an incident note (e.g. "IP 8.8.8.8,
// domínio evil.com"). Pure.
func Summarize(indicators []Indicator) string {
	parts := make([]string, 0, len(indicators))
	for _, ind := range indicators {
		label := indicatorLabels[ind.Type]
		if label == "" {
			label = ind.Type
		}
		parts = append(parts, label+" "+ind.Value)
	}
	return strings.Join(parts, ", ")
}

// FleetRiskBonus maps the number of OTHER tenants that have seen an indicator to a risk-score bonus.
// More corroborating tenants ⇒ higher confidence the indicator is genuinely malicious. Pure.
func FleetRiskBonus(otherTenants int) int {
	switch {
	case otherTenants >= 5:
		return 30
	case otherTenants >= 3:
		return 20
	case otherTenants >= 1:
		return 10
	default:
		return 0
	}
}

// TopShared returns the most widely-seen indicators (by distinct-tenant count, then recency). The
// result is an aggregate only — it never reveals which tenants contributed.
func TopShared(ctx context.Context, pool *pgxpool.Pool, limit int) ([]SharedIndicator, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := pool.Query(ctx, `
		SELECT indicator_type, indicator_value, observation_count, tenant_count, first_seen, last_seen
		FROM threat_intel_indicators
		ORDER BY tenant_count DESC, last_seen DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]SharedIndicator, 0, limit)
	for rows.Next() {
		var s SharedIndicator
		if err := rows.Scan(&s.IndicatorType, &s.IndicatorValue, &s.ObservationCount, &s.TenantCount, &s.FirstSeen, &s.LastSeen); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
