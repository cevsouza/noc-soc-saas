'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { AlertTriangle, Building2, RefreshCw, Server, ShieldAlert, Users, Zap } from 'lucide-react';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { PlatformUsage, TenantUsage } from '@/types';
import { PLAN_NAMES, formatLimit } from '@/types';

// Control-plane dashboard (Backlog B2 fatia 1): the MSSP-wide usage roll-up over GET
// /api/v1/admin/usage — every tenant's metered usage plus platform totals. Platform-admin only
// (the endpoint is RequireGlobalRole-gated; the sidebar button is gated on global_role too). This is
// the metering basis the future Stripe billing (B2 fatias 2+) will price against.

type SortKey = 'alerts_in_window' | 'open_incidents' | 'active_integrations' | 'active_users' | 'total_alerts_stored';

const SORT_LABELS: Record<SortKey, string> = {
  alerts_in_window: 'Alertas (30d)',
  open_incidents: 'Incidentes abertos',
  active_integrations: 'Integrações',
  active_users: 'Usuários',
  total_alerts_stored: 'Total armazenado',
};

function fmt(n: number): string {
  return new Intl.NumberFormat('pt-BR').format(Math.round(n));
}

export function ControlPlanePanel() {
  const [data, setData] = useState<PlatformUsage | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [sortKey, setSortKey] = useState<SortKey>('alerts_in_window');
  const [savingPlan, setSavingPlan] = useState<string | null>(null);

  const fetchUsage = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const d = await apiFetchJson<PlatformUsage>('/api/v1/admin/usage');
      setData(d);
    } catch (err) {
      console.error('Failed to fetch platform usage:', err);
      setError('Não foi possível carregar o uso da plataforma. Requer papel de administrador de plataforma (MSSP).');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchUsage();
  }, [fetchUsage]);

  // changePlan assigns a named plan to a tenant (control-plane PUT), then refetches so the
  // usage-vs-limit rendering reflects the new quotas. The endpoint fills preset limits server-side.
  const changePlan = useCallback(
    async (tenantId: string, planName: string) => {
      setSavingPlan(tenantId);
      try {
        const res = await apiFetch('/api/v1/admin/plans', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ tenant_id: tenantId, plan_name: planName }),
        });
        if (!res.ok) throw new Error(`PUT plan failed: ${res.status}`);
        await fetchUsage();
      } catch (err) {
        console.error('Failed to change plan:', err);
      } finally {
        setSavingPlan(null);
      }
    },
    [fetchUsage],
  );

  const sortedTenants = useMemo(() => {
    if (!data) return [];
    return [...data.tenants].sort((a, b) => (b[sortKey] as number) - (a[sortKey] as number));
  }, [data, sortKey]);

  const chartData = useMemo(
    () => sortedTenants.slice(0, 10).map((t) => ({ name: (t.tenant_name || t.tenant_id).slice(0, 18), alertas: t.alerts_in_window })),
    [sortedTenants],
  );

  const totals: TenantUsage | null = data?.totals ?? null;

  // A tenant is over quota if any metered dimension exceeds its finite (>0) limit.
  const isOver = (t: TenantUsage): boolean =>
    (!!t.max_alerts_per_month && t.max_alerts_per_month > 0 && t.alerts_in_window > t.max_alerts_per_month) ||
    (!!t.max_integrations && t.max_integrations > 0 && t.active_integrations > t.max_integrations) ||
    (!!t.max_users && t.max_users > 0 && t.active_users > t.max_users);
  const overLimitCount = useMemo(() => sortedTenants.filter(isOver).length, [sortedTenants]);

  // cell renders `used / limit`, coloured red when the finite limit is exceeded.
  const cell = (used: number, limit: number | undefined) => {
    const over = !!limit && limit > 0 && used > limit;
    return (
      <span className={over ? 'text-rose-400 font-bold' : 'text-slate-400'}>
        {fmt(used)} <span className="text-slate-600">/ {formatLimit(limit)}</span>
      </span>
    );
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div>
          <h3 className="text-sm font-bold text-slate-200 flex items-center gap-2">
            <Building2 className="w-4 h-4 text-cyan-400" /> Control-Plane · Uso da Plataforma (MSSP)
          </h3>
          <p className="text-[11px] text-slate-500 mt-0.5">
            Uso medido de todos os tenants nos últimos {data?.window_days ?? 30} dias — base para faturamento.
          </p>
        </div>
        <button
          onClick={fetchUsage}
          disabled={isLoading}
          className="px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 disabled:opacity-50 text-slate-300 text-[10px] font-bold uppercase tracking-wider border border-white/10 transition-all cursor-pointer flex items-center gap-1.5"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {error ? (
        <div className="p-4 rounded-lg bg-rose-950/20 border border-rose-500/20 text-rose-300 text-xs flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 shrink-0" /> {error}
        </div>
      ) : isLoading && !data ? (
        <div className="flex items-center justify-center py-10 gap-2 text-xs text-slate-500">
          <RefreshCw className="w-4 h-4 animate-spin text-cyan-500" /> Carregando uso da plataforma…
        </div>
      ) : data ? (
        <>
          {/* Totals */}
          <div className="grid grid-cols-2 md:grid-cols-4 lg:grid-cols-7 gap-3">
            <StatCard icon={<Building2 className="w-4 h-4 text-cyan-400" />} label="Tenants" value={fmt(data.tenant_count)} />
            <StatCard icon={<Zap className="w-4 h-4 text-amber-400" />} label="Alertas (30d)" value={fmt(totals?.alerts_in_window ?? 0)} />
            <StatCard icon={<Zap className="w-4 h-4 text-violet-400" />} label="EPS agregado" value={(totals?.eps ?? 0).toFixed(3)} />
            <StatCard icon={<AlertTriangle className="w-4 h-4 text-rose-400" />} label="Incidentes abertos" value={fmt(totals?.open_incidents ?? 0)} />
            <StatCard icon={<Server className="w-4 h-4 text-emerald-400" />} label="Integrações ativas" value={fmt(totals?.active_integrations ?? 0)} />
            <StatCard icon={<Users className="w-4 h-4 text-sky-400" />} label="Usuários" value={fmt(totals?.active_users ?? 0)} />
            <StatCard
              icon={<ShieldAlert className={`w-4 h-4 ${overLimitCount > 0 ? 'text-rose-400' : 'text-slate-500'}`} />}
              label="Acima do limite"
              value={fmt(overLimitCount)}
            />
          </div>

          {/* Top tenants by alert volume */}
          {chartData.length > 0 && (
            <div className="rounded-xl bg-black/30 border border-white/5 p-4">
              <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-3">Top tenants por volume de alertas (30d)</div>
              <ResponsiveContainer width="100%" height={220}>
                <BarChart data={chartData} margin={{ top: 4, right: 8, bottom: 4, left: 8 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.05)" />
                  <XAxis dataKey="name" tick={{ fontSize: 10, fill: '#94a3b8' }} interval={0} angle={-20} textAnchor="end" height={50} />
                  <YAxis tick={{ fontSize: 10, fill: '#94a3b8' }} />
                  <Tooltip contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 8, fontSize: 11 }} />
                  <Bar dataKey="alertas" fill="#22d3ee" radius={[3, 3, 0, 0]} />
                </BarChart>
              </ResponsiveContainer>
            </div>
          )}

          {/* Per-tenant table */}
          <div className="rounded-xl bg-black/30 border border-white/5 overflow-hidden">
            <div className="flex items-center justify-between px-4 py-2.5 border-b border-white/5">
              <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Por tenant ({data.tenant_count})</span>
              <div className="flex items-center gap-1.5">
                <span className="text-[10px] text-slate-600">Ordenar:</span>
                <select
                  value={sortKey}
                  onChange={(e) => setSortKey(e.target.value as SortKey)}
                  className="bg-black/40 border border-white/10 rounded px-2 py-1 text-[10px] text-slate-300 focus:outline-none"
                >
                  {(Object.keys(SORT_LABELS) as SortKey[]).map((k) => (
                    <option key={k} value={k}>{SORT_LABELS[k]}</option>
                  ))}
                </select>
              </div>
            </div>
            <div className="overflow-x-auto">
              <table className="w-full text-[11px]">
                <thead>
                  <tr className="text-slate-500 border-b border-white/5">
                    <th className="text-left font-semibold px-4 py-2">Tenant</th>
                    <th className="text-left font-semibold px-3 py-2">Plano</th>
                    <th className="text-right font-semibold px-3 py-2">Alertas 30d / limite</th>
                    <th className="text-right font-semibold px-3 py-2">Incid. abertos</th>
                    <th className="text-right font-semibold px-3 py-2">Integr. / limite</th>
                    <th className="text-right font-semibold px-3 py-2">Usuários / limite</th>
                    <th className="text-right font-semibold px-4 py-2">Armazenado</th>
                  </tr>
                </thead>
                <tbody>
                  {sortedTenants.map((t) => (
                    <tr key={t.tenant_id} className={`border-b border-white/[0.03] hover:bg-white/[0.02] ${isOver(t) ? 'bg-rose-950/10' : ''}`}>
                      <td className="text-left px-4 py-2 text-slate-300 truncate max-w-[180px]">
                        <span className="inline-flex items-center gap-1.5">
                          {isOver(t) && <ShieldAlert className="w-3 h-3 text-rose-400 shrink-0" />}
                          {t.tenant_name || t.tenant_id}
                        </span>
                      </td>
                      <td className="text-left px-3 py-2">
                        <select
                          value={t.plan_name || 'free'}
                          disabled={savingPlan === t.tenant_id}
                          onChange={(e) => changePlan(t.tenant_id, e.target.value)}
                          className="bg-black/40 border border-white/10 rounded px-2 py-1 text-[10px] text-slate-300 focus:outline-none disabled:opacity-50 capitalize"
                        >
                          {PLAN_NAMES.map((p) => (
                            <option key={p} value={p}>{p}</option>
                          ))}
                        </select>
                      </td>
                      <td className="text-right px-3 py-2">{cell(t.alerts_in_window, t.max_alerts_per_month)}</td>
                      <td className={`text-right px-3 py-2 ${t.open_incidents > 0 ? 'text-rose-400 font-bold' : 'text-slate-500'}`}>{fmt(t.open_incidents)}</td>
                      <td className="text-right px-3 py-2">{cell(t.active_integrations, t.max_integrations)}</td>
                      <td className="text-right px-3 py-2">{cell(t.active_users, t.max_users)}</td>
                      <td className="text-right px-4 py-2 text-slate-500">{fmt(t.total_alerts_stored)}</td>
                    </tr>
                  ))}
                  {sortedTenants.length === 0 && (
                    <tr>
                      <td colSpan={7} className="text-center px-4 py-6 text-slate-600">Nenhum tenant ativo.</td>
                    </tr>
                  )}
                </tbody>
              </table>
            </div>
          </div>
        </>
      ) : null}
    </div>
  );
}

function StatCard({ icon, label, value }: { icon: React.ReactNode; label: string; value: string }) {
  return (
    <div className="rounded-xl bg-black/30 border border-white/5 p-3">
      <div className="flex items-center gap-1.5 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
        {icon} <span className="truncate">{label}</span>
      </div>
      <div className="text-lg font-extrabold text-slate-100 mt-1">{value}</div>
    </div>
  );
}
