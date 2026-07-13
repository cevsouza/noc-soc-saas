'use client';

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Activity, RefreshCw } from 'lucide-react';
import {
  LineChart,
  Line,
  XAxis,
  YAxis,
  CartesianGrid,
  Tooltip,
  ResponsiveContainer,
} from 'recharts';
import { apiFetchJson } from '@/lib/api-client';
import type { MetricCatalogEntry, MetricSeriesEntry } from '@/types';

const RANGES = [
  { label: '1h', hours: 1 },
  { label: '6h', hours: 6 },
  { label: '24h', hours: 24 },
  { label: '72h', hours: 72 },
];

// SNMP time-series graphs (slice 3): pick a metric from the tenant's catalog and a time range, then
// chart the raw sampled values the agent has been pushing.
export function MetricsView({ tenantId }: { tenantId?: string }) {
  const qtenant = tenantId ? `?tenant_id=${tenantId}` : '';
  const qtenantAmp = tenantId ? `&tenant_id=${tenantId}` : '';

  const [catalog, setCatalog] = useState<MetricCatalogEntry[]>([]);
  const [selectedKey, setSelectedKey] = useState<string>('');
  const [hours, setHours] = useState(6);
  const [series, setSeries] = useState<MetricSeriesEntry[]>([]);
  const [loading, setLoading] = useState(false);

  const keyOf = (c: MetricCatalogEntry) => `${c.target_id ?? ''}|${c.oid}`;
  const selected = useMemo(() => catalog.find((c) => keyOf(c) === selectedKey), [catalog, selectedKey]);

  const fetchCatalog = useCallback(async () => {
    try {
      const data = await apiFetchJson<MetricCatalogEntry[]>(`/api/v1/agent/metrics/catalog${qtenant}`);
      setCatalog(data || []);
      if ((data || []).length > 0) {
        setSelectedKey((prev) => (prev ? prev : `${data[0].target_id ?? ''}|${data[0].oid}`));
      }
    } catch (err) {
      console.error('Failed to fetch metrics catalog:', err);
    }
  }, [qtenant]);

  useEffect(() => {
    fetchCatalog();
  }, [fetchCatalog]);

  const fetchSeries = useCallback(async () => {
    if (!selected || !selected.target_id) {
      setSeries([]);
      return;
    }
    setLoading(true);
    try {
      const data = await apiFetchJson<MetricSeriesEntry[]>(
        `/api/v1/agent/metrics/series?target_id=${selected.target_id}&oid=${encodeURIComponent(selected.oid)}&hours=${hours}${qtenantAmp}`,
      );
      setSeries(data || []);
    } catch (err) {
      console.error('Failed to fetch metric series:', err);
      setSeries([]);
    } finally {
      setLoading(false);
    }
  }, [selected, hours, qtenantAmp]);

  useEffect(() => {
    fetchSeries();
  }, [fetchSeries]);

  const chartData = series.map((s) => ({
    t: new Date(s.ts).toLocaleTimeString([], { hour: '2-digit', minute: '2-digit' }),
    value: s.value,
  }));

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between flex-wrap gap-3 border-b border-white/5 pb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Activity className="w-4 h-4 text-sky-400" /> Métricas SNMP
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Séries temporais coletadas pelo agente nos equipamentos de rede
          </p>
        </div>
        <button
          onClick={fetchSeries}
          disabled={loading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <select
          value={selectedKey}
          onChange={(e) => setSelectedKey(e.target.value)}
          className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-sky-500 min-w-[240px]"
        >
          {catalog.length === 0 && <option value="">Nenhuma métrica disponível</option>}
          {catalog.map((c) => (
            <option key={keyOf(c)} value={keyOf(c)}>
              {c.label} ({c.oid})
            </option>
          ))}
        </select>
        <div className="flex gap-1">
          {RANGES.map((r) => (
            <button
              key={r.hours}
              onClick={() => setHours(r.hours)}
              className={`px-3 py-1.5 rounded-lg border text-xs font-bold transition-all cursor-pointer ${
                hours === r.hours
                  ? 'bg-sky-500/20 border-sky-500/50 text-sky-300'
                  : 'bg-white/[0.02] border-white/10 text-slate-500 hover:text-slate-300'
              }`}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>

      <div className="p-4 rounded-xl bg-black/40 border border-white/5" style={{ height: 340 }}>
        {chartData.length === 0 ? (
          <div className="h-full flex items-center justify-center text-xs text-slate-500 text-center px-4">
            {catalog.length === 0
              ? 'Nenhuma métrica ainda. Cadastre alvos SNMP e aguarde o agente coletar.'
              : 'Sem amostras nesta janela de tempo para a métrica selecionada.'}
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={chartData} margin={{ top: 8, right: 16, left: 0, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" stroke="rgba(255,255,255,0.06)" />
              <XAxis dataKey="t" tick={{ fill: '#64748b', fontSize: 10 }} minTickGap={40} />
              <YAxis tick={{ fill: '#64748b', fontSize: 10 }} width={44} />
              <Tooltip
                contentStyle={{ background: '#0b0f19', border: '1px solid rgba(255,255,255,0.1)', borderRadius: 8, fontSize: 12 }}
                labelStyle={{ color: '#94a3b8' }}
              />
              <Line type="monotone" dataKey="value" stroke="#38bdf8" strokeWidth={2} dot={false} name={selected?.label || 'valor'} />
            </LineChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}
