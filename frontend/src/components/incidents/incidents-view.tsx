'use client';

import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, ChevronDown, ChevronRight, MessageSquarePlus, RefreshCw, ShieldCheck, Siren } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { Incident, IncidentAlert, IncidentComment, IncidentDisposition } from '@/types';

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

// Dynamic risk score (0-100): severity + recurrence. Colored by band.
function riskClass(score: number): string {
  if (score >= 80) return 'bg-rose-600/20 text-rose-300 border-rose-500/40';
  if (score >= 55) return 'bg-amber-500/15 text-amber-400 border-amber-500/30';
  if (score >= 30) return 'bg-sky-500/15 text-sky-400 border-sky-500/30';
  return 'bg-slate-500/15 text-slate-400 border-slate-500/25';
}

// Business criticality of the affected CMDB asset (B1). Only high/critical get a visible badge — they
// are what raised the risk score above the ordinary-host baseline; low/medium are neutral.
const CRIT_LABEL: Record<string, string> = { critical: 'ativo crítico', high: 'ativo importante' };
function critClass(c: string): string {
  if (c === 'critical') return 'bg-fuchsia-600/20 text-fuchsia-300 border-fuchsia-500/40';
  return 'bg-purple-500/15 text-purple-300 border-purple-500/30';
}

// Analyst disposition (K5): the verdict badge + the three classify options.
const DISPOSITIONS: { id: IncidentDisposition; label: string; cls: string }[] = [
  { id: 'true_positive', label: 'Verdadeiro-Positivo', cls: 'bg-emerald-500/15 text-emerald-300 border-emerald-500/30' },
  { id: 'false_positive', label: 'Falso-Positivo', cls: 'bg-rose-500/15 text-rose-300 border-rose-500/30' },
  { id: 'benign', label: 'Benigno', cls: 'bg-slate-500/15 text-slate-300 border-slate-500/30' },
];

