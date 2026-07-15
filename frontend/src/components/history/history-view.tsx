'use client';

import { useCallback, useEffect, useState } from 'react';
import { Archive, CheckCircle2, RefreshCw, RotateCcw, Search } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import { SeverityBadge } from '@/components/alerts/severity-badge';
import { SourceBadge } from '@/components/alerts/source-badge';
import { StatusBadge } from '@/components/alerts/status-badge';
import { isOpen } from '@/lib/alert-priority';
import type { Alert } from '@/types';

const PAGE_SIZE = 50;

const SEVERITIES = ['', 'fatal', 'critical', 'warning', 'info'] as const;
const STATUSES = ['', 'triggered', 'acknowledged', 'resolved', 'suppressed'] as const;
const WINDOWS = [
  { hours: 0, label: 'Tudo' },
  { hours: 24, label: '24h' },
  { hours: 168, label: '7d' },
  { hours: 720, label: '30d' },
] as const;

// History/search view (item 3): the archive over ALL alerts — resolved and suppressed included —
// that the operational console (open-only) deliberately hides. Search/filter to find a past alert
// and, when something was closed prematurely, reopen it back into the working queue.
export function HistoryView({ tenantId, domain }: { tenantId?: string; domain?: 'noc' | 'soc' }) {
  const [search, setSearch] = useState('');
  const [debouncedSearch, setDebouncedSearch] = useState('');
  const [severity, setSeverity] = useState('');
  const [status, setStatus] = useState('');
  const [hours, setHours] = useState(0);

  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [offset, setOffset] = useState(0);
  const [hasMore, setHasMore] = useState(false);

  const [confirmTarget, setConfirmTarget] = useState<Alert | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [actioningId, setActioningId] = useState<string | null>(null);

  // Debounce the free-text search so we don't refetch on every keystroke.
  useEffect(() => {
    const t = setTimeout(() => setDebouncedSearch(search.trim()), 350);
    return () => clearTimeout(t);
  }, [search]);

  const qtenant = tenantId ? `&tenant_id=${tenantId}` : '';
  const qdomain = domain ? `&domain=${domain}` : '';

  const buildQuery = useCallback(
    (off: number) => {
      const parts = [`limit=${PAGE_SIZE}`, `offset=${off}`];
      if (debouncedSearch) parts.push(`q=${encodeURIComponent(debouncedSearch)}`);
      if (severity) parts.push(`severity=${severity}`);
      if (status) parts.push(`status=${status}`);
      if (hours > 0) parts.push(`hours=${hours}`);
      return `/api/v1/alerts/history?${parts.join('&')}${qtenant}${qdomain}`;
    },
    [debouncedSearch, severity, status, hours, qtenant, qdomain],
  );

  // Fetch page 0 whenever any filter changes.
  useEffect(() => {
    let cancelled = false;
    setIsLoading(true);
    (async () => {
      try {
        const data = await apiFetchJson<Alert[]>(buildQuery(0));
        if (cancelled) return;
        setAlerts(data || []);
        setOffset(data?.length || 0);
        setHasMore((data?.length || 0) === PAGE_SIZE);
      } catch (err) {
        if (!cancelled) console.error('Failed to fetch alert history:', err);
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [buildQuery]);

  const loadMore = async () => {
    if (isLoading) return;
    setIsLoading(true);
    try {
      const data = await apiFetchJson<Alert[]>(buildQuery(offset));
      const page = data || [];
      setAlerts((prev) => [...prev, ...page]);
      setOffset((o) => o + page.length);
      setHasMore(page.length === PAGE_SIZE);
    } catch (err) {
      console.error('Failed to load more history:', err);
    } finally {
      setIsLoading(false);
    }
  };

  // Manual refresh: re-run the page-0 fetch for the current filters.
  const handleRefresh = () => {
    setIsLoading(true);
    (async () => {
      try {
        const data = await apiFetchJson<Alert[]>(buildQuery(0));
        setAlerts(data || []);
        setOffset(data?.length || 0);
        setHasMore((data?.length || 0) === PAGE_SIZE);
      } catch (err) {
        console.error('Failed to refresh history:', err);
      } finally {
        setIsLoading(false);
      }
    })();
  };

  const handleReopen = async () => {
    if (!confirmTarget) return;
    setActioningId(confirmTarget.id);
    setConfirmError(null);
    try {
      const res = await apiFetch(`/api/v1/incidents/reopen${tenantId ? `?tenant_id=${tenantId}` : ''}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: confirmTarget.id, created_at: confirmTarget.created_at }),
      });
      if (!res.ok) {
        const txt = await res.text().catch(() => '');
        setConfirmError(txt || 'Falha ao reabrir o alerta.');
        return;
      }
      // Reflect the reopen locally: the alert is now 'triggered' again.
      setAlerts((prev) => prev.map((a) => (a.id === confirmTarget.id ? { ...a, status: 'triggered' } : a)));
      setConfirmTarget(null);
    } catch {
      setConfirmError('Erro de conectividade com o backend.');
    } finally {
      setActioningId(null);
    }
  };

  const selectClass =
    'bg-black/40 border border-white/10 rounded-lg px-2.5 py-1.5 text-xs text-slate-200 focus:outline-none focus:border-violet-500/40 cursor-pointer';

  return (
    <div className="glass-card rounded-xl border border-white/5 bg-surface/30 p-6 flex flex-col gap-5">
      <div className="flex items-center justify-between">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Archive className="w-4 h-4 text-violet-400" /> Histórico de alertas
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Todos os alertas, incluindo resolvidos e suprimidos — busque e reabra o que foi fechado antes da hora
          </p>
        </div>
        <button
          onClick={handleRefresh}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {/* Filters */}
      <div className="flex flex-wrap items-center gap-3">
        <div className="relative flex-1 min-w-[220px]">
          <Search className="absolute left-3 top-2.5 w-4 h-4 text-slate-500" />
          <input
            type="text"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Buscar por resumo ou tipo de evento…"
            className="w-full bg-black/40 border border-white/10 rounded-lg pl-10 pr-3 py-2 text-xs text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-violet-500/40"
          />
        </div>
        <select value={severity} onChange={(e) => setSeverity(e.target.value)} className={selectClass} aria-label="Severidade">
          {SEVERITIES.map((s) => (
            <option key={s || 'all'} value={s}>
              {s ? s.toUpperCase() : 'Toda severidade'}
            </option>
          ))}
        </select>
        <select value={status} onChange={(e) => setStatus(e.target.value)} className={selectClass} aria-label="Status">
          {STATUSES.map((s) => (
            <option key={s || 'all'} value={s}>
              {s
                ? s === 'triggered'
                  ? 'Disparado'
                  : s === 'acknowledged'
                    ? 'Reconhecido'
                    : s === 'resolved'
                      ? 'Resolvido'
                      : 'Suprimido'
                : 'Todo status'}
            </option>
          ))}
        </select>
        <select value={hours} onChange={(e) => setHours(Number(e.target.value))} className={selectClass} aria-label="Janela">
          {WINDOWS.map((w) => (
            <option key={w.hours} value={w.hours}>
              {w.label}
            </option>
          ))}
        </select>
      </div>

      {/* Results */}
      {isLoading && alerts.length === 0 ? (
        <div className="flex items-center justify-center py-10 gap-2 text-xs text-slate-500">
          <RefreshCw className="w-4 h-4 animate-spin text-violet-500" /> Carregando histórico…
        </div>
      ) : alerts.length === 0 ? (
        <div className="p-4 rounded-lg bg-white/[0.02] border border-white/5 text-slate-500 text-xs flex items-center gap-2">
          <Archive className="w-4 h-4 shrink-0" /> Nenhum alerta encontrado para os filtros atuais.
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <div className="text-[10px] text-slate-600 uppercase tracking-wider font-bold">{alerts.length} resultado(s)</div>
          {alerts.map((a) => (
            <div key={`${a.id}-${a.created_at}`} className="flex items-center justify-between gap-3 rounded-lg bg-black/30 border border-white/5 px-3 py-2">
              <div className="flex items-center gap-2 min-w-0">
                <SeverityBadge severity={a.severity} />
                <SourceBadge source={a.ai_analysis?.source as string | undefined} />
                <span className="text-slate-200 text-xs font-medium truncate max-w-[22rem]">{a.summary}</span>
                <code className="text-[10px] text-slate-500 hidden md:inline">{a.event_type}</code>
              </div>
              <div className="flex items-center gap-3 shrink-0">
                <span className="text-[10px] text-slate-500 font-mono hidden sm:inline">{new Date(a.created_at).toLocaleString()}</span>
                <StatusBadge status={a.status} />
                {!isOpen(a) && (
                  <button
                    onClick={() => {
                      setConfirmTarget(a);
                      setConfirmError(null);
                    }}
                    className="flex items-center gap-1 px-2.5 py-1 rounded bg-violet-600/15 hover:bg-violet-600/30 text-violet-300 text-[10px] font-bold uppercase tracking-wider border border-violet-500/25 transition-all cursor-pointer"
                  >
                    <RotateCcw className="w-3 h-3" /> Reabrir
                  </button>
                )}
              </div>
            </div>
          ))}
          {hasMore && (
            <button
              onClick={loadMore}
              disabled={isLoading}
              className="mt-1 self-center px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50 flex items-center gap-2"
            >
              {isLoading ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : null} Carregar mais
            </button>
          )}
        </div>
      )}

      <Dialog open={!!confirmTarget} onOpenChange={(open) => !open && setConfirmTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <RotateCcw className="w-4 h-4 text-violet-400" /> Reabrir alerta
            </DialogTitle>
            <DialogDescription>{confirmTarget?.summary}</DialogDescription>
          </DialogHeader>
          <p className="text-xs text-violet-300 bg-violet-500/10 border border-violet-500/20 rounded-lg p-3">
            Reabrir volta este alerta para a fila de trabalho (status <strong>Disparado</strong>) para ser tratado de novo.
            O incidente agrupado não é alterado — uma recorrência real abre um incidente novo por conta própria.
          </p>
          {confirmError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">{confirmError}</p>}
          <DialogFooter>
            <button
              onClick={() => setConfirmTarget(null)}
              disabled={!!actioningId}
              className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50"
            >
              Cancelar
            </button>
            <button
              onClick={handleReopen}
              disabled={!!actioningId}
              className="px-4 py-2 rounded-lg bg-violet-600 hover:bg-violet-500 text-slate-950 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50 flex items-center justify-center gap-2"
            >
              {actioningId ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2 className="w-3.5 h-3.5" />}
              Reabrir
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
