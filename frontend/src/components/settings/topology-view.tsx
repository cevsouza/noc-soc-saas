'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Radar, ServerCog } from 'lucide-react';
import { apiFetchJson } from '@/lib/api-client';
import type { TopologyResponse, TopologyNode } from '@/types';

// Real asset topology (Fase 7 fatia A-topologia). Replaces the old hardcoded 6-node SVG that was
// identical for every tenant with the hosts that have actually reported telemetry, fetched from
// GET /api/v1/topology. Lines here mean "reports telemetry into the NOC core" (a real relationship),
// not physical network links we don't have. Clicking a node filters the Alerts tab by that host.
export function TopologyView({
  tenantId,
  onSearchTermChange,
}: {
  tenantId?: string;
  onSearchTermChange: (term: string) => void;
}) {
  const [data, setData] = useState<TopologyResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchTopology = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const qs = tenantId ? `?tenant_id=${tenantId}` : '';
      const res = await apiFetchJson<TopologyResponse>(`/api/v1/topology${qs}`);
      setData(res);
    } catch (err) {
      console.error('Failed to fetch topology:', err);
      setError('Não foi possível carregar a topologia de ativos.');
    } finally {
      setIsLoading(false);
    }
  }, [tenantId]);

  useEffect(() => {
    fetchTopology();
  }, [fetchTopology]);

  const nodes = data?.nodes ?? [];

  // Radial layout: assets arranged in a ring around a central NOC core. Positions computed once
  // per node set. viewBox is 800x440; center at (400,210).
  const layout = useMemo(() => {
    const cx = 400;
    const cy = 210;
    const n = nodes.length;
    const radius = n <= 6 ? 130 : n <= 12 ? 155 : 175;
    return nodes.map((node, i) => {
      const angle = (2 * Math.PI * i) / Math.max(n, 1) - Math.PI / 2;
      return { node, x: cx + radius * Math.cos(angle), y: cy + radius * Math.sin(angle) };
    });
  }, [nodes]);

  return (
    <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5 p-6 bg-[#040812]">
      <div className="flex justify-between items-center mb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Radar className="w-4 h-4 text-cyan-400" /> Mapeamento de Topologia &amp; CMDB
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Ativos reais derivados da telemetria recebida ({data?.window_days ?? 30}d) · {data?.total_assets ?? 0} host(s)
          </p>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex gap-3 text-[10px] font-bold text-slate-400">
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-emerald-500"></span> Operacional</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-amber-500"></span> Atenção</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-rose-500 animate-ping"></span> Incidente</span>
          </div>
          <button
            onClick={fetchTopology}
            disabled={isLoading}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all disabled:opacity-50"
          >
            <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
          </button>
        </div>
      </div>

      {error && (
        <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300 mb-3">{error}</div>
      )}

      <div className="relative w-full h-[420px] bg-black/60 rounded-xl border border-white/5 flex items-center justify-center overflow-hidden">
        {isLoading && !data ? (
          <div className="text-xs text-slate-500">Carregando ativos…</div>
        ) : nodes.length === 0 ? (
          <div className="flex flex-col items-center gap-2 text-slate-500 px-6 text-center">
            <ServerCog className="w-8 h-8 text-slate-600" />
            <p className="text-sm font-bold text-slate-400">Nenhum ativo descoberto ainda</p>
            <p className="text-[11px] max-w-md">
              Os nós aparecem automaticamente conforme as fontes de monitoramento enviam alertas
              identificando o host afetado. Configure e conecte as integrações para popular o mapa.
            </p>
          </div>
        ) : (
          <svg className="w-full h-full" viewBox="0 0 800 440">
            <defs>
              <pattern id="topo-grid" width="20" height="20" patternUnits="userSpaceOnUse">
                <path d="M 20 0 L 0 0 0 20" fill="none" stroke="rgba(255,255,255,0.015)" strokeWidth="1" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill="url(#topo-grid)" />

            {/* Links: every asset reports into the central NOC core */}
            {layout.map(({ node, x, y }) => (
              <line
                key={`l-${node.host}`}
                x1="400"
                y1="210"
                x2={x}
                y2={y}
                stroke="rgba(255,255,255,0.08)"
                strokeWidth="1.5"
                strokeDasharray={node.unresolved_alerts > 0 ? '4 2' : undefined}
              />
            ))}

            {/* Central NOC core */}
            <g>
              <circle cx="400" cy="210" r="34" className="fill-slate-900 stroke-cyan-500/50 stroke-2" />
              <text x="400" y="207" className="fill-cyan-300 font-bold" fontSize="11" textAnchor="middle">NOC</text>
              <text x="400" y="220" className="fill-slate-400 font-bold" fontSize="8" textAnchor="middle">CORE</text>
            </g>

            {/* Asset nodes */}
            {layout.map(({ node, x, y }) => {
              const color = severityColor(node.worst_severity, node.unresolved_alerts);
              const label = node.host.length > 14 ? node.host.slice(0, 13) + '…' : node.host;
              return (
                <g key={node.host} className="cursor-pointer" onClick={() => onSearchTermChange(node.host)}>
                  <title>
                    {`${node.host}\n${node.total_alerts} alerta(s), ${node.unresolved_alerts} em aberto${
                      node.sources.length ? `\nFontes: ${node.sources.join(', ')}` : ''
                    }`}
                  </title>
                  {node.unresolved_alerts > 0 && (
                    <circle cx={x} cy={y} r="30" className="fill-none animate-ping" stroke={color.ring} strokeWidth="1" />
                  )}
                  <circle cx={x} cy={y} r="24" fill={color.fill} stroke={color.stroke} strokeWidth="2" />
                  <text x={x} y={y - 1} className="fill-slate-100 font-bold" fontSize="8.5" textAnchor="middle">
                    {label}
                  </text>
                  {node.unresolved_alerts > 0 && (
                    <text x={x} y={y + 10} className="fill-slate-300" fontSize="7.5" textAnchor="middle">
                      {node.unresolved_alerts} aberto(s)
                    </text>
                  )}
                </g>
              );
            })}
          </svg>
        )}

        <div className="absolute bottom-4 left-6 text-[10px] text-slate-500 bg-black/60 border border-white/5 px-2.5 py-1 rounded-md">
          💡 <em>Clique num ativo para filtrar os incidentes daquele host na aba Alertas.</em>
        </div>
      </div>
    </div>
  );
}

function severityColor(worst: string, unresolved: number): { fill: string; stroke: string; ring: string } {
  if (unresolved === 0) return { fill: '#0f172a', stroke: '#10b981', ring: '#10b981' }; // operational (emerald)
  switch (worst) {
    case 'fatal':
    case 'critical':
      return { fill: '#221015', stroke: '#f43f5e', ring: '#f43f5e' }; // rose
    case 'warning':
      return { fill: '#221a10', stroke: '#f59e0b', ring: '#f59e0b' }; // amber
    default:
      return { fill: '#0f172a', stroke: '#38bdf8', ring: '#38bdf8' }; // info (sky)
  }
}