// Incidents view (Fase 3/3b-cont): the grouped-investigation surface. Each row is an incident —
// the recurring alerts of one problem collapsed together — with acknowledge/resolve actions and an
// expandable list of the alerts it groups. Resolving closes the incident (a new occurrence opens a
// fresh one).
export function IncidentsView({ tenantId, domain }: { tenantId?: string; domain?: 'noc' | 'soc' }) {
  const [filter, setFilter] = useState<Filter>('open');
  const [incidents, setIncidents] = useState<Incident[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [expandedId, setExpandedId] = useState<string | null>(null);
  const [alerts, setAlerts] = useState<Record<string, IncidentAlert[]>>({});
  const [comments, setComments] = useState<Record<string, IncidentComment[]>>({});
  const [noteDraft, setNoteDraft] = useState<Record<string, string>>({});
  const [postingId, setPostingId] = useState<string | null>(null);
  const [confirmTarget, setConfirmTarget] = useState<Incident | null>(null);
  const [confirmIntent, setConfirmIntent] = useState<Intent | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [actioningId, setActioningId] = useState<string | null>(null);
  const [classifyingId, setClassifyingId] = useState<string | null>(null);

  const qtenant = tenantId ? `&tenant_id=${tenantId}` : '';
  const qdomain = domain ? `&domain=${domain}` : '';

  const fetchIncidents = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await apiFetchJson<Incident[]>(`/api/v1/incidents?status=${filter}${qtenant}${qdomain}`);
      setIncidents(data || []);
    } catch (err) {
      console.error('Failed to fetch incidents:', err);
    } finally {
      setIsLoading(false);
    }
  }, [filter, qtenant, qdomain]);

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
    if (!comments[inc.id]) {
      await refreshComments(inc.id);
    }
  };

  const refreshComments = async (incidentID: string) => {
    try {
      const data = await apiFetchJson<IncidentComment[]>(`/api/v1/incidents/comments?incident_id=${incidentID}${qtenant}`);
      setComments((prev) => ({ ...prev, [incidentID]: data || [] }));
    } catch (err) {
      console.error('Failed to fetch incident comments:', err);
    }
  };

  const postNote = async (incidentID: string) => {
    const text = (noteDraft[incidentID] || '').trim();
    if (!text || postingId) return;
    setPostingId(incidentID);
    try {
      const created = await apiFetchJson<IncidentComment>(`/api/v1/incidents/comments${tenantId ? `?tenant_id=${tenantId}` : ''}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ incident_id: incidentID, comment: text }),
      });
      setComments((prev) => ({ ...prev, [incidentID]: [...(prev[incidentID] || []), created] }));
      setNoteDraft((prev) => ({ ...prev, [incidentID]: '' }));
    } catch (err) {
      console.error('Failed to post note:', err);
    } finally {
      setPostingId(null);
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

  // Classify an incident's disposition (K5): TP / FP / benign. Reflects locally on success.
  const classifyIncident = async (inc: Incident, disposition: IncidentDisposition) => {
    setClassifyingId(inc.id);
    try {
      const res = await apiFetch(`/api/v1/incidents/group/disposition${tenantId ? `?tenant_id=${tenantId}` : ''}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ incident_id: inc.id, disposition }),
      });
      if (!res.ok) {
        console.error('Failed to classify incident:', await res.text());
        return;
      }
      setIncidents((prev) => prev.map((i) => (i.id === inc.id ? { ...i, disposition } : i)));
    } catch (err) {
      console.error('Network error classifying incident:', err);
    } finally {
      setClassifyingId(null);
    }
  };

  const handleConfirm = async () => {
    if (!confirmTarget || !confirmIntent) return;
    setActioningId(confirmTarget.id);
    setConfirmError(null);
    try {
      const endpoint = confirmIntent === 'acknowledge' ? '/api/v1/incidents/group/acknowledge' : '/api/v1/incidents/group/resolve';
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
                      <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${riskClass(inc.risk_score)}`} title="Score de risco dinâmico (severidade + recorrência + criticidade do ativo)">
                        risco {inc.risk_score}
                      </span>
                      {inc.asset_criticality && CRIT_LABEL[inc.asset_criticality] && (
                        <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${critClass(inc.asset_criticality)}`} title="Criticidade de negócio do ativo afetado (CMDB) — elevou o score de risco">
                          {CRIT_LABEL[inc.asset_criticality]}
                        </span>
                      )}
                      <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${SEVERITY_CLASS[inc.severity] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>{inc.severity}</span>
                      <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${STATUS_CLASS[inc.status] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>{inc.status}</span>
                      {inc.disposition && (
                        <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${DISPOSITIONS.find((d) => d.id === inc.disposition)?.cls || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`} title="Classificação do analista (K5)">
                          {DISPOSITIONS.find((d) => d.id === inc.disposition)?.label || inc.disposition}
                        </span>
                      )}
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
                  {/* Disposition classification (K5): the analyst's verdict, feeds the FP-rate KPI. */}
                  <div className="flex items-center gap-2 flex-wrap mb-3 pb-3 border-b border-white/5">
                    <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Classificação</span>
                    {DISPOSITIONS.map((d) => (
                      <button
                        key={d.id}
                        disabled={classifyingId === inc.id}
                        onClick={() => classifyIncident(inc, d.id)}
                        className={`px-2.5 py-1 rounded text-[10px] font-bold uppercase tracking-wider border transition-all cursor-pointer disabled:opacity-50 ${
                          inc.disposition === d.id ? d.cls : 'bg-white/[0.02] border-white/10 text-slate-500 hover:text-slate-300'
                        }`}
                      >
                        {d.label}
                      </button>
                    ))}
                  </div>
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
                          <span className="text-slate-500 shrink-0">{a.status} · {new Date(a.created_at).toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' })} {new Date(a.created_at).toLocaleTimeString('pt-BR')}</span>
                        </div>
                      ))}
                    </div>
                  )}

                  {/* Investigation timeline (B1 fatia 2): notes live on the incident, alongside the
                      SOAR/approval audit trail the worker now stamps with the real incident id. */}
                  <div className="mt-4 pt-3 border-t border-white/5">
                    <div className="text-[10px] font-bold uppercase tracking-wider text-slate-500 mb-2">Notas da investigação</div>
                    {!comments[inc.id] ? (
                      <div className="text-[11px] text-slate-500">Carregando notas…</div>
                    ) : comments[inc.id].length === 0 ? (
                      <div className="text-[11px] text-slate-600 mb-2">Nenhuma nota ainda. Registre o que foi investigado ou decidido.</div>
                    ) : (
                      <div className="flex flex-col gap-2 mb-2">
                        {comments[inc.id].map((c) => (
                          <div key={c.id} className="text-[11px] bg-black/30 border border-white/5 rounded-lg px-3 py-2">
                            <div className="flex items-center justify-between gap-2 mb-1">
                              <span className="font-bold text-slate-300 truncate">{c.author}</span>
                              <span className="text-slate-600 shrink-0">{new Date(c.created_at).toLocaleString()}</span>
                            </div>
                            <div className="text-slate-400 whitespace-pre-wrap break-words">{c.comment}</div>
                          </div>
                        ))}
                      </div>
                    )}
                    <div className="flex items-start gap-2">
                      <textarea
                        value={noteDraft[inc.id] || ''}
                        onChange={(e) => setNoteDraft((prev) => ({ ...prev, [inc.id]: e.target.value }))}
                        placeholder="Adicionar uma nota à investigação…"
                        rows={2}
                        className="flex-1 resize-y rounded-lg bg-black/40 border border-white/10 px-3 py-2 text-[11px] text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-violet-500/40"
                      />
                      <button
                        onClick={() => postNote(inc.id)}
                        disabled={postingId === inc.id || !(noteDraft[inc.id] || '').trim()}
                        className="px-3 py-2 rounded-lg bg-violet-600/20 hover:bg-violet-600/30 disabled:opacity-40 text-violet-300 text-[10px] font-bold uppercase tracking-wider border border-violet-500/25 transition-all cursor-pointer flex items-center gap-1.5 shrink-0"
                      >
                        {postingId === inc.id ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <MessageSquarePlus className="w-3.5 h-3.5" />}
                        Nota
                      </button>
                    </div>
                  </div>
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
