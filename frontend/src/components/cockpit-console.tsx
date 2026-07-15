'use client';

import { useEffect, useState } from 'react';
import Link from 'next/link';
import { Activity, Archive, LineChart, Network, Settings, Siren, Target } from 'lucide-react';
import { useAuth } from '@/lib/auth-context';
import { useTenantSelection } from '@/lib/tenant-context';
import { useAlertsSocket } from '@/lib/use-alerts-socket';
import { usePendingApprovalsCount } from '@/lib/use-pending-approvals-count';
import { usePendingResponseCount } from '@/lib/use-pending-response-count';
import { domainForSource, type ConsoleMode } from '@/lib/domain';
import { alertComparator, isOpen, withinLens, type TimeLens, type AlertSortKey } from '@/lib/alert-priority';
import { AppHeader } from '@/components/app-header';
import { AlertStatCards } from '@/components/alerts/alert-stat-cards';
import { AlertsSearchBar } from '@/components/alerts/alerts-search-bar';
import { AlertsTable } from '@/components/alerts/alerts-table';
import { AlertDetailSheet } from '@/components/alerts/alert-detail-sheet';
import { LegacyCockpitPanels } from '@/components/legacy-cockpit-panels';
import { IncidentsView } from '@/components/incidents/incidents-view';
import { HistoryView } from '@/components/history/history-view';
import { MetricsView } from '@/components/metrics/metrics-view';
import { GlobalSearchPalette } from '@/components/global-search-palette';
import type { Alert, AlertSeverity, AlertStatus, SearchAlertResult, SearchTenantResult } from '@/types';

type CockpitTab = 'alerts' | 'incidents' | 'history' | 'metrics' | 'topology' | 'settings';
type SeverityFilterValue = 'all' | 'fatal' | 'critical' | 'warning' | 'info';

const TAB_BUTTON_CLASS = (active: boolean) =>
  `pb-2 px-3 text-xs uppercase tracking-wider font-bold border-b-2 transition-all flex items-center gap-1.5 cursor-pointer ${
    active ? 'border-violet-500 text-white' : 'border-transparent text-slate-400 hover:text-slate-200'
  }`;

const LINK_CHIP = 'px-2.5 py-1 rounded-lg border bg-white/[0.02] border-white/10 text-slate-500 hover:text-slate-300 transition-all';

