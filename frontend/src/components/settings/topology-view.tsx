'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Radar, ServerCog } from 'lucide-react';
import { apiFetchJson } from '@/lib/api-client';
import type { TopologyGraph, GraphNode } from '@/types';

// Merged topology graph (discovery slice C). Unifies three real sources fetched from
// GET /api/v1/topology/graph: devices found by the active SNMP sweep (slice A), assets that reported
// telemetry/alerts (carrying severity), and the physical LLDP/CDP edges between them (slice B). Lines
// here are REAL physical adjacencies (not "reports to NOC core"). Clicking a node filters the Alerts
// tab by that asset.
export function TopologyView({
  tenantId,
  onSearchTermChange,
}: {
  tenantId?: string;
  onSearchTermChange: (term: string) => void;
}) {
  const [data, setData] = useState<TopologyGraph | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchGraph = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const qs = tenantId ? `?tenant_id=${tenantId}` : '';
      const res = await apiFetchJson<TopologyGraph>(`/api/v1/topology/graph${qs}`);
      setData(res);
    } catch (err) {
      console.error('Failed to fetch topology graph:', err);
      setError('Não foi possível carregar o grafo de topologia.');
    } finally {
      setIsLoading(false);
    }
  }, [tenantId]);

  useEffect(() => {
    fetchGraph();
  }, [fetchGraph]);

  const nodes = data?.nodes ?? [];
  const edges = data?.edges ?? [];

  // Ring layout: nodes on a circle, physical edges drawn as chords between their positions. Positions
  // computed once per node set. viewBox 820x480; center (410,240).
  const positions = useMemo(() => {
    const cx = 410;
    const cy = 240;
    const n = nodes.length;
    const radius = n <= 6 ? 150 : n <= 14 ? 185 : 210;
    const map: Record<string, { x: number; y: number; node: GraphNode }> = {};
    nodes.forEach((node, i) => {
      const angle = (2 * Math.PI * i) / Math.max(n, 1) - Math.PI / 2;
      map[node.id] = { x: cx + radius * Math.cos(angle), y: cy + radius * Math.sin(angle), node };
    });
    return map;
  }, [nodes]);

  const counts = useMemo(() => {
    let discovery = 0;
    let telemetry = 0;
    let neighbor = 0;
    for (const n of nodes) {
      if (n.origin === 'neighbor') neighbor++;
      else if (n.origin === 'telemetry') telemetry++;
      else discovery++; // discovery + both
    }
    return { discovery, telemetry, neighbor };
  }, [nodes]);

  return (
    <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5 p-6 bg-[#040812]">
      <div className="flex justify-between items-center mb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Radar className="w-4 h-4 text-cyan-400" /> Grafo de Topologia (descoberta ativa)
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            {nodes.length} ativo(s) · {edges.length} enlace(s) físico(s) LLDP/CDP · descobertos {counts.discovery} · telemetria {counts.telemetry} · vizinhos {counts.neighbor}
          </p>
        </div>
        <div className="flex items-center gap-4">
          <div className="flex gap-3 text-[10px] font-bold text-slate-400">
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-emerald-500"></span> Operacional</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-amber-500"></span> Atenção</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-rose-500 animate-ping"></span> Incidente</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-slate-500"></span> Vizinho</span>
          </div>
          <button
            onClick={fetchGraph}
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

      <div className="relative w-full h-[460px] bg-black/60 rounded-xl border border-white/5 flex items-center justify-center overflow-hidden">
        {isLoading && !data ? (
          <div className="text-xs text-slate-500">Carregando grafo…</div>
        ) : nodes.length === 0 ? (
          <div className="flex flex-col items-center gap-2 text-slate-500 px-6 text-center">
            <ServerCog className="w-8 h-8 text-slate-600" />
            <p className="text-sm font-bold text-slate-400">Nenhum ativo no grafo ainda</p>
            <p className="text-[11px] max-w-md">
              Os nós aparecem conforme a descoberta ativa (varredura SNMP) inventaria dispositivos e as
              fontes de monitoramento reportam alertas. Os enlaces surgem quando os equipamentos expõem
              vizinhos por LLDP/CDP. Configure faixas em <strong>Descoberta de Rede</strong>.
            </p>
          </div>
        ) : (
          <svg className="w-full h-full" viewBox="0 0 820 480">
            <defs>
              <pattern id="topo-grid" width="20" height="20" patternUnits="userSpaceOnUse">
                <path d="M 20 0 L 0 0 0 20" fill="none" stroke="rgba(255,255,255,0.015)" strokeWidth="1" />
              </pattern>
            </defs>
            <rect width="100%" height="100%" fill="url(#topo-grid)" />

            {/* Physical edges (LLDP/CDP) */}
            {edges.map((e, i) => {
              const s = positions[e.source];
              const t = positions[e.target];
              if (!s || !t) return null;
              return (
                <g key={`e-${i}`}>
                  <line
                    x1={s.x}
                    y1={s.y}
                    x2={t.x}
                    y2={t.y}
                    stroke="rgba(56,189,248,0.35)"
                    strokeWidth="1.5"
                  />
                  <title>{`${s.node.label} (${e.local_port || '?'}) ↔ ${t.node.label} (${e.remote_port || '?'}) · ${e.protocol.toUpperCase()}`}</title>
                </g>
              );
            })}

            {/* Nodes */}
            {nodes.map((node) => {
              const p = positions[node.id];
              if (!p) return null;
              const color = nodeColor(node);
              const label = node.label.length > 14 ? node.label.slice(0, 13) + '…' : node.label;
              return (
                <g key={node.id} className="cursor-pointer" onClick={() => onSearchTermChange(node.label)}>
                  <title>
                    {`${node.label}\n${originLabel(node.origin)}${node.vendor ? ` · ${node.vendor}` : ''}${
                      node.kind ? ` · ${node.kind}` : ''
                    }${node.unresolved_alerts > 0 ? `\n${node.unresolved_alerts} alerta(s) em aberto` : ''}`}
                  </title>
                  {node.unresolved_alerts > 0 && (
                    <circle cx={p.x} cy={p.y} r="28" className="fill-none animate-ping" stroke={color.ring} strokeWidth="1" />
                  )}
                  <circle cx={p.x} cy={p.y} r="22" fill={color.fill} stroke={color.stroke} strokeWidth="2" strokeDasharray={node.origin === 'neighbor' ? '3 2' : undefined} />
                  <text x={p.x} y={p.y - 1} className="fill-slate-100 font-bold" fontSize="8.5" textAnchor="middle">
                    {label}
                  </text>
                  <text x={p.x} y={p.y + 9} className="fill-slate-400" fontSize="7" textAnchor="middle">
                    {node.unresolved_alerts > 0 ? `${node.unresolved_alerts} aberto(s)` : kindLabel(node.kind)}
                  </text>
                </g>
              );
            })}
          </svg>
        )}

        <div className="absolute bottom-4 left-6 text-[10px] text-slate-500 bg-black/60 border border-white/5 px-2.5 py-1 rounded-md">
          💡 <em>Linhas = enlaces físicos LLDP/CDP. Clique num ativo para filtrar os incidentes na aba Alertas.</em>
        </div>
      </div>
    </div>
  );
}

