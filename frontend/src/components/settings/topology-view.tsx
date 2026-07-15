'use client';

import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { RefreshCw, Radar, ServerCog, Wifi, WifiOff, Settings2, AlertCircle, Search, X, Filter, ZoomIn, ZoomOut, Maximize2, Repeat, Tv } from 'lucide-react';
import { apiFetchJson } from '@/lib/api-client';
import type { TopologyGraph, GraphNode, GraphEdge, TopologyStatus } from '@/types';

// Fixed logical viewport the graph is drawn into; the SVG scales it to the container, and a
// transform on the inner <g> provides zoom/pan (T-A).
const VBW = 960;
const VBH = 480;

// Merged topology graph (discovery slice C; onboarding T1; robust matching T3; scalable view T4).
// Fetches GET /api/v1/topology/graph and renders it as a HIERARCHICAL layout grouped by device role
// (perimeter → core → access → hosts → unmanaged neighbours), with filters (kind/severity/origin/search)
// and per-node drill-down. Lines are REAL physical LLDP/CDP adjacencies. Clicking "filter alerts" on a
// node scopes the Alerts tab to that asset. GET /api/v1/topology/status drives the onboarding state.
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

  const [search, setSearch] = useState('');
  const [onlyAlerting, setOnlyAlerting] = useState(false);
  const [hideStale, setHideStale] = useState(false);
  const [originFilter, setOriginFilter] = useState<'all' | 'discovery' | 'telemetry' | 'both' | 'neighbor'>('all');
  const [colorMode, setColorMode] = useState<'severity' | 'subnet' | 'location'>('severity');
  const [hiddenTiers, setHiddenTiers] = useState<Set<string>>(new Set());
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [autoRefresh, setAutoRefresh] = useState(false);
  const [isFull, setIsFull] = useState(false);
  const containerRef = useRef<HTMLDivElement | null>(null);

  // silent=true (auto-refresh) skips the spinner and keeps the current selection/zoom, so a wallboard
  // doesn't flicker or lose the operator's view each cycle.
  const fetchGraph = useCallback(
    async (silent = false) => {
      if (!silent) setIsLoading(true);
      setError(null);
      try {
        const qs = tenantId ? `?tenant_id=${tenantId}` : '';
        const [g, s] = await Promise.all([
          apiFetchJson<TopologyGraph>(`/api/v1/topology/graph${qs}`),
          apiFetchJson<TopologyStatus>(`/api/v1/topology/status${qs}`),
        ]);
        setData(g);
        setStatus(s);
        if (!silent) setSelectedId(null);
      } catch (err) {
        console.error('Failed to fetch topology graph:', err);
        setError('Não foi possível carregar o grafo de topologia.');
      } finally {
        if (!silent) setIsLoading(false);
      }
    },
    [tenantId],
  );

  useEffect(() => {
    fetchGraph();
  }, [fetchGraph]);

  // Auto-refresh (T-E): poll every 30s while enabled, silently (no view reset).
  useEffect(() => {
    if (!autoRefresh) return;
    const id = setInterval(() => fetchGraph(true), 30000);
    return () => clearInterval(id);
  }, [autoRefresh, fetchGraph]);

  // Wallboard/fullscreen (T-E): mirror the browser fullscreen state so the toggle icon stays honest.
  useEffect(() => {
    const onFs = () => setIsFull(!!document.fullscreenElement);
    document.addEventListener('fullscreenchange', onFs);
    return () => document.removeEventListener('fullscreenchange', onFs);
  }, []);

  const toggleFullscreen = () => {
    if (document.fullscreenElement) {
      document.exitFullscreen().catch(() => {});
    } else {
      containerRef.current?.requestFullscreen().catch(() => {});
    }
  };

  const allNodes = useMemo(() => data?.nodes ?? [], [data]);
  const allEdges = useMemo(() => data?.edges ?? [], [data]);

  // Which tiers actually have nodes (drives the tier filter chips).
  const presentTiers = useMemo(() => {
    const set = new Set<string>();
    for (const n of allNodes) set.add(tierKey(n.kind));
    return TIERS.filter((t) => set.has(t.key));
  }, [allNodes]);

  // Apply all filters; edges kept only when both endpoints survive.
  const nodes = useMemo(() => {
    const q = search.trim().toLowerCase();
    return allNodes.filter((n) => {
      if (hiddenTiers.has(tierKey(n.kind))) return false;
      if (onlyAlerting && n.unresolved_alerts === 0) return false;
      if (hideStale && n.stale) return false;
      if (originFilter !== 'all' && n.origin !== originFilter) return false;
      if (q && !n.label.toLowerCase().includes(q) && !n.id.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [allNodes, hiddenTiers, onlyAlerting, hideStale, originFilter, search]);

  const staleCount = useMemo(() => allNodes.filter((n) => n.stale).length, [allNodes]);

  // Distinct groups present among the visible nodes, for the legend (T-C).
  const groups = useMemo(() => {
    if (colorMode === 'severity') return [];
    const set = new Set<string>();
    for (const n of nodes) set.add(groupKeyOf(n, colorMode));
    return Array.from(set).sort();
  }, [nodes, colorMode]);

  const visibleIds = useMemo(() => new Set(nodes.map((n) => n.id)), [nodes]);
  const edges = useMemo(
    () => allEdges.filter((e) => visibleIds.has(e.source) && visibleIds.has(e.target)),
    [allEdges, visibleIds],
  );

  const layout = useMemo(() => computeLayout(nodes, edges), [nodes, edges]);
  const nodesById = useMemo(() => {
    const m: Record<string, GraphNode> = {};
    for (const n of allNodes) m[n.id] = n;
    return m;
  }, [allNodes]);
  const selected = selectedId ? nodesById[selectedId] : null;

  // Zoom & pan (T-A). The graph lives in a fixed VBW×VBH viewport; a transform on the inner <g> scales
  // and translates it. Fit-to-view whenever the layout changes; wheel zooms toward the cursor; drag pans.
  const svgRef = useRef<SVGSVGElement | null>(null);
  const draggingRef = useRef(false);
  const didDragRef = useRef(false);
  const [tf, setTf] = useState({ x: 0, y: 0, k: 1 });

  const fitView = useCallback(() => {
    const k = Math.min(VBW / layout.width, VBH / layout.height, 1.2);
    setTf({ k, x: (VBW - layout.width * k) / 2, y: (VBH - layout.height * k) / 2 });
  }, [layout.width, layout.height]);

  useEffect(() => {
    fitView();
  }, [fitView]);

  const zoomAround = (factor: number, cx: number, cy: number) => {
    setTf((t) => {
      const k = Math.min(4, Math.max(0.2, t.k * factor));
      const r = k / t.k;
      return { k, x: cx - (cx - t.x) * r, y: cy - (cy - t.y) * r };
    });
  };

  // Wheel zoom needs a non-passive listener to preventDefault (React's onWheel is passive).
  useEffect(() => {
    const el = svgRef.current;
    if (!el) return;
    const onWheel = (e: WheelEvent) => {
      e.preventDefault();
      const rect = el.getBoundingClientRect();
      if (!rect.width) return;
      const cx = ((e.clientX - rect.left) * VBW) / rect.width;
      const cy = ((e.clientY - rect.top) * VBH) / rect.height;
      zoomAround(e.deltaY < 0 ? 1.12 : 1 / 1.12, cx, cy);
    };
    el.addEventListener('wheel', onWheel, { passive: false });
    return () => el.removeEventListener('wheel', onWheel);
  }, [nodes.length]);

  const onPanStart = () => {
    draggingRef.current = true;
    didDragRef.current = false;
  };
  const onPanMove = (e: React.MouseEvent) => {
    if (!draggingRef.current || !svgRef.current) return;
    const rect = svgRef.current.getBoundingClientRect();
    if (!rect.width) return;
    didDragRef.current = true;
    const sx = VBW / rect.width;
    const sy = VBH / rect.height;
    setTf((t) => ({ ...t, x: t.x + e.movementX * sx, y: t.y + e.movementY * sy }));
  };
  const onPanEnd = () => {
    draggingRef.current = false;
  };

  const noDiscovery = !!status && status.discovered_devices === 0;
  const nothingAtAll = noDiscovery && allNodes.length === 0;

  const toggleTier = (key: string) => {
    setHiddenTiers((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  };

  return (
    <div ref={containerRef} className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5 p-6 bg-[#040812]">
      <div className="flex justify-between items-start mb-4 gap-4 flex-wrap">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Radar className="w-4 h-4 text-cyan-400" /> Grafo de Topologia (descoberta ativa)
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            {nodes.length}/{allNodes.length} ativo(s) · {edges.length} enlace(s) físico(s) LLDP/CDP · layout hierárquico por função
          </p>
        </div>
        <div className="flex items-center gap-3">
          <div className="flex gap-3 text-[10px] font-bold text-slate-400">
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-emerald-500"></span> Operacional</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-amber-500"></span> Atenção</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-rose-500 animate-ping"></span> Incidente</span>
            <span className="flex items-center gap-1.5"><span className="w-2.5 h-2.5 rounded-full bg-slate-500"></span> Vizinho</span>
          </div>
          <button
            onClick={() => setAutoRefresh((v) => !v)}
            title="Atualizar o grafo automaticamente a cada 30s (sem perder o zoom/seleção)"
            className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold transition-all border ${
              autoRefresh ? 'bg-emerald-500/15 text-emerald-200 border-emerald-500/30' : 'bg-white/5 text-slate-300 border-white/10 hover:bg-white/10'
            }`}
          >
            <Repeat className={`w-3 h-3 ${autoRefresh ? 'animate-spin' : ''}`} style={autoRefresh ? { animationDuration: '3s' } : undefined} /> Auto {autoRefresh ? '(30s)' : ''}
          </button>
          <button
            onClick={toggleFullscreen}
            title="Modo wallboard (tela cheia)"
            className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold transition-all border ${
              isFull ? 'bg-cyan-500/15 text-cyan-200 border-cyan-500/30' : 'bg-white/5 text-slate-300 border-white/10 hover:bg-white/10'
            }`}
          >
            <Tv className="w-3 h-3" /> Wallboard
          </button>
          <button
            onClick={() => fetchGraph()}
            disabled={isLoading}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all disabled:opacity-50"
          >
            <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
          </button>
        </div>
      </div>

      {status && <StatusStrip status={status} onConfigureDiscovery={onConfigureDiscovery} />}

      {error && (
        <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300 mb-3">{error}</div>
      )}

      {noDiscovery && !nothingAtAll && (
        <div className="mb-3 flex items-start gap-2 p-3 rounded-lg bg-amber-950/15 border border-amber-500/20 text-[11px] text-amber-200/90">
          <AlertCircle className="w-4 h-4 mt-0.5 shrink-0 text-amber-400" />
          <span>
            O grafo mostra apenas ativos <strong>derivados de alertas</strong>. A topologia física
            (dispositivos e enlaces LLDP/CDP) só aparece quando um agente varre a rede.{' '}
            {onConfigureDiscovery && (
              <button onClick={onConfigureDiscovery} className="underline font-bold text-amber-100 hover:text-white">
                Configurar descoberta →
              </button>
            )}
          </span>
        </div>
      )}

      {/* Filter bar */}
      {allNodes.length > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <div className="relative">
            <Search className="w-3.5 h-3.5 text-slate-500 absolute left-2 top-1/2 -translate-y-1/2" />
            <input
              value={search}
              onChange={(e) => setSearch(e.target.value)}
              placeholder="Buscar ativo…"
              className="pl-7 pr-2 py-1.5 rounded-md bg-black/40 border border-white/10 text-xs text-slate-200 placeholder:text-slate-600 w-44"
            />
          </div>
          <button
            onClick={() => setOnlyAlerting((v) => !v)}
            className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-[11px] font-bold transition-all border ${
              onlyAlerting ? 'bg-rose-500/15 text-rose-200 border-rose-500/30' : 'bg-white/5 text-slate-400 border-white/10 hover:bg-white/10'
            }`}
          >
            <AlertCircle className="w-3.5 h-3.5" /> Só com alerta
          </button>
          {staleCount > 0 && (
            <button
              onClick={() => setHideStale((v) => !v)}
              title="Dispositivos que o agente não vê há mais de 24h (possivelmente desativados)"
              className={`flex items-center gap-1.5 px-2.5 py-1.5 rounded-md text-[11px] font-bold transition-all border ${
                hideStale ? 'bg-amber-500/15 text-amber-200 border-amber-500/30' : 'bg-white/5 text-slate-400 border-white/10 hover:bg-white/10'
              }`}
            >
              <WifiOff className="w-3.5 h-3.5" /> Ocultar obsoletos ({staleCount})
            </button>
          )}
          <select
            value={originFilter}
            onChange={(e) => setOriginFilter(e.target.value as typeof originFilter)}
            className="px-2 py-1.5 rounded-md bg-black/40 border border-white/10 text-[11px] font-bold text-slate-300"
          >
            <option value="all">Todas as origens</option>
            <option value="both">Descoberto + telemetria</option>
            <option value="discovery">Descoberto (SNMP)</option>
            <option value="telemetry">Telemetria (alertas)</option>
            <option value="neighbor">Vizinho</option>
          </select>
          <select
            value={colorMode}
            onChange={(e) => setColorMode(e.target.value as typeof colorMode)}
            className="px-2 py-1.5 rounded-md bg-black/40 border border-white/10 text-[11px] font-bold text-slate-300"
            title="Como colorir os nós"
          >
            <option value="severity">Cor: severidade</option>
            <option value="subnet">Cor: sub-rede</option>
            <option value="location">Cor: localização</option>
          </select>
          <span className="flex items-center gap-1 text-[10px] text-slate-500 uppercase font-bold ml-1"><Filter className="w-3 h-3" /> Tipos:</span>
          {presentTiers.map((t) => (
            <button
              key={t.key}
              onClick={() => toggleTier(t.key)}
              className={`px-2 py-1 rounded-md text-[10px] font-bold transition-all border ${
                hiddenTiers.has(t.key) ? 'bg-white/[0.02] text-slate-600 border-white/5 line-through' : 'bg-cyan-500/10 text-cyan-300 border-cyan-500/20'
              }`}
            >
              {t.label}
            </button>
          ))}
        </div>
      )}

      {/* Grouping legend (T-C): colors by subnet or CMDB location. */}
      {colorMode !== 'severity' && groups.length > 0 && (
        <div className="mb-3 flex flex-wrap items-center gap-2 text-[10px]">
          <span className="text-slate-500 uppercase font-bold">{colorMode === 'subnet' ? 'Sub-redes' : 'Localizações'}:</span>
          {groups.map((g) => (
            <span key={g} className="flex items-center gap-1.5 px-2 py-0.5 rounded-md bg-white/[0.03] border border-white/10 text-slate-300">
              <span className="w-2.5 h-2.5 rounded-full" style={{ background: groupColor(g) }} />
              {g}
            </span>
          ))}
        </div>
      )}

      <div className="flex gap-3">
        <div className={`relative flex-1 ${isFull ? 'h-[calc(100vh-13rem)]' : 'h-[480px]'} bg-black/60 rounded-xl border border-white/5 overflow-hidden`}>
          {isLoading && !data ? (
            <div className="absolute inset-0 flex items-center justify-center text-xs text-slate-500">Carregando grafo…</div>
          ) : nothingAtAll ? (
            <div className="absolute inset-0 flex items-center justify-center">
              <DiscoveryOnboarding onConfigureDiscovery={onConfigureDiscovery} hasAgent={!!status && status.agent_count > 0} hasTargets={!!status && status.discovery_targets > 0} />
            </div>
          ) : nodes.length === 0 ? (
            <div className="absolute inset-0 flex flex-col items-center justify-center gap-2 text-slate-500 px-6 text-center">
              <ServerCog className="w-8 h-8 text-slate-600" />
              <p className="text-sm font-bold text-slate-400">Nenhum ativo com os filtros atuais</p>
              <p className="text-[11px] max-w-md">Ajuste a busca ou os filtros de tipo/origem acima.</p>
            </div>
          ) : (
            <>
              <svg
                ref={svgRef}
                width="100%"
                height="100%"
                viewBox={`0 0 ${VBW} ${VBH}`}
                preserveAspectRatio="xMidYMid meet"
                onMouseDown={onPanStart}
                onMouseMove={onPanMove}
                onMouseUp={onPanEnd}
                onMouseLeave={onPanEnd}
                className="block"
                style={{ cursor: draggingRef.current ? 'grabbing' : 'grab', userSelect: 'none', touchAction: 'none' }}
              >
                <defs>
                  <pattern id="topo-grid" width="24" height="24" patternUnits="userSpaceOnUse">
                    <path d="M 24 0 L 0 0 0 24" fill="none" stroke="rgba(255,255,255,0.015)" strokeWidth="1" />
                  </pattern>
                </defs>
                <rect x={0} y={0} width={VBW} height={VBH} fill="url(#topo-grid)" />

                <g transform={`translate(${tf.x} ${tf.y}) scale(${tf.k})`}>
                  {/* Tier band labels */}
                  {layout.tiers.map((t) => (
                    <text key={t.key} x={10} y={t.y - 26} className="fill-slate-600 font-bold uppercase" fontSize="8" letterSpacing="1">
                      {t.label}
                    </text>
                  ))}

                  {/* Physical edges (LLDP/CDP) */}
                  {edges.map((e, i) => {
                    const s = layout.pos[e.source];
                    const t = layout.pos[e.target];
                    if (!s || !t) return null;
                    const active = selectedId === e.source || selectedId === e.target;
                    return (
                      <g key={`e-${i}`}>
                        <line
                          x1={s.x}
                          y1={s.y}
                          x2={t.x}
                          y2={t.y}
                          stroke={active ? 'rgba(56,189,248,0.8)' : 'rgba(56,189,248,0.28)'}
                          strokeWidth={active ? 2 : 1.25}
                        />
                        <title>{`${nodesById[e.source]?.label} (${e.local_port || '?'}) ↔ ${nodesById[e.target]?.label} (${e.remote_port || '?'}) · ${e.protocol.toUpperCase()}`}</title>
                      </g>
                    );
                  })}

                  {/* Nodes */}
                  {nodes.map((node) => {
                    const p = layout.pos[node.id];
                    if (!p) return null;
                    const color = nodeColor(node);
                    // When grouping (T-C), the node border takes the group color while the pulsing ring
                    // still signals open-alert severity — so you see site/subnet AND danger at once.
                    const stroke = colorMode === 'severity' ? color.stroke : groupColor(groupKeyOf(node, colorMode));
                    const label = node.label.length > 15 ? node.label.slice(0, 14) + '…' : node.label;
                    const isSel = selectedId === node.id;
                    return (
                      <g
                        key={node.id}
                        className="cursor-pointer"
                        opacity={node.stale ? 0.4 : 1}
                        onClick={() => {
                          if (didDragRef.current) return; // suppress selection after a pan drag
                          setSelectedId(isSel ? null : node.id);
                        }}
                      >
                        <title>{`${node.label}\n${originLabel(node.origin)}${node.vendor ? ` · ${node.vendor}` : ''}${node.kind ? ` · ${node.kind}` : ''}${node.stale ? ' · OBSOLETO (sem contato recente)' : ''}`}</title>
                        {node.unresolved_alerts > 0 && (
                          <circle cx={p.x} cy={p.y} r="26" className="fill-none animate-ping" stroke={color.ring} strokeWidth="1" />
                        )}
                        {isSel && <circle cx={p.x} cy={p.y} r="27" className="fill-none" stroke="#e2e8f0" strokeWidth="1.5" strokeDasharray="2 2" />}
                        <circle cx={p.x} cy={p.y} r="21" fill={color.fill} stroke={stroke} strokeWidth="2" strokeDasharray={node.origin === 'neighbor' ? '3 2' : undefined} />
                        <text x={p.x} y={p.y - 1} className="fill-slate-100 font-bold" fontSize="8" textAnchor="middle">{label}</text>
                        <text x={p.x} y={p.y + 9} className="fill-slate-400" fontSize="6.5" textAnchor="middle">
                          {node.unresolved_alerts > 0 ? `${node.unresolved_alerts} aberto(s)` : kindLabel(node.kind)}
                        </text>
                      </g>
                    );
                  })}
                </g>
              </svg>

              {/* Zoom controls */}
              <div className="absolute top-3 right-3 flex flex-col gap-1">
                <button onClick={() => zoomAround(1.2, VBW / 2, VBH / 2)} title="Aproximar" className="p-1.5 rounded-md bg-black/60 border border-white/10 text-slate-300 hover:bg-white/10 transition-all">
                  <ZoomIn className="w-3.5 h-3.5" />
                </button>
                <button onClick={() => zoomAround(1 / 1.2, VBW / 2, VBH / 2)} title="Afastar" className="p-1.5 rounded-md bg-black/60 border border-white/10 text-slate-300 hover:bg-white/10 transition-all">
                  <ZoomOut className="w-3.5 h-3.5" />
                </button>
                <button onClick={fitView} title="Ajustar à tela" className="p-1.5 rounded-md bg-black/60 border border-white/10 text-slate-300 hover:bg-white/10 transition-all">
                  <Maximize2 className="w-3.5 h-3.5" />
                </button>
              </div>
            </>
          )}
          <div className="absolute bottom-3 left-4 text-[10px] text-slate-500 bg-black/60 border border-white/5 px-2.5 py-1 rounded-md pointer-events-none">
            💡 <em>Arraste para mover · role para dar zoom · clique num ativo para detalhes. Linhas = enlaces LLDP/CDP.</em>
          </div>
        </div>

        {/* Node drill-down panel */}
        {selected && (
          <NodeDetailPanel
            node={selected}
            edges={allEdges}
            nodesById={nodesById}
            onClose={() => setSelectedId(null)}
            onFilterAlerts={() => onSearchTermChange(selected.label)}
          />
        )}
      </div>
    </div>
  );
}

// NodeDetailPanel shows the drill-down for one asset: identity, origin/severity, physical neighbours,
// and a cross-link to scope the Alerts tab to it.
function NodeDetailPanel({
  node,
  edges,
  nodesById,
  onClose,
  onFilterAlerts,
}: {
  node: GraphNode;
  edges: GraphEdge[];
  nodesById: Record<string, GraphNode>;
  onClose: () => void;
  onFilterAlerts: () => void;
}) {
  const neighbours = edges
    .filter((e) => e.source === node.id || e.target === node.id)
    .map((e) => {
      const otherId = e.source === node.id ? e.target : e.source;
      const localPort = e.source === node.id ? e.local_port : e.remote_port;
      const remotePort = e.source === node.id ? e.remote_port : e.local_port;
      return { label: nodesById[otherId]?.label ?? otherId, protocol: e.protocol, localPort, remotePort };
    });

  return (
    <div className="w-64 shrink-0 bg-black/50 rounded-xl border border-white/10 p-4 flex flex-col gap-3 max-h-[520px] overflow-auto">
      <div className="flex items-start justify-between gap-2">
        <div>
          <p className="text-sm font-extrabold text-slate-100 break-all">{node.label}</p>
          <p className="text-[10px] text-slate-500 font-mono">{node.id}</p>
        </div>
        <button onClick={onClose} className="p-1 rounded text-slate-500 hover:text-slate-200 hover:bg-white/10">
          <X className="w-3.5 h-3.5" />
        </button>
      </div>

      <div className="flex flex-wrap gap-1.5">
        <Chip>{originLabel(node.origin)}</Chip>
        <Chip>{kindLabel(node.kind)}</Chip>
        {node.vendor && <Chip>{node.vendor}</Chip>}
      </div>

      {node.stale && (
        <div className="p-2.5 rounded-lg bg-amber-950/20 border border-amber-500/25 flex items-start gap-2">
          <WifiOff className="w-3.5 h-3.5 mt-0.5 shrink-0 text-amber-400" />
          <p className="text-[11px] text-amber-200/90">
            <strong>Obsoleto</strong> — o agente não vê este dispositivo {node.last_seen ? relativeTime(node.last_seen) : 'há um tempo'}. Pode ter sido desativado.
          </p>
        </div>
      )}

      {node.unresolved_alerts > 0 ? (
        <div className="p-2.5 rounded-lg bg-rose-950/20 border border-rose-500/25">
          <p className="text-[11px] font-bold text-rose-300">{node.unresolved_alerts} alerta(s) em aberto</p>
          <p className="text-[10px] text-rose-400/80 uppercase">pior severidade: {node.worst_severity || '—'}</p>
        </div>
      ) : (
        <div className="p-2.5 rounded-lg bg-emerald-950/15 border border-emerald-500/20">
          <p className="text-[11px] font-bold text-emerald-300">Sem alertas em aberto</p>
        </div>
      )}

      <div>
        <p className="text-[10px] font-bold text-slate-500 uppercase tracking-widest mb-1">Vizinhos físicos ({neighbours.length})</p>
        {neighbours.length === 0 ? (
          <p className="text-[11px] text-slate-600">Nenhum enlace LLDP/CDP descoberto.</p>
        ) : (
          <ul className="flex flex-col gap-1">
            {neighbours.map((nb, i) => (
              <li key={i} className="text-[11px] text-slate-300 flex flex-col bg-white/[0.03] rounded-md px-2 py-1">
                <span className="font-bold text-slate-200">{nb.label}</span>
                <span className="text-[9px] text-slate-500 uppercase">
                  {nb.protocol} · {nb.localPort || '?'} ↔ {nb.remotePort || '?'}
                </span>
              </li>
            ))}
          </ul>
        )}
      </div>

      <button
        onClick={onFilterAlerts}
        className="mt-auto flex items-center justify-center gap-1.5 px-3 py-2 rounded-lg text-xs font-bold text-cyan-100 bg-cyan-500/15 hover:bg-cyan-500/25 border border-cyan-500/30 transition-all"
      >
        <Search className="w-3.5 h-3.5" /> Filtrar alertas por este ativo
      </button>
    </div>
  );
}

function Chip({ children }: { children: React.ReactNode }) {
  return <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-white/5 text-slate-300 border border-white/10">{children}</span>;
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
        {status.last_seen && <span className="text-slate-500">Último contato: {relativeTime(status.last_seen)}</span>}
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

// ---- layout ----

// TIERS orders device roles top-to-bottom: perimeter → core → access → hosts → unmanaged neighbours.
const TIERS: { key: string; label: string }[] = [
  { key: 'perimeter', label: 'Perímetro (firewall)' },
  { key: 'routing', label: 'Roteamento' },
  { key: 'core', label: 'Comutação (switch)' },
  { key: 'access', label: 'Acesso / servidores' },
  { key: 'host', label: 'Hosts / telemetria' },
  { key: 'neighbor', label: 'Vizinhos (não gerenciados)' },
];

function tierKey(kind: string): string {
  switch (kind) {
    case 'firewall':
      return 'perimeter';
    case 'router':
      return 'routing';
    case 'switch':
      return 'core';
    case 'access_point':
    case 'server':
    case 'hypervisor':
    case 'network_device':
      return 'access';
    case 'neighbor':
      return 'neighbor';
    case 'host':
    case 'endpoint':
      return 'host';
    default:
      return 'access';
  }
}

type Layout = { pos: Record<string, { x: number; y: number }>; width: number; height: number; tiers: { key: string; label: string; y: number }[] };

// computeLayout places nodes in horizontal bands by tier, wrapping wide tiers into sub-rows. Within a
// tier, nodes are ordered by a barycenter (median) heuristic over the physical edges so connected
// nodes sit near each other and edge crossings drop — falling back to open-alerts/label for a stable,
// deterministic picture. Connectivity-aware ordering is T-A.
function computeLayout(nodes: GraphNode[], edges: GraphEdge[]): Layout {
  const perRow = 8;
  const dx = 108;
  const dyRow = 96;
  const bandGap = 34;
  const topMargin = 48;

  // Adjacency over the visible edges (both directions) for the barycenter passes.
  const adj: Record<string, string[]> = {};
  for (const e of edges) {
    (adj[e.source] ||= []).push(e.target);
    (adj[e.target] ||= []).push(e.source);
  }

  const byTier = new Map<string, GraphNode[]>();
  for (const n of nodes) {
    const k = tierKey(n.kind);
    if (!byTier.has(k)) byTier.set(k, []);
    byTier.get(k)!.push(n);
  }
  // Seed order: most-alerting first, then label (deterministic).
  byTier.forEach((arr) => {
    arr.sort((a, b) => (b.unresolved_alerts - a.unresolved_alerts) || a.label.localeCompare(b.label));
  });

  // Normalized column index (0..1) of each node within its tier — the coordinate the barycenter uses.
  const colIndex: Record<string, number> = {};
  const recomputeCol = () => {
    byTier.forEach((arr) => {
      arr.forEach((node, i) => {
        colIndex[node.id] = arr.length <= 1 ? 0.5 : i / (arr.length - 1);
      });
    });
  };
  recomputeCol();

  // A few barycenter sweeps: reorder each tier by the mean column of its neighbours (in any tier).
  // Nodes with no edges keep their seed position (barycenter = their own current index).
  for (let iter = 0; iter < 4; iter++) {
    byTier.forEach((arr) => {
      const bary: Record<string, number> = {};
      for (const node of arr) {
        const nbrs = adj[node.id];
        let sum = 0;
        let cnt = 0;
        if (nbrs) {
          for (const m of nbrs) {
            if (colIndex[m] !== undefined) {
              sum += colIndex[m];
              cnt++;
            }
          }
        }
        bary[node.id] = cnt ? sum / cnt : colIndex[node.id];
      }
      arr.sort(
        (a, b) =>
          (bary[a.id] - bary[b.id]) ||
          (b.unresolved_alerts - a.unresolved_alerts) ||
          a.label.localeCompare(b.label),
      );
    });
    recomputeCol();
  }

  const pos: Record<string, { x: number; y: number }> = {};
  const tierBands: { key: string; label: string; y: number }[] = [];
  let y = topMargin;
  let maxRowW = 0;

  for (const tier of TIERS) {
    const arr = byTier.get(tier.key);
    if (!arr || arr.length === 0) continue;
    tierBands.push({ key: tier.key, label: tier.label, y });
    const rows = Math.ceil(arr.length / perRow);
    for (let r = 0; r < rows; r++) {
      const rowNodes = arr.slice(r * perRow, (r + 1) * perRow);
      const rowW = rowNodes.length * dx;
      maxRowW = Math.max(maxRowW, rowW);
      const startX = -rowW / 2 + dx / 2;
      rowNodes.forEach((n, i) => {
        pos[n.id] = { x: startX + i * dx, y };
      });
      y += dyRow;
    }
    y += bandGap;
  }

  const width = Math.max(700, maxRowW + 90);
  const height = Math.max(440, y + 10);
  const cx = width / 2;
  for (const id in pos) pos[id].x += cx;

  return { pos, width, height, tiers: tierBands };
}

function agentState(s: TopologyStatus): { connected: boolean; label: string; className: string } {
  if (s.agent_count === 0) return { connected: false, label: 'Nenhum agente conectado', className: 'text-slate-400' };
  if (s.agent_connected) return { connected: true, label: 'Agente conectado', className: 'text-emerald-300' };
  return { connected: false, label: 'Agente sem contato recente', className: 'text-amber-300' };
}

function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '—';
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (secs < 60) return `há ${secs}s`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `há ${mins} min`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `há ${hours}h`;
  return `há ${Math.floor(hours / 24)}d`;
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
  if (node.origin === 'neighbor') return { fill: '#0b0f19', stroke: '#64748b', ring: '#64748b' };
  return { fill: '#0f172a', stroke: '#10b981', ring: '#10b981' };
}

// ---- grouping (T-C): color nodes by subnet (from IPv4 id) or CMDB location ----

const GROUP_PALETTE = ['#38bdf8', '#a78bfa', '#34d399', '#fbbf24', '#f472b6', '#22d3ee', '#c084fc', '#4ade80', '#facc15', '#fb7185'];

// subnetOf returns the /24 of an IPv4 node id (device nodes are keyed by IP), or null for hostnames.
function subnetOf(id: string): string | null {
  const m = id.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.\d{1,3}$/);
  return m ? `${m[1]}.${m[2]}.${m[3]}.0/24` : null;
}

function groupKeyOf(node: GraphNode, mode: 'subnet' | 'location'): string {
  if (mode === 'subnet') return subnetOf(node.id) ?? 'sem IP';
  return node.location || 'sem localização';
}

// groupColor maps a group key to a stable palette color; the "unknown" buckets get a neutral grey.
function groupColor(key: string): string {
  if (key === 'sem IP' || key === 'sem localização') return '#64748b';
  let h = 0;
  for (let i = 0; i < key.length; i++) h = (h * 31 + key.charCodeAt(i)) >>> 0;
  return GROUP_PALETTE[h % GROUP_PALETTE.length];
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
    case 'hypervisor':
      return 'hypervisor';
    case 'host':
      return 'host';
    case 'endpoint':
      return 'endpoint';
    case 'neighbor':
      return 'vizinho';
    default:
      return 'rede';
  }
}