// The cockpit body, rendered by three routes: `/` (unified, all domains — no toggle), `/noc` and `/soc`
// (dedicated consoles, B9). The domain is a property of the ROUTE, not an in-page control: `/` always
// shows every domain, and NOC/SOC are reached only via their own bookmarkable URLs, so a segregated
// team lands already scoped and can't accidentally switch. Each route links across to the others.
export function CockpitConsole({ lockedConsole }: { lockedConsole?: 'noc' | 'soc' }) {
  const { token, user } = useAuth();
  const { tenants, selectedTenantIds, setSelectedTenantIds } = useTenantSelection();
  const { alerts, setAlerts, connStatus } = useAlertsSocket(token, selectedTenantIds);
  const { count: pendingApprovals, refetch: refetchApprovals } = usePendingApprovalsCount(token);
  const { count: pendingResponses } = usePendingResponseCount(token);
  const pendingSettingsCount = pendingApprovals + pendingResponses;

  const [cockpitTab, setCockpitTab] = useState<CockpitTab>('alerts');
  // Domain comes from the route: `/` is always all-domains; `/noc` and `/soc` lock to their domain.
  const consoleMode: ConsoleMode = lockedConsole ?? 'all';
  const [timeLens, setTimeLens] = useState<TimeLens>('all');
  const [sortKey, setSortKey] = useState<AlertSortKey>('priority');
  const [searchTerm, setSearchTerm] = useState('');
  const [severityFilter, setSeverityFilter] = useState<SeverityFilterValue>('all');
  const [selectedAlert, setSelectedAlert] = useState<Alert | null>(null);
  const [isSearchOpen, setIsSearchOpen] = useState(false);

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.metaKey || e.ctrlKey) && e.key.toLowerCase() === 'k') {
        e.preventDefault();
        setIsSearchOpen((prev) => !prev);
      }
    };
    window.addEventListener('keydown', handleKeyDown);
    return () => window.removeEventListener('keydown', handleKeyDown);
  }, []);

  // Segregated NOC/SOC console: when a domain is selected, every view (stat cards, alerts, incidents)
  // shows only that domain's data, derived from each alert's source.
  const inConsole = (a: Alert) => consoleMode === 'all' || domainForSource(a.ai_analysis?.source as string | undefined) === consoleMode;
  const consoleDomain = consoleMode === 'all' ? undefined : consoleMode;

  const baseStat = (a: Alert) => selectedTenantIds.includes(a.tenant_id) && inConsole(a) && isOpen(a);
  const stats = {
    total: alerts.filter(baseStat).length,
    fatal: alerts.filter((a) => baseStat(a) && a.severity === 'fatal').length,
    critical: alerts.filter((a) => baseStat(a) && a.severity === 'critical').length,
    warning: alerts.filter((a) => baseStat(a) && a.severity === 'warning').length,
    info: alerts.filter((a) => baseStat(a) && a.severity === 'info').length,
  };

  // Operational console: the working set is OPEN alerts only (resolved/suppressed live in the History
  // view), matching the search/severity/tenant/console filters. Computed WITHOUT the time lens so we
  // can both narrow it and report how many open alerts the lens hides.
  const consoleWorkingSet = alerts.filter((a) => {
    const matchesSearch =
      a.summary.toLowerCase().includes(searchTerm.toLowerCase()) ||
      a.event_type.toLowerCase().includes(searchTerm.toLowerCase()) ||
      ((a.ai_analysis?.source as string) || '').toLowerCase().includes(searchTerm.toLowerCase());
    const matchesSeverity = severityFilter === 'all' || a.severity === severityFilter;
    const matchesTenant = selectedTenantIds.includes(a.tenant_id);
    return matchesSearch && matchesSeverity && matchesTenant && isOpen(a) && inConsole(a);
  });

  // The time lens is a convenience narrowing by age; 'all' (default) never hides an open alert. The
  // sort key defaults to 'priority' (severity → SLA burn → recency) but the operator can re-sort.
  const filteredAlerts = consoleWorkingSet.filter((a) => withinLens(a, timeLens)).sort(alertComparator(sortKey));
  const hiddenByLens = consoleWorkingSet.length - filteredAlerts.length;

  useEffect(() => {
    const latest = alerts[0];
    if (latest && (latest.severity === 'fatal' || latest.severity === 'critical')) {
      refetchApprovals();
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [alerts[0]?.id]);

  const handleSearchSelectAlert = (result: SearchAlertResult) => {
    const existing = alerts.find((a) => a.id === result.id);
    setSelectedAlert(
      existing || {
        id: result.id,
        tenant_id: result.tenant_id,
        event_type: '',
        severity: result.severity as AlertSeverity,
        status: 'triggered',
        summary: result.summary,
        payload: {},
        created_at: result.created_at,
        updated_at: result.created_at,
      }
    );
    setCockpitTab('alerts');
  };

  const handleSearchSelectTenant = (result: SearchTenantResult) => {
    setSelectedTenantIds([result.id]);
  };

  const handleSearchSelectRunbook = () => {
    setCockpitTab('settings');
  };

  const handleStatusChange = (alertId: string, newStatus: AlertStatus) => {
    const now = new Date().toISOString();
    setAlerts((prev) =>
      prev.map((a) =>
        a.id === alertId
          ? {
              ...a,
              status: newStatus,
              resolved_at: newStatus === 'resolved' ? now : a.resolved_at,
              acknowledged_at: newStatus === 'acknowledged' ? now : a.acknowledged_at,
              updated_at: now,
            }
          : a
      )
    );
    setSelectedAlert((prev) =>
      prev && prev.id === alertId
        ? {
            ...prev,
            status: newStatus,
            resolved_at: newStatus === 'resolved' ? now : prev.resolved_at,
            acknowledged_at: newStatus === 'acknowledged' ? now : prev.acknowledged_at,
            updated_at: now,
          }
        : prev
    );
  };

  return (
    <div className="min-h-screen bg-background text-slate-100 flex flex-col">
      <AppHeader connStatus={connStatus} />

      <main className="flex-1 flex overflow-hidden">
        <section className="flex-1 flex flex-col p-6 overflow-y-auto gap-6 w-full max-w-7xl mx-auto">
          <div className="flex border-b border-white/5 gap-2 pb-1">
            <button onClick={() => setCockpitTab('alerts')} className={TAB_BUTTON_CLASS(cockpitTab === 'alerts')}>
              <Activity className="w-3.5 h-3.5" />
              Painel de Alertas
            </button>
            <button onClick={() => setCockpitTab('incidents')} className={TAB_BUTTON_CLASS(cockpitTab === 'incidents')}>
              <Siren className="w-3.5 h-3.5" />
              Incidentes
            </button>
            <button onClick={() => setCockpitTab('history')} className={TAB_BUTTON_CLASS(cockpitTab === 'history')}>
              <Archive className="w-3.5 h-3.5" />
              Histórico
            </button>
            <button onClick={() => setCockpitTab('metrics')} className={TAB_BUTTON_CLASS(cockpitTab === 'metrics')}>
              <LineChart className="w-3.5 h-3.5" />
              Métricas SNMP
            </button>
            <button onClick={() => setCockpitTab('topology')} className={TAB_BUTTON_CLASS(cockpitTab === 'topology')}>
              <Network className="w-3.5 h-3.5" />
              Topologia CMDB &amp; Ativos
            </button>
            {(user?.role === 'admin' || user?.role === 'operator') && (
              <button onClick={() => setCockpitTab('settings')} className={TAB_BUTTON_CLASS(cockpitTab === 'settings')}>
                <Settings className="w-3.5 h-3.5" />
                Configuração MSP
                {pendingSettingsCount > 0 && (
                  <span className="ml-1 px-1.5 py-0.5 rounded-full bg-rose-500/20 border border-rose-500/40 text-rose-400 text-[9px] font-bold leading-none">
                    {pendingSettingsCount}
                  </span>
                )}
              </button>
            )}
          </div>

          {/* Console selector / dedicated-console nav (B9), plus the time lens and sort controls. */}
          <div className="flex flex-wrap items-center gap-2 gap-y-2 text-[10px] uppercase font-bold tracking-wider">
            <span className="text-slate-500">Console</span>
            {lockedConsole ? (
              <>
                <span
                  className={`px-3 py-1 rounded-lg border ${
                    lockedConsole === 'soc' ? 'bg-rose-500/20 border-rose-500/50 text-rose-300' : 'bg-sky-500/20 border-sky-500/50 text-sky-300'
                  }`}
                >
                  {lockedConsole === 'soc' ? 'SOC · Segurança' : 'NOC · Rede & Disponibilidade'}
                </span>
                <Link href={lockedConsole === 'soc' ? '/noc' : '/soc'} className={LINK_CHIP}>
                  {lockedConsole === 'soc' ? 'NOC ↗' : 'SOC ↗'}
                </Link>
                <Link href="/" className={LINK_CHIP}>
                  Unificado ↗
                </Link>
              </>
            ) : (
              <>
                <span className="px-3 py-1 rounded-lg border bg-white/10 border-white/20 text-slate-100">
                  Unificado · Todos os domínios
                </span>
                <Link href="/noc" className={LINK_CHIP} title="Console NOC dedicado (rede &amp; disponibilidade)">
                  NOC ↗
                </Link>
                <Link href="/soc" className={LINK_CHIP} title="Console SOC dedicado (segurança)">
                  SOC ↗
                </Link>
              </>
            )}

            {/* Time lens: convenience narrowing of the open set by age. 'Tudo' is the safe default. */}
            <span className="text-slate-500 ml-4">Janela</span>
            {([
              { id: 'all', label: 'Tudo' },
              { id: '1h', label: '1h' },
              { id: '24h', label: '24h' },
              { id: '7d', label: '7d' },
            ] as { id: TimeLens; label: string }[]).map((w) => (
              <button
                key={w.id}
                onClick={() => setTimeLens(w.id)}
                className={`px-3 py-1 rounded-lg border transition-all cursor-pointer ${
                  timeLens === w.id
                    ? 'bg-white/10 border-white/20 text-slate-100'
                    : 'bg-white/[0.02] border-white/10 text-slate-500 hover:text-slate-300'
                }`}
              >
                {w.label}
              </button>
            ))}

            {/* Sort selector: how the alerts list is ordered. 'Prioridade' is the default. */}
            <span className="text-slate-500 ml-4">Ordenar</span>
            {([
              { id: 'priority', label: 'Prioridade' },
              { id: 'sla', label: 'SLA' },
              { id: 'recent', label: 'Recentes' },
              { id: 'oldest', label: 'Antigos' },
            ] as { id: AlertSortKey; label: string }[]).map((s) => (
              <button
                key={s.id}
                onClick={() => setSortKey(s.id)}
                className={`px-3 py-1 rounded-lg border transition-all cursor-pointer ${
                  sortKey === s.id
                    ? 'bg-white/10 border-white/20 text-slate-100'
                    : 'bg-white/[0.02] border-white/10 text-slate-500 hover:text-slate-300'
                }`}
              >
                {s.label}
              </button>
            ))}
          </div>

          {selectedTenantIds.length === 1 && (
            <div className="p-3 rounded-xl bg-violet-950/20 border border-violet-500/35 flex items-center justify-between text-xs text-violet-300">
              <div className="flex items-center gap-2">
                <Target className="w-4 h-4 text-violet-400 animate-pulse animate-duration-1000" />
                <span>
                  Modo de Foco Ativo: Monitorando apenas o tenant{' '}
                  <strong>{tenants.find((t) => t.id === selectedTenantIds[0])?.name || 'Cliente Selecionado'}</strong>
                </span>
              </div>
              <button
                onClick={() => setSelectedTenantIds(tenants.map((t) => t.id))}
                className="px-3 py-1 rounded bg-violet-500/20 hover:bg-violet-500/35 text-violet-300 hover:text-white transition-all font-bold uppercase text-[9px] cursor-pointer"
              >
                Ver Todos os Clientes
              </button>
            </div>
          )}

          {cockpitTab === 'alerts' && (
            <>
              <AlertStatCards stats={stats} severityFilter={severityFilter} onSelectFilter={setSeverityFilter} />
              <AlertsSearchBar value={searchTerm} onChange={setSearchTerm} />
              {hiddenByLens > 0 && (
                <button
                  onClick={() => setTimeLens('all')}
                  className="flex items-center justify-between gap-2 px-3 py-2 rounded-lg bg-amber-500/10 border border-amber-500/30 text-amber-300 text-xs hover:bg-amber-500/20 transition-all cursor-pointer text-left"
                >
                  <span>
                    <strong>{hiddenByLens}</strong> alerta{hiddenByLens > 1 ? 's' : ''} aberto{hiddenByLens > 1 ? 's' : ''} mais
                    antigo{hiddenByLens > 1 ? 's' : ''} oculto{hiddenByLens > 1 ? 's' : ''} pela janela de tempo.
                  </span>
                  <span className="font-bold uppercase tracking-wider whitespace-nowrap">Ver todos →</span>
                </button>
              )}
              <AlertsTable
                alerts={filteredAlerts}
                tenants={tenants}
                selectedAlertId={selectedAlert?.id}
                onSelectAlert={setSelectedAlert}
                onFocusTenant={(tenantId) => setSelectedTenantIds([tenantId])}
              />
            </>
          )}

          {cockpitTab === 'incidents' && (
            <IncidentsView tenantId={selectedTenantIds.length === 1 ? selectedTenantIds[0] : undefined} domain={consoleDomain} />
          )}

          {cockpitTab === 'history' && (
            <HistoryView tenantId={selectedTenantIds.length === 1 ? selectedTenantIds[0] : undefined} domain={consoleDomain} />
          )}

          {cockpitTab === 'metrics' && (
            <MetricsView tenantId={selectedTenantIds.length === 1 ? selectedTenantIds[0] : undefined} />
          )}

          {(cockpitTab === 'topology' || cockpitTab === 'settings') && (
            <LegacyCockpitPanels cockpitTab={cockpitTab} onSearchTermChange={setSearchTerm} onNavigateSettings={() => setCockpitTab('settings')} />
          )}
        </section>
      </main>

      {selectedAlert && (
        <AlertDetailSheet
          alert={selectedAlert}
          onOpenChange={(open) => !open && setSelectedAlert(null)}
          onStatusChange={handleStatusChange}
          userRole={user?.role}
        />
      )}

      <GlobalSearchPalette
        open={isSearchOpen}
        onOpenChange={setIsSearchOpen}
        tenantIds={selectedTenantIds}
        onSelectAlert={handleSearchSelectAlert}
        onSelectTenant={handleSearchSelectTenant}
        onSelectRunbook={handleSearchSelectRunbook}
      />
    </div>
  );
}
