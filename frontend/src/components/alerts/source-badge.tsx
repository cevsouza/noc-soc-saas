import { getIntegrationById } from '@/lib/integrations-registry';

/**
 * Renders the alert's origin integration. Driven by the shared integrations registry (covers
 * all 17 backend integration types) instead of the original 6-way hardcoded ternary at
 * page.tsx:2396-2410, which only recognized prometheus/wazuh/sentinel/uptimekuma/grafana/zabbix
 * and fell everything else back to a generic gray badge.
 */
export function SourceBadge({ source }: { source?: string }) {
  const sourceId = source || 'generic';
  const entry = getIntegrationById(sourceId);
  const colorClass = entry?.accentColorClass || 'border-slate-500/20 text-slate-400';

  return (
    <span className={`inline-block px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider border bg-white/[0.03] ${colorClass}`}>
      {sourceId}
    </span>
  );
}
