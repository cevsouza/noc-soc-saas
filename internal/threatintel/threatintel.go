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
	"os"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IndicatorTypeIP is the only indicator type recorded in fatia 1 (domain/hash come in fatia 2).
const IndicatorTypeIP = "ip"

// Indicator is one extracted IOC.
type Indicator struct {
	Type  string `json:"indicator_type"`
	Value string `json:"indicator_value"`
}

// payloadIPKeys are the common payload fields that carry a source IP across the various connectors.
var payloadIPKeys = []string{"src_ip", "source_ip", "sourceip", "client_ip", "clientip", "ip", "remote_addr", "src", "source_address"}

// ExtractIndicators pulls shareable IOCs from an alert's host + raw payload. Only PUBLIC IPs are
// returned: RFC1918 private, loopback, link-local and other non-global addresses are dropped so a
// tenant's internal topology is never shared. Pure and unit-testable; results are de-duplicated.
func ExtractIndicators(host string, payload map[string]interface{}) []Indicator {
	seen := map[string]bool{}
	var out []Indicator
	add := func(raw string) {
		ip := parsePublicIP(raw)
		if ip == "" || seen[ip] {
			return
		}
		seen[ip] = true
		out = append(out, Indicator{Type: IndicatorTypeIP, Value: ip})
	}

	add(host)
	for _, k := range payloadIPKeys {
		if v, ok := payload[k]; ok {
			if s, ok := v.(string); ok {
				add(s)
			}
		}
	}
	return out
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
