'use client';

import { useCallback, useEffect, useState } from 'react';
import { Activity, AlertOctagon, Bot, Radio, RefreshCw, ShieldHalf, Layers } from 'lucide-react';
import { Bar, BarChart, CartesianGrid, ResponsiveContainer, Tooltip, XAxis, YAxis } from 'recharts';
import { apiFetchJson } from '@/lib/api-client';
import type { OperationalStats } from '@/types';

// Tactical NOC/SOC KPI panel (Fase 6 fatia 1 backend → this fatia 2 UI). Complements the SLA
// executive report with alert-fatigue, top-offender, automation-ROI, MITRE and silent-source
// metrics. Self-contained (same style as runbook-approvals-panel / access-control-panel); accepts
// an optional tenantId so an MSP admin viewing a specific tenant sees that tenant's KPIs, exactly
// like the SLA panel does.
export function OperationalKpisPanel({ tenantId }: { tenantId?: string }) {
  const [stats, setStats] = useState<OperationalStats | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchStats = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const qs = tenantId ? `?tenant_id=${tenantId}` : '';
      const data = await apiFetchJson<OperationalStats>(`/api/v1/reports/operational/stats${qs}`);
      setStats(data);
    } catch (err) {
      console.error('Failed to fetch operational stats:', err);
      setError('Não foi possível carregar os KPIs operacionais.');
    } finally {
      setIsLoading(false);
    }
  }, [tenantId]);

  useEffect(() => {
    fetchStats();
  }, [fetchStats]);

  const windowDays = stats?.window_days ?? 30;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-emerald-400 font-extrabold uppercase text-[10px]">
          <Activity className="w-3.5 h-3.5" /> KPIs Operacionais (janela {windowDays}d)
        </div>
        <button
          onClick={fetchStats}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all disabled:opacity-50"
        >
          <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {error && (
        <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300">{error}</div>
      )}

      {!stats && isLoading && <div className="text-xs text-slate-400 py-8 text-center">Carregando…</div>}

      {stats && (
        <>
          {/* Top KPI cards */}
          <div className="grid grid-cols-2 lg:grid-cols-4 gap-3">
            <KpiCard
              icon={<Layers className="w-3.5 h-3.5 text-cyan-400" />}
              label="Fila de Triagem"
              value={`${stats.triage_backlog.triggered + stats.triage_backlog.acknowledged}`}
              sub={`${stats.triage_backlog.triggered} novos · ${stats.triage_backlog.acknowledged} em análise`}
            />
            <KpiCard
              icon={<AlertOctagon className="w-3.5 h-3.5 text-amber-400" />}
              label="Razão de Ruído"
              value={`${stats.noise_ratio.ratio.toFixed(2)}×`}
              sub={`${stats.noise_ratio.total_alerts} alertas → ${stats.noise_ratio.distinct_incidents} incidentes`}
            />
            <KpiCard
              icon={<Bot className="w-3.5 h-3.5 text-emerald-400" />}
              label="Horas Economizadas"
              value={`${stats.automation.estimated_hours_saved.toFixed(1)}h`}
              sub={`${stats.automation.soar_executed + stats.automation.response_executed} automações`}
              accent="text-emerald-400"
            />
            <KpiCard
              icon={<ShieldHalf className="w-3.5 h-3.5 text-violet-400" />}
              label="Contenções"
              value={`${stats.automation.response_executed}`}
              sub={`${stats.automation.response_failed} falharam`}
            />
          </div>

          {/* Top offenders bar chart */}
          <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
            <div className="text-[10px] font-bold uppercase text-slate-400 mb-3">Top Ofensores (por tipo de evento)</div>
            {stats.top_offenders.length > 0 ? (
              <ResponsiveContainer width="100%" height={200}>
                <BarChart data={stats.top_offenders} margin={{ top: 8, right: 8, left: 0, bottom: 8 }}>
                  <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.06)" />
                  <XAxis dataKey="event_type" tick={{ fill: '#94a3b8', fontSize: 10 }} />
                  <YAxis tick={{ fill: '#94a3b8', fontSize: 10 }} allowDecimals={false} />
                  <Tooltip
                    contentStyle={{ background: '#0f172a', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 8, fontSize: 12 }}
                    labelStyle={{ color: '#e2e8f0' }}
                  />
                  <Bar dataKey="count" fill="#22d3ee" radius={[4, 4, 0, 0]} name="Alertas" />
                </BarChart>
              </ResponsiveContainer>
            ) : (
              <EmptyRow text="Nenhum alerta no período." />
            )}
          </div>

          <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
            {/* Automation breakdown */}
            <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
              <div className="text-[10px] font-bold uppercase text-slate-400 mb-3">ROI de Automação</div>
              <div className="flex flex-col gap-2 text-xs">
                <StatRow label="Runbooks SOAR executados" value={stats.automation.soar_executed} good />
                <StatRow label="Runbooks SOAR com falha" value={stats.automation.soar_failed} />
                <StatRow label="Ações de contenção executadas" value={stats.automation.response_executed} good />
                <StatRow label="Ações de contenção com falha" value={stats.automation.response_failed} />
              </div>
            </div>

            {/* MITRE breakdown */}
            <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
              <div className="text-[10px] font-bold uppercase text-slate-400 mb-3">Táticas MITRE ATT&amp;CK</div>
              {stats.by_mitre.length > 0 ? (
                <div className="flex flex-col gap-2 text-xs">
                  {stats.by_mitre.map((m) => (
                    <StatRow key={m.tactic} label={m.tactic} value={m.count} />
                  ))}
                </div>
              ) : (
                <EmptyRow text="Nenhuma tática MITRE mapeada no período." />
              )}
            </div>
          </div>

          {/* Source health */}
          <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
            <div className="text-[10px] font-bold uppercase text-slate-400 mb-3">Saúde das Fontes de Telemetria</div>
            {stats.source_health.length > 0 ? (
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-2">
                {stats.source_health.map((s) => (
                  <div key={s.type} className="flex items-center justify-between px-3 py-2 rounded-lg bg-white/[0.02] border border-white/5">
                    <div className="flex items-center gap-2">
                      <Radio className={`w-3.5 h-3.5 ${s.silent ? 'text-rose-400' : 'text-emerald-400'}`} />
                      <span className="text-xs font-semibold text-slate-200 capitalize">{s.type}</span>
                    </div>
                    <span className={`text-[11px] font-bold ${s.silent ? 'text-rose-400' : 'text-emerald-400'}`}>
                      {s.silent ? 'Silenciosa' : 'Ativa'}
                      {s.last_seen_seconds_ago >= 0 && (
                        <span className="text-slate-500 font-normal"> · {formatAgo(s.last_seen_seconds_ago)}</span>
                      )}
                    </span>
                  </div>
                ))}
              </div>
            ) : (
              <EmptyRow text="Nenhuma integração ativa para monitorar." />
            )}
          </div>
        </>
      )}
    </div>
  );
}

function KpiCard({ icon, label, value, sub, accent }: { icon: React.ReactNode; label: string; value: string; sub: string; accent?: string }) {
  return (
    <div className="p-3.5 rounded-xl bg-white/[0.02] border border-white/5 flex flex-col gap-1">
      <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase text-slate-400">
        {icon} {label}
      </div>
      <span className={`text-2xl font-bold ${accent ?? 'text-slate-100'}`}>{value}</span>
      <span className="text-[11px] text-slate-500">{sub}</span>
    </div>
  );
}

function StatRow({ label, value, good }: { label: string; value: number; good?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-slate-400">{label}</span>
      <span className={`font-bold ${good && value > 0 ? 'text-emerald-400' : 'text-slate-200'}`}>{value}</span>
    </div>
  );
}

function EmptyRow({ text }: { text: string }) {
  return <div className="text-xs text-slate-500 py-4 text-center">{text}</div>;
}

function formatAgo(seconds: number): string {
  if (seconds < 60) return `há ${seconds}s`;
  if (seconds < 3600) return `há ${Math.floor(seconds / 60)}min`;
  if (seconds < 86400) return `há ${Math.floor(seconds / 3600)}h`;
  return `há ${Math.floor(seconds / 86400)}d`;
}
