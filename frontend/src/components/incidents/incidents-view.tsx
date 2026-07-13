'use client';

import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, RefreshCw, ShieldCheck, Siren } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { Incident, IncidentAlert } from '@/types';

type Filter = 'open' | 'acknowledged' | 'resolved' | 'all';
type Intent = 'acknowledge' | 'resolve';

const SEVERITY_CLASS: Record<string, string> = {
  fatal: 'bg-rose-600/20 text-rose-300 border-rose-500/40',
  critical: 'bg-rose-500/15 text-rose-400 border-rose-500/30',
  warning: 'bg-amber-500/15 text-amber-400 border-amber-500/30',
  info: 'bg-sky-500/15 text-sky-400 border-sky-500/30',
};

const STATUS_CLASS: Record<string, string> = {
  open: 'bg-rose-500/10 text-rose-400 border-rose-500/25',
  acknowledged: 'bg-amber-500/10 text-amber-400 border-amber-500/25',
  resolved: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/25',
};

// Incidents view (Fase 3/3b-cont): the grouped-investigation surface. Each row is an incident —
// the recurring alerts of one problem collapsed together — with acknowledge/resolve actions and an
// expandable list of the alerts it groups. Resolving closes the incident (a new occurrence opens a
// fresh one).
export function IncidentsView({ tenantId }: { tenantId?: string }) {
  const [filter, setFilter] = useState<Filter>('open');
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [alerts, setAlerts] = useState<Record<string, IncidentAlert[]>>({});
  const [confirmTarget, setConfirmTarget] = useState<Incident | null>(null);
  const [confirmIntent, setConfirmIntent] = useState<Intent | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [actioningId, setActioningId] = useState<string | null>(null);

  const qtenant = tenantId ? `&tenant_id=${tenantId}` : '';

  const fetchIncidents = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await apiFetchJson<Incident[]>(`/api/v1/incidents?status=${filter}${qtenant}`);
      setIncidents(data || []);
    } catch (err) {
      console.error('Failed to fetch incidents:', err);
    } finally {
      setIsLoading(false);
    }
  }, [filter, qtenant]);

  useEffect(() => {
    fetchIncidents();
  }, [fetchIncidents]);

  const toggleExpand = async (inc: Incident) => {
    if (expandedId === inc.id) {
      setExpandedId(null);
      return;
    }
    setExpandedId(inc.id);
    if (!alerts[inc.id]) {
      try {
        const data = await apiFetchJson<IncidentAlert[]>(`/api/v1/incidents/alerts?incident_id=${inc.id}${qtenant}`);
        setAlerts((prev) => ({ ...prev, [inc.id]: data || [] }));
      } catch (err) {
        console.error('Failed to fetch incident alerts:', err);
      }
    }
  };

  const openConfirm = (inc: Incident, intent: Intent) => {
    setConfirmTarget(inc);
    setConfirmIntent(intent);
    setConfirmError(null);
  };
  const closeConfirm = () => {
    setConfirmTarget(null);
    setConfirmIntent(null);
    setConfirmError(null);
  };

  const handleConfirm = async () => {
    if (!confirmTarget || !confirmIntent) return;
    setActioningId(confirmTarget.id);
    setConfirmError(null);
    try {
      const endpoint = confirmIntent === 'acknowledge' ? '/api/v1/incidents/acknowledge' : '/api/v1/incidents/resolve';
      const res = await apiFetch(`${endpoint}${tenantId ? `?tenant_id=${tenantId}` : ''}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ incident_id: confirmTarget.id }),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        setConfirmError(data.message || data.error || 'Falha ao processar a ação.');
        return;
      }
      closeConfirm();
      setAlerts((prev) => {
        const next = { ...prev };
        delete next[confirmTarget.id];
        return next;
      });
      fetchIncidents();
    } catch {
      setConfirmError('Erro de conectividade com o backend.');
    } finally {
      setActioningId(null);
    }
  };

  return (
    <div className="glass-card rounded-xl border border-white/5 bg-surface/30 p-6 flex flex-col gap-5">
      <div className="flex items-center justify-between">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Siren className="w-4 h-4 text-rose-400" /> Incidentes (investigação agrupada)
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Alertas recorrentes do mesmo problema agrupados em uma investigação única
          </p>
        </div>
        <button
          onClick={fetchIncidents}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      <div className="flex gap-2">
        {(['open', 'acknowledged', 'resolved', 'all'] as Filter[]).map((f) => (
          <button
            key={f}
            onClick={() => setFilter(f)}
            className={`px-3 py-1.5 rounded-lg text-[10px] font-bold uppercase tracking-wider transition-all cursor-pointer ${
              filter === f ? 'bg-violet-500/15 text-violet-300 border border-violet-500/30' : 'bg-white/5 text-slate-400 border border-transparent hover:text-slate-200'
            }`}
          >
            {f === 'open' ? 'Abertos' : f === 'acknowledged' ? 'Reconhecidos' : f === 'resolved' ? 'Resolvidos' : 'Todos'}
          </button>
        ))}
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-10 gap-2 text-xs text-slate-500">
          <RefreshCw className="w-4 h-4 animate-spin text-violet-500" /> Carregando incidentes…
        </div>
      ) : incidents.length === 0 ? (
        <div className="p-4 rounded-lg bg-emerald-950/10 border border-emerald-500/10 text-emerald-400 text-xs flex items-center gap-2">
          <CheckCircle2 className="w-4 h-4 shrink-0" /> Nenhum incidente {filter === 'open' ? 'aberto' : 'nessa visão'} no momento.
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {incidents.map((inc) => (
            <div key={inc.id} className="rounded-xl bg-black/40 border border-white/5 overflow-hidden">
              <div className="p-4 flex items-start justify-between gap-4">
                <button onClick={() => toggleExpand(inc)} className="flex items-start gap-2 min-w-0 text-left cursor-pointer">
                  {expandedId === inc.id ? <ChevronDown className="w-4 h-4 text-slate-400 mt-0.5 shrink-0" /> : <ChevronRight className="w-4 h-4 text-slate-400 mt-0.5 shrink-0" />}
                  <div className="flex flex-col gap-1 min-w-0">
                    <div className="flex items-center gap-2 flex-wrap">
                      <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${SEVERITY_CLASS[inc.severity] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>{inc.severity}</span>
                      <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${STATUS_CLASS[inc.status] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>{inc.status}</span>
                      <span className="text-sm font-bold text-slate-200 truncate">{inc.title}</span>
                    </div>
                    <span className="text-[10px] text-slate-500">
                      {inc.alert_count} alerta(s) · última ocorrência {new Date(inc.last_seen).toLocaleString()}
                    </span>
                  </div>
                </button>
                {inc.status !== 'resolved' && (
                  <div className="flex items-center gap-2 shrink-0">
                    {inc.status === 'open' && (
                      <button
                        disabled={actioningId === inc.id}
                        onClick={() => openConfirm(inc, 'acknowledge')}
                        className="px-2.5 py-1.5 rounded bg-amber-500/10 hover:bg-amber-500/20 disabled:opacity-50 text-amber-400 text-[10px] font-bold uppercase tracking-wider border border-amber-500/20 transition-all cursor-pointer"
                      >
                        Reconhecer
                      </button>
                    )}
                    <button
                      disabled={actioningId === inc.id}
                      onClick={() => openConfirm(inc, 'resolve')}
                      className="px-2.5 py-1.5 rounded bg-emerald-500/10 hover:bg-emerald-500/20 disabled:opacity-50 text-emerald-400 text-[10px] font-bold uppercase tracking-wider border border-emerald-500/20 transition-all cursor-pointer"
                    >
                      Resolver
                    </button>
                  </div>
                )}
              </div>

              {expandedId === inc.id && (
                <div className="border-t border-white/5 bg-black/30 px-4 py-3">
                  {!alerts[inc.id] ? (
                    <div className="text-[11px] text-slate-500">Carregando alertas…</div>
                  ) : alerts[inc.id].length === 0 ? (
                    <div className="text-[11px] text-slate-500">Nenhum alerta vinculado.</div>
                  ) : (
                    <div className="flex flex-col gap-1.5">
                      {alerts[inc.id].map((a) => (
                        <div key={a.id} className="flex items-center justify-between gap-3 text-[11px]">
                          <div className="flex items-center gap-2 min-w-0">
                            <span className={`w-2 h-2 rounded-full ${a.severity === 'fatal' || a.severity === 'critical' ? 'bg-rose-500' : a.severity === 'warning' ? 'bg-amber-500' : 'bg-sky-500'}`} />
                            <span className="text-slate-300 truncate">{a.summary}</span>
                            <code className="text-slate-500">{a.event_type}</code>
                          </div>
                          <span className="text-slate-500 shrink-0">{a.status} · {new Date(a.created_at).toLocaleTimeString()}</span>
                        </div>
                      ))}
                    </div>
                  )}
                </div>
              )}
            </div>
          ))}
        </div>
      )}

      <Dialog open={!!confirmTarget} onOpenChange={(open) => !open && closeConfirm()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {confirmIntent === 'resolve' ? <ShieldCheck className="w-4 h-4 text-emerald-400" /> : <AlertTriangle className="w-4 h-4 text-amber-400" />}
              {confirmIntent === 'resolve' ? 'Resolver incidente' : 'Reconhecer incidente'}
            </DialogTitle>
            <DialogDescription>{confirmTarget?.title}</DialogDescription>
          </DialogHeader>
          {confirmIntent === 'resolve' && (
            <p className="text-xs text-emerald-400 bg-emerald-500/10 border border-emerald-500/20 rounded-lg p-3">
              Resolver marca o incidente e todos os seus alertas como resolvidos e fecha a investigação. Uma nova ocorrência do mesmo problema abrirá um novo incidente.
            </p>
          )}
          {confirmError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">{confirmError}</p>}
          <DialogFooter>
            <button onClick={closeConfirm} disabled={!!actioningId} className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50">
              Cancelar
            </button>
            <button
              onClick={handleConfirm}
              disabled={!!actioningId}
              className={`px-4 py-2 rounded-lg text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50 flex items-center justify-center gap-2 ${
                confirmIntent === 'resolve' ? 'bg-emerald-600 hover:bg-emerald-500 text-slate-950' : 'bg-amber-600 hover:bg-amber-500 text-slate-950'
              }`}
            >
              {actioningId ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2 className="w-3.5 h-3.5" />}
              Confirmar
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
