// Package domain classifies an alert/incident source into an operational domain — NOC (network/
// availability/performance) vs SOC (security) — so the platform can offer segregated NOC and SOC
// consoles over the same tenant data. The domain is derived from the source (no schema change): an
// incident groups a single fingerprint = a single source, so its domain is well-defined too.
package domain

const (
	NOC = "noc" // network operations: availability, performance, infrastructure
	SOC = "soc" // security operations: threats, EDR, firewall, SIEM
)

// socSources are the security-domain sources. Everything else (monitoring/availability, plus the
// system watchdog and the agent's SNMP telemetry) is NOC. pagerduty/opsgenie are generic on-call
// routers and are treated as NOC (operational) by default — adjust here if a tenant uses them purely
// for security paging.
var socSources = map[string]bool{
	"wazuh":       true,
	"sentinel":    true,
	"crowdstrike": true,
	"paloalto":    true,
	"fortinet":    true,
}

// nocSources is the explicit NOC set (used to build the query filter). Kept in sync with the sources
// the connectors/agent emit; unknown sources default to NOC via ForSource.
var nocSources = []string{
	"prometheus", "zabbix", "uptimekuma", "grafana", "icinga", "otlp",
	"cloudwatch", "azuremonitor", "loki", "system", "snmp", "agent",
	"pagerduty", "opsgenie",
}

// ForSource returns the domain of a source (pure). Unknown sources default to NOC.
func ForSource(source string) string {
	if socSources[source] {
		return SOC
	}
	return NOC
}

// SourcesForDomain returns the explicit source list for a domain and whether the domain is valid.
// Used to build a `ai_analysis->>'source' = ANY($1)` filter. For NOC it returns the known NOC
// sources; note unknown sources also count as NOC via ForSource but won't match this ANY list —
// acceptable since the console filter targets the known connector sources.
func SourcesForDomain(d string) ([]string, bool) {
	switch d {
	case SOC:
		out := make([]string, 0, len(socSources))
		for s := range socSources {
			out = append(out, s)
		}
		return out, true
	case NOC:
		out := make([]string, len(nocSources))
		copy(out, nocSources)
		return out, true
	default:
		return nil, false
	}
}
