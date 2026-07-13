'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { RefreshCw, Radar, ServerCog, Wifi, WifiOff, Settings2, AlertCircle } from 'lucide-react';
import { apiFetchJson } from '@/lib/api-client';
import type { TopologyGraph, GraphNode, TopologyStatus } from '@/types';

// Merged topology graph (discovery slice C, onboarding in slice T1). Unifies three real sources fetched
// from GET /api/v1/topology/graph: devices found by the active SNMP sweep (slice A), assets that reported
// telemetry/alerts (carrying severity), and the physical LLDP/CDP edges between them (slice B). Lines
// here are REAL physical adjacencies (not "reports to NOC core"). Clicking a node filters the Alerts
// tab by that asset. GET /api/v1/topology/status drives the onboarding state so an empty graph explains
// that it needs an agent + CIDR ranges instead of degrading silently.
export function TopologyView({
  tenantId,
  onSearchTermChange,
  onConfigureDiscovery,
}: {
  tenantId?: string;
  onSearchTermChange: (term: string) => void;
  onConfigureDiscovery?: () => void;
}) {
  const [data, setData] = useState<TopologyGraph | null>(null);
  const [status, setStatus] = useState<TopologyStatus | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchGraph = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const qs = tenantId ? `?tenant_id=${tenantId}` : '';
      const [g, s] = await Promise.all([
        apiFetchJson<TopologyGraph>(`/api/v1/topology/graph${qs}`),
        apiFetchJson<TopologyStatus>(`/api/v1/topology/status${qs}`),
      ]);
      setData(g);
      setStatus(s);
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

  // No actively-discovered devices yet: the physical topology can't be drawn until an agent sweeps the
  // network. Show the onboarding banner (strong empty-state when there's nothing at all, inline note
  // when there are at least telemetry-derived hosts to show).
  const noDiscovery = !!status && status.discovered_devices === 0;
  const nothingAtAll = noDiscovery && nodes.length === 0;

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

      {/* Discovery status strip: is an agent connected, when did it last check in, how much has it found */}
      {status && <StatusStrip status={status} onConfigureDiscovery={onConfigureDiscovery} />}

      {error && (
        <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300 mb-3">{error}</div>
      )}

      {/* Inline note when there are telemetry hosts to show but no actively-discovered devices yet */}
      {noDiscovery && !nothingAtAll && (
        <div className="mb-3 flex items-start gap-2 p-3 rounded-lg bg-amber-950/15 border border-amber-500/20 text-[11px] text-amber-200/90">
          <AlertCircle className="w-4 h-4 mt-0.5 shrink-0 text-amber-400" />
          <span>
            O grafo abaixo mostra apenas ativos <strong>derivados de alertas</strong>. A topologia física
            (dispositivos e enlaces LLDP/CDP) só aparece quando um agente varre a rede.{' '}
            {onConfigureDiscovery && (
              <button onClick={onConfigureDiscovery} className="underline font-bold text-amber-100 hover:text-white">
                Configurar descoberta →
              </button>
            )}
          </span>
        </div>
      )}

      <div className="relative w-full h-[460px] bg-black/60 rounded-xl border border-white/5 flex items-center justify-center overflow-hidden">
        {isLoading && !data ? (
          <div className="text-xs text-slate-500">Carregando grafo…</div>
        ) : nothingAtAll ? (
          <DiscoveryOnboarding onConfigureDiscovery={onConfigureDiscovery} hasAgent={!!status && status.agent_count > 0} hasTargets={!!status && status.discovery_targets > 0} />
        ) : nodes.length === 0 ? (
          <div className="flex flex-col items-center gap-2 text-slate-500 px-6 text-center">
            <ServerCog className="w-8 h-8 text-slate-600" />
            <p className="text-sm font-bold text-slate-400">Nenhum ativo no grafo ainda</p>
            <p className="text-[11px] max-w-md">
              Os nós aparecem conforme a descoberta ativa (varredura SNMP) inventaria dispositivos e as
              fontes de monitoramento reportam alertas.
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

// StatusStrip shows whether active discovery is wired up, so the graph state is never a mystery.
function StatusStrip({ status, onConfigureDiscovery }: { status: TopologyStatus; onConfigureDiscovery?: () => void }) {
  const agent = agentState(status);
  return (
    <div className="mb-3 flex flex-wrap items-center justify-between gap-2 px-3 py-2 rounded-lg bg-white/[0.03] border border-white/5">
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 text-[11px]">
        <span className={`flex items-center gap-1.5 font-bold ${agent.className}`}>
          {agent.connected ? <Wifi className="w-3.5 h-3.5" /> : <WifiOff className="w-3.5 h-3.5" />}
          {agent.label}
        </span>
        {status.last_seen && (
          <span className="text-slate-500">Último contato: {relativeTime(status.last_seen)}</span>
        )}
        <span className="text-slate-400">
          <strong className="text-slate-200">{status.discovery_targets}</strong> faixa(s) ·{' '}
          <strong className="text-slate-200">{status.discovered_devices}</strong> dispositivo(s) ·{' '}
          <strong className="text-slate-200">{status.discovered_links}</strong> enlace(s)
        </span>
      </div>
      {onConfigureDiscovery && (
        <button
          onClick={onConfigureDiscovery}
          className="flex items-center gap-1.5 px-2.5 py-1 rounded-md text-[11px] font-bold text-cyan-200 bg-cyan-500/10 hover:bg-cyan-500/20 border border-cyan-500/25 transition-all"
        >
          <Settings2 className="w-3.5 h-3.5" /> Descoberta de Rede
        </button>
      )}
    </div>
  );
}

// DiscoveryOnboarding is the strong empty-state shown when nothing has been discovered and there are no
// telemetry hosts either — it explains what the topology is and exactly how to populate it.
function DiscoveryOnboarding({ onConfigureDiscovery, hasAgent, hasTargets }: { onConfigureDiscovery?: () => void; hasAgent: boolean; hasTargets: boolean }) {
  return (
    <div className="flex flex-col items-center gap-3 text-slate-400 px-8 text-center max-w-lg">
      <div className="w-14 h-14 rounded-full bg-cyan-500/10 border border-cyan-500/20 flex items-center justify-center">
        <Radar className="w-7 h-7 text-cyan-400" />
      </div>
      <p className="text-base font-extrabold text-slate-200">A topologia é montada por descoberta ativa</p>
      <p className="text-[12px] leading-relaxed">
        Um <strong className="text-slate-300">agente</strong> instalado na sua rede varre as faixas
        configuradas via <strong className="text-slate-300">SNMP</strong> (só saída 443) e lê a
        vizinhança <strong className="text-slate-300">LLDP/CDP</strong> dos equipamentos — assim o grafo
        mostra dispositivos e enlaces físicos reais, inclusive os que nunca dispararam um alerta.
      </p>
      <div className="text-[11px] text-left w-full flex flex-col gap-1.5 bg-white/[0.02] border border-white/5 rounded-lg p-3">
        <span className={`flex items-center gap-2 ${hasAgent ? 'text-emerald-300' : 'text-slate-400'}`}>
          <span className={`w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-bold ${hasAgent ? 'bg-emerald-500/20 text-emerald-300' : 'bg-white/10 text-slate-400'}`}>1</span>
          Instalar e enrolar o agente {hasAgent ? '✓' : '(pendente)'}
        </span>
        <span className={`flex items-center gap-2 ${hasTargets ? 'text-emerald-300' : 'text-slate-400'}`}>
          <span className={`w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-bold ${hasTargets ? 'bg-emerald-500/20 text-emerald-300' : 'bg-white/10 text-slate-400'}`}>2</span>
          Cadastrar uma faixa CIDR + SNMP community {hasTargets ? '✓' : '(pendente)'}
        </span>
        <span className="flex items-center gap-2 text-slate-400">
          <span className="w-4 h-4 rounded-full flex items-center justify-center text-[9px] font-bold bg-white/10 text-slate-400">3</span>
          Aguardar o próximo ciclo de varredura (~15 min)
        </span>
      </div>
      {onConfigureDiscovery && (
        <button
          onClick={onConfigureDiscovery}
          className="flex items-center gap-2 px-4 py-2 rounded-lg text-xs font-bold text-cyan-100 bg-cyan-500/15 hover:bg-cyan-500/25 border border-cyan-500/30 transition-all"
        >
          <Settings2 className="w-4 h-4" /> Configurar Descoberta de Rede
        </button>
      )}
    </div>
  );
}

function agentState(s: TopologyStatus): { connected: boolean; label: string; className: string } {
  if (s.agent_count === 0) return { connected: false, label: 'Nenhum agente conectado', className: 'text-slate-400' };
  if (s.agent_connected) return { connected: true, label: 'Agente conectado', className: 'text-emerald-300' };
  return { connected: false, label: 'Agente sem contato recente', className: 'text-amber-300' };
}

// relativeTime renders a short pt-BR "há X" string from an ISO timestamp.
function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '—';
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (secs < 60) return `há ${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `há ${mins} min`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `há ${hours}h`;
  const days = Math.floor(hours / 24);
  return `há ${days}d`;
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