function nodeColor(node: GraphNode): { fill: string; stroke: string; ring: string } {
  if (node.unresolved_alerts > 0) {
    switch (node.worst_severity) {
      case 'fatal':
      case 'critical':
        return { fill: '#221015', stroke: '#f43f5e', ring: '#f43f5e' };
      case 'warning':
        return { fill: '#221a10', stroke: '#f59e0b', ring: '#f59e0b' };
      default:
        return { fill: '#0f172a', stroke: '#38bdf8', ring: '#38bdf8' };
    }
  }
  if (node.origin === 'neighbor') return { fill: '#0b0f19', stroke: '#64748b', ring: '#64748b' }; // slate (unmanaged)
  return { fill: '#0f172a', stroke: '#10b981', ring: '#10b981' }; // operational
}

function originLabel(origin: string): string {
  switch (origin) {
    case 'discovery':
      return 'Descoberto (SNMP)';
    case 'telemetry':
      return 'Telemetria (alertas)';
    case 'both':
      return 'Descoberto + telemetria';
    case 'neighbor':
      return 'Vizinho (não gerenciado)';
    default:
      return origin;
  }
}

function kindLabel(kind: string): string {
  switch (kind) {
    case 'firewall':
      return 'firewall';
    case 'switch':
      return 'switch';
    case 'router':
      return 'roteador';
    case 'access_point':
      return 'AP';
    case 'server':
      return 'servidor';
    case 'host':
      return 'host';
    case 'neighbor':
      return 'vizinho';
    default:
      return 'rede';
  }
}
