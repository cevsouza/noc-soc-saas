// Mirrors internal/domain/domain.go — classifies an alert source into the NOC (network/availability)
// or SOC (security) console domain. Keep in sync with the Go mapping.
export type ConsoleDomain = 'noc' | 'soc';
export type ConsoleMode = 'all' | ConsoleDomain;

const SOC_SOURCES = new Set(['wazuh', 'sentinel', 'crowdstrike', 'paloalto', 'fortinet']);

// domainForSource returns 'soc' for security sources, 'noc' otherwise (default).
export function domainForSource(source: string | undefined | null): ConsoleDomain {
  return source && SOC_SOURCES.has(source.toLowerCase()) ? 'soc' : 'noc';
}
