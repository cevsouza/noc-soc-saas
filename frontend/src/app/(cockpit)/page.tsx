'use client';

import { useEffect, useState } from 'react';
import { Activity, Network, Settings, Target } from 'lucide-react';
import { useAuth } from '@/lib/auth-context';
import { useTenantSelection } from '@/lib/tenant-context';
import { useAlertsSocket } from '@/lib/use-alerts-socket';
import { usePendingApprovalsCount } from '@/lib/use-pending-approvals-count';
import { usePendingResponseCount } from '@/lib/use-pending-response-count';
import { AppHeader } from '@/components/app-header';
import { AlertStatCards } from '@/components/alerts/alert-stat-cards';
import { AlertsSearchBar } from '@/components/alerts/alerts-search-bar';
import { AlertsTable } from '@/components/alerts/alerts-table';
import { AlertDetailSheet } from '@/components/alerts/alert-detail-sheet';
import { LegacyCockpitPanels } from '@/components/legacy-cockpit-panels';
import { GlobalSearchPalette } from '@/components/global-search-palette';
import type { Alert, AlertSeverity, AlertStatus, SearchAlertResult, SearchTenantResult } from '@/types';

type CockpitTab = 'alerts' | 'topology' | 'settings';
type SeverityFilterValue = 'all' | 'fatal' | 'critical' | 'warning' | 'info';

const TAB_BUTTON_CLASS = (active: boolean) =>
  `pb-2 px-3 text-xs uppercase tracking-wider font-bold border-b-2 transition-all flex items-center gap-1.5 cursor-pointer ${
    active ? 'border-violet-500 text-white' : 'border-transparent text-slate-400 hover:text-slate-200'
  }`;

// Single unified `/` route bridging the new architecture (this file) with the still-legacy
// Topology/Settings tabs (rendered via `LegacyCockpitPanels`) — same tab bar, same URL, no
// visible change for the user during the migration. See the Fase 2 plan for the full rationale.
export default function CockpitPage() {
  const { token, user } = useAuth();
  const { tenants, selectedTenantIds, setSelectedTenantIds } = useTenantSelection();
  const { alerts, setAlerts, connStatus } = useAlertsSocket(token, selectedTenantIds);
  const { count: pendingApprovals, refetch: refetchApprovals } = usePendingApprovalsCount(token);
  const { count: pendingResponses } = usePendingResponseCount(token);
  const pendingSettingsCount = pendingApprovals + pendingResponses;

  const [cockpitTab, setCockpitTab] = useState<CockpitTab>('alerts');
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

  const stats = {
    total: alerts.filter((a) => selectedTenantIds.includes(a.tenant_id) && a.status !== 'resolved' && a.status !== 'suppressed').length,
    fatal: alerts.filter((a) => selectedTenantIds.includes(a.tenant_id) && a.severity === 'fatal' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    critical: alerts.filter((a) => selectedTenantIds.includes(a.tenant_id) && a.severity === 'critical' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    warning: alerts.filter((a) => selectedTenantIds.includes(a.tenant_id) && a.severity === 'warning' && a.status !== 'resolved' && a.status !== 'suppressed').length,
    info: alerts.filter((a) => selectedTenantIds.includes(a.tenant_id) && a.severity === 'info' && a.status !== 'resolved' && a.status !== 'suppressed').length,
  };

  const filteredAlerts = alerts.filter((a) => {
    const matchesSearch =
      a.summary.toLowerCase().includes(searchTerm.toLowerCase()) ||
      a.event_type.toLowerCase().includes(searchTerm.toLowerCase()) ||
      ((a.ai_analysis?.source as string) || '').toLowerCase().includes(searchTerm.toLowerCase());
    const matchesSeverity = severityFilter === 'all' || a.severity === severityFilter;
    const matchesTenant = selectedTenantIds.includes(a.tenant_id);
    const isActive = a.status !== 'resolved' && a.status !== 'suppressed';
    return matchesSearch && matchesSeverity && matchesTenant && isActive;
  });

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
    // v1 simplification: just switch to Settings — selectedIntegrationTool (which sub-panel
    // is showing) is internal state of LegacyCockpitPanels, not worth a prop path for this.
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
              <AlertsTable
                alerts={filteredAlerts}
                tenants={tenants}
                selectedAlertId={selectedAlert?.id}
                onSelectAlert={setSelectedAlert}
                onFocusTenant={(tenantId) => setSelectedTenantIds([tenantId])}
              />
            </>
          )}

          {cockpitTab !== 'alerts' && (
            <LegacyCockpitPanels cockpitTab={cockpitTab} alerts={alerts} onSearchTermChange={setSearchTerm} />
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
