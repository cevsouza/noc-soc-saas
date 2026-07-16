'use client';

import { useEffect, useState } from 'react';
import {
  AlertTriangle, Bell, CheckCircle2, ChevronDown, ChevronRight, MessageSquare,
  Play, Plus, RefreshCw, ShieldX, Trash2, Workflow, XCircle,
} from 'lucide-react';
import {
  Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { Playbook, PlaybookRun, PlaybookStep, PlaybookStepType } from '@/types';

const NOTIFY_CHANNELS = ['slack', 'teams', 'email', 'pagerduty', 'opsgenie'];
const RESP_INTEGRATIONS = ['paloalto', 'fortinet', 'crowdstrike'];
const RESP_ACTIONS = ['block_ip', 'unblock_ip', 'contain_host', 'lift_containment'];

const RUN_STATUS_CLASS: Record<string, string> = {
  running: 'bg-sky-500/10 text-sky-400 border-sky-500/25',
  awaiting_approval: 'bg-amber-500/10 text-amber-400 border-amber-500/25',
  completed: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/25',
  failed: 'bg-rose-500/10 text-rose-400 border-rose-500/25',
  rejected: 'bg-slate-500/10 text-slate-400 border-slate-500/25',
};
const STEP_STATUS_CLASS: Record<string, string> = {
  pending: 'text-slate-500',
  succeeded: 'text-emerald-400',
  failed: 'text-rose-400',
  awaiting_approval: 'text-amber-400',
  skipped: 'text-slate-500',
};
const STEP_ICON: Record<PlaybookStepType, typeof Bell> = {
  notify: Bell,
  comment: MessageSquare,
  response_action: ShieldX,
};

function newStep(type: PlaybookStepType): PlaybookStep {
  if (type === 'notify') return { type, channel: 'slack', message: '' };
  if (type === 'response_action') return { type, integration_type: 'paloalto', action_type: 'block_ip', target: '', target_from: '' };
  return { type, text: '' };
}

function stepSummary(s: PlaybookStep): string {
  if (s.type === 'notify') return `Notificar ${s.channel}`;
  if (s.type === 'comment') return `Comentário: ${s.text || '(vazio)'}`;
  return `${s.action_type} em ${s.target || s.target_from || '?'} via ${s.integration_type}`;
}

// UI for the multi-step SOAR playbook engine (Backlog B7 fatia 2). "Definições" lists/builds/runs
// playbooks; "Execuções" lists runs, shows their per-step progress, and approves/rejects a run paused
// at a response_action gate. Sibling of response-actions-panel.tsx / runbook-approvals-panel.tsx.
export function PlaybooksPanel({ isTenantAdmin = false }: { isTenantAdmin?: boolean }) {
  const [tab, setTab] = useState<'defs' | 'runs'>('defs');
  const [playbooks, setPlaybooks] = useState<Playbook[]>([]);
  const [runs, setRuns] = useState<PlaybookRun[]>([]);
  const [isLoading, setIsLoading] = useState(false);

  // Builder
  const [showBuilder, setShowBuilder] = useState(false);
  const [draftName, setDraftName] = useState('');
  const [draftDesc, setDraftDesc] = useState('');
  const [draftSteps, setDraftSteps] = useState<PlaybookStep[]>([newStep('comment')]);
  const [saveError, setSaveError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);

  // Run dialog
  const [runTarget, setRunTarget] = useState<Playbook | null>(null);
  const [runIncidentId, setRunIncidentId] = useState('');
  const [runCtx, setRunCtx] = useState<{ key: string; value: string }[]>([{ key: '', value: '' }]);
  const [runResult, setRunResult] = useState<string | null>(null);
  const [running, setRunning] = useState(false);

  // Run detail + approve/reject
  const [expanded, setExpanded] = useState<string | null>(null);
  const [detail, setDetail] = useState<PlaybookRun | null>(null);
  const [confirmRun, setConfirmRun] = useState<PlaybookRun | null>(null);
  const [confirmIntent, setConfirmIntent] = useState<'approve' | 'reject' | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [acting, setActing] = useState(false);

  const fetchPlaybooks = async () => {
    try {
      setPlaybooks((await apiFetchJson<Playbook[]>('/api/v1/playbooks')) || []);
    } catch (e) { console.error('playbooks', e); }
  };
  const fetchRuns = async () => {
    setIsLoading(true);
    try {
      setRuns((await apiFetchJson<PlaybookRun[]>('/api/v1/playbooks/runs')) || []);
    } catch (e) { console.error('runs', e); } finally { setIsLoading(false); }
  };

  useEffect(() => { fetchPlaybooks(); fetchRuns(); }, []);

  const playbookName = (id: string) => playbooks.find((p) => p.id === id)?.name || id.slice(0, 8);

  // --- builder helpers ---
  const addStep = () => setDraftSteps((s) => [...s, newStep('comment')]);
  const removeStep = (i: number) => setDraftSteps((s) => s.filter((_, idx) => idx !== i));
  const updateStep = (i: number, patch: Partial<PlaybookStep>) =>
    setDraftSteps((s) => s.map((st, idx) => (idx === i ? { ...st, ...patch } : st)));
  const changeStepType = (i: number, type: PlaybookStepType) =>
    setDraftSteps((s) => s.map((st, idx) => (idx === i ? newStep(type) : st)));

  const resetBuilder = () => {
    setShowBuilder(false); setDraftName(''); setDraftDesc(''); setDraftSteps([newStep('comment')]); setSaveError(null);
  };

  const savePlaybook = async () => {
    setSaving(true); setSaveError(null);
    try {
      const res = await apiFetch('/api/v1/playbooks', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: draftName, description: draftDesc, steps: draftSteps }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { setSaveError(data.message || data.error || 'Falha ao salvar o playbook.'); return; }
      resetBuilder(); fetchPlaybooks();
    } catch { setSaveError('Erro de conectividade.'); } finally { setSaving(false); }
  };

  const deletePlaybook = async (id: string) => {
    await apiFetch(`/api/v1/playbooks?id=${id}`, { method: 'DELETE' });
    fetchPlaybooks();
  };

  // --- run ---
  const openRun = (p: Playbook) => { setRunTarget(p); setRunIncidentId(''); setRunCtx([{ key: '', value: '' }]); setRunResult(null); };
  const doRun = async () => {
    if (!runTarget) return;
    setRunning(true); setRunResult(null);
    const context: Record<string, string> = {};
    runCtx.forEach((c) => { if (c.key.trim()) context[c.key.trim()] = c.value; });
    try {
      const res = await apiFetch('/api/v1/playbooks/run', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ playbook_id: runTarget.id, incident_id: runIncidentId.trim() || undefined, context }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { setRunResult(data.message || data.error || 'Falha ao iniciar.'); return; }
      setRunResult(`Run iniciado — status: ${data.status}`);
      fetchRuns();
    } catch { setRunResult('Erro de conectividade.'); } finally { setRunning(false); }
  };

  // --- run detail + approve/reject ---
  const toggleDetail = async (r: PlaybookRun) => {
    if (expanded === r.id) { setExpanded(null); setDetail(null); return; }
    setExpanded(r.id); setDetail(null);
    try {
      const arr = await apiFetchJson<PlaybookRun[]>(`/api/v1/playbooks/runs?id=${r.id}`);
      setDetail(arr && arr[0] ? arr[0] : null);
    } catch { setDetail(null); }
  };

  const handleConfirm = async () => {
    if (!confirmRun || !confirmIntent) return;
    setActing(true); setConfirmError(null);
    try {
      const endpoint = confirmIntent === 'approve' ? '/api/v1/playbooks/runs/approve' : '/api/v1/playbooks/runs/reject';
      const res = await apiFetch(endpoint, {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ run_id: confirmRun.id }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) { setConfirmError(data.message || data.error || 'Falha ao processar.'); return; }
      setConfirmRun(null); setConfirmIntent(null);
      await fetchRuns();
      if (expanded === confirmRun.id) {
        const arr = await apiFetchJson<PlaybookRun[]>(`/api/v1/playbooks/runs?id=${confirmRun.id}`);
        setDetail(arr && arr[0] ? arr[0] : null);
      }
    } catch { setConfirmError('Erro de conectividade.'); } finally { setActing(false); }
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between border-b border-white/5 pb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Workflow className="w-4 h-4 text-cyan-400" /> Playbooks SOAR
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Orquestração multi-passo: notificar, comentar e conter — com aprovação humana nas ações de contenção
          </p>
        </div>
        <button onClick={() => { fetchPlaybooks(); fetchRuns(); }} disabled={isLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50">
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> <span>Atualizar</span>
        </button>
      </div>

      <div className="flex gap-2">
        {(['defs', 'runs'] as const).map((t) => (
          <button key={t} onClick={() => setTab(t)}
            className={`px-3 py-1.5 rounded-lg text-[10px] font-bold uppercase tracking-wider transition-all cursor-pointer ${
              tab === t ? 'bg-cyan-500/15 text-cyan-400 border border-cyan-500/30' : 'bg-white/5 text-slate-400 border border-transparent hover:text-slate-200'
            }`}>
            {t === 'defs' ? 'Definições' : 'Execuções'}
          </button>
        ))}
      </div>

      {tab === 'defs' && (
        <div className="flex flex-col gap-4">
          {isTenantAdmin && !showBuilder && (
            <button onClick={() => setShowBuilder(true)}
              className="self-start flex items-center gap-1.5 px-3 py-2 rounded-lg bg-cyan-600/15 hover:bg-cyan-600/25 border border-cyan-500/30 text-cyan-300 text-xs font-bold transition-all cursor-pointer">
              <Plus className="w-3.5 h-3.5" /> Novo Playbook
            </button>
          )}

          {showBuilder && (
            <div className="p-4 rounded-xl bg-black/40 border border-cyan-500/20 flex flex-col gap-3">
              <div className="grid grid-cols-2 gap-3">
                <input value={draftName} onChange={(e) => setDraftName(e.target.value)} placeholder="Nome do playbook"
                  className="bg-surface border border-white/10 rounded-lg p-2.5 text-xs text-slate-200 focus:outline-none focus:border-cyan-500/50" />
                <input value={draftDesc} onChange={(e) => setDraftDesc(e.target.value)} placeholder="Descrição (opcional)"
                  className="bg-surface border border-white/10 rounded-lg p-2.5 text-xs text-slate-200 focus:outline-none focus:border-cyan-500/50" />
              </div>

              <div className="flex flex-col gap-2">
                {draftSteps.map((s, i) => (
                  <div key={i} className="p-3 rounded-lg bg-white/[0.02] border border-white/5 flex flex-col gap-2">
                    <div className="flex items-center gap-2">
                      <span className="text-[10px] font-bold text-slate-500 w-6">#{i + 1}</span>
                      <select value={s.type} onChange={(e) => changeStepType(i, e.target.value as PlaybookStepType)}
                        className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none">
                        <option value="comment">Comentário</option>
                        <option value="notify">Notificar</option>
                        <option value="response_action">Contenção (aprovação)</option>
                      </select>
                      <button onClick={() => removeStep(i)} className="ml-auto text-slate-500 hover:text-rose-400 cursor-pointer"><Trash2 className="w-3.5 h-3.5" /></button>
                    </div>
                    {s.type === 'notify' && (
                      <div className="grid grid-cols-2 gap-2 pl-8">
                        <select value={s.channel} onChange={(e) => updateStep(i, { channel: e.target.value })}
                          className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none">
                          {NOTIFY_CHANNELS.map((c) => <option key={c} value={c}>{c}</option>)}
                        </select>
                        <input value={s.message || ''} onChange={(e) => updateStep(i, { message: e.target.value })} placeholder="Mensagem (opcional)"
                          className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none" />
                      </div>
                    )}
                    {s.type === 'comment' && (
                      <input value={s.text || ''} onChange={(e) => updateStep(i, { text: e.target.value })} placeholder="Texto do comentário"
                        className="ml-8 bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none" />
                    )}
                    {s.type === 'response_action' && (
                      <div className="grid grid-cols-2 gap-2 pl-8">
                        <select value={s.integration_type} onChange={(e) => updateStep(i, { integration_type: e.target.value })}
                          className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none">
                          {RESP_INTEGRATIONS.map((c) => <option key={c} value={c}>{c}</option>)}
                        </select>
                        <select value={s.action_type} onChange={(e) => updateStep(i, { action_type: e.target.value })}
                          className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none">
                          {RESP_ACTIONS.map((c) => <option key={c} value={c}>{c}</option>)}
                        </select>
                        <input value={s.target || ''} onChange={(e) => updateStep(i, { target: e.target.value })} placeholder="Alvo literal (IP/host)"
                          className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none" />
                        <input value={s.target_from || ''} onChange={(e) => updateStep(i, { target_from: e.target.value })} placeholder="ou chave do contexto (ex: src_ip)"
                          className="bg-surface border border-white/10 rounded p-1.5 text-[11px] text-slate-200 focus:outline-none" />
                      </div>
                    )}
                  </div>
                ))}
                <button onClick={addStep} className="self-start flex items-center gap-1 text-[11px] text-cyan-400 hover:text-cyan-300 font-bold cursor-pointer">
                  <Plus className="w-3 h-3" /> Adicionar passo
                </button>
              </div>

              {saveError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-2.5">{saveError}</p>}
              <div className="flex gap-2 justify-end">
                <button onClick={resetBuilder} className="px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-[11px] font-bold uppercase tracking-wider cursor-pointer">Cancelar</button>
                <button onClick={savePlaybook} disabled={saving || !draftName.trim()}
                  className="px-3 py-1.5 rounded-lg bg-cyan-600 hover:bg-cyan-500 disabled:opacity-50 text-white text-[11px] font-bold uppercase tracking-wider cursor-pointer flex items-center gap-1.5">
                  {saving && <RefreshCw className="w-3 h-3 animate-spin" />} Salvar
                </button>
              </div>
            </div>
          )}

          {playbooks.length === 0 ? (
            <p className="text-xs text-slate-500 italic">Nenhum playbook definido ainda.</p>
          ) : (
            playbooks.map((p) => (
              <div key={p.id} className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-2">
                <div className="flex items-start justify-between gap-3">
                  <div className="flex flex-col gap-0.5 min-w-0">
                    <span className="text-sm font-bold text-slate-200">{p.name}</span>
                    {p.description && <span className="text-[11px] text-slate-500">{p.description}</span>}
                  </div>
                  <div className="flex items-center gap-2 shrink-0">
                    <button onClick={() => openRun(p)} className="flex items-center gap-1 px-2.5 py-1.5 rounded bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 text-[10px] font-bold uppercase tracking-wider border border-emerald-500/20 cursor-pointer">
                      <Play className="w-3 h-3" /> Executar
                    </button>
                    {isTenantAdmin && (
                      <button onClick={() => deletePlaybook(p.id)} className="p-1.5 rounded bg-white/5 hover:bg-rose-500/20 text-slate-400 hover:text-rose-400 cursor-pointer"><Trash2 className="w-3.5 h-3.5" /></button>
                    )}
                  </div>
                </div>
                <div className="flex flex-col gap-1 pt-1 border-t border-white/5">
                  {p.steps.map((s, i) => {
                    const Icon = STEP_ICON[s.type];
                    return (
                      <div key={i} className="flex items-center gap-2 text-[11px] text-slate-400">
                        <span className="text-slate-600 w-5">#{i + 1}</span>
                        <Icon className={`w-3 h-3 ${s.type === 'response_action' ? 'text-amber-400' : 'text-slate-500'}`} />
                        <span>{stepSummary(s)}</span>
                        {s.type === 'response_action' && <span className="text-[9px] text-amber-400 uppercase font-bold">gate</span>}
                      </div>
                    );
                  })}
                </div>
              </div>
            ))
          )}
        </div>
      )}

      {tab === 'runs' && (
        <div className="flex flex-col gap-3">
          {isLoading ? (
            <div className="flex items-center justify-center py-10 gap-2 text-xs text-slate-500"><RefreshCw className="w-4 h-4 animate-spin text-cyan-500" /> Carregando execuções...</div>
          ) : runs.length === 0 ? (
            <p className="text-xs text-slate-500 italic">Nenhuma execução registrada.</p>
          ) : (
            runs.map((r) => (
              <div key={r.id} className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-2">
                <div className="flex items-start justify-between gap-3">
                  <button onClick={() => toggleDetail(r)} className="flex items-center gap-2 min-w-0 cursor-pointer text-left">
                    {expanded === r.id ? <ChevronDown className="w-4 h-4 text-slate-500 shrink-0" /> : <ChevronRight className="w-4 h-4 text-slate-500 shrink-0" />}
                    <span className="text-sm font-bold text-slate-200 truncate">{playbookName(r.playbook_id)}</span>
                    <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${RUN_STATUS_CLASS[r.status] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>{r.status}</span>
                  </button>
                  {r.status === 'awaiting_approval' && (
                    <div className="flex items-center gap-2 shrink-0">
                      <button onClick={() => { setConfirmRun(r); setConfirmIntent('reject'); setConfirmError(null); }}
                        className="px-2.5 py-1.5 rounded bg-slate-500/10 hover:bg-slate-500/20 text-slate-300 text-[10px] font-bold uppercase tracking-wider border border-slate-500/20 cursor-pointer">Rejeitar</button>
                      <button onClick={() => { setConfirmRun(r); setConfirmIntent('approve'); setConfirmError(null); }}
                        className="px-2.5 py-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 text-[10px] font-bold uppercase tracking-wider border border-rose-500/20 cursor-pointer">Aprovar & Executar</button>
                    </div>
                  )}
                </div>
                <span className="text-[10px] text-slate-500 pl-6">Por <strong className="text-slate-400">{r.started_by}</strong> em {new Date(r.created_at).toLocaleString('pt-BR')}</span>

                {expanded === r.id && (
                  <div className="pl-6 pt-2 border-t border-white/5 flex flex-col gap-1.5">
                    {!detail ? (
                      <span className="text-[11px] text-slate-500">Carregando passos...</span>
                    ) : (detail.steps || []).map((s) => (
                      <div key={s.step_index} className="flex flex-col gap-0.5">
                        <div className="flex items-center gap-2 text-[11px]">
                          <span className="text-slate-600 w-5">#{s.step_index + 1}</span>
                          <span className="text-slate-300">{s.step_type}</span>
                          <span className={`font-bold uppercase text-[9px] ${STEP_STATUS_CLASS[s.status] || 'text-slate-500'}`}>{s.status}</span>
                        </div>
                        {s.output && <span className="pl-7 text-[10px] text-slate-500 font-mono whitespace-pre-wrap">{s.output}</span>}
                      </div>
                    ))}
                  </div>
                )}
              </div>
            ))
          )}
        </div>
      )}

      {/* Run dialog */}
      <Dialog open={!!runTarget} onOpenChange={(o) => !o && setRunTarget(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2"><Play className="w-4 h-4 text-emerald-400" /> Executar “{runTarget?.name}”</DialogTitle>
            <DialogDescription>Passos de contenção pausarão para aprovação. Informe o incidente e o contexto se algum passo usar <code>target_from</code>.</DialogDescription>
          </DialogHeader>
          <input value={runIncidentId} onChange={(e) => setRunIncidentId(e.target.value)} placeholder="incident_id (opcional)"
            className="bg-white/5 border border-white/10 rounded-lg p-2.5 text-xs text-slate-200 font-mono focus:outline-none" />
          <div className="flex flex-col gap-2">
            <label className="text-[10px] uppercase font-bold text-slate-500">Contexto (chave → valor)</label>
            {runCtx.map((c, i) => (
              <div key={i} className="grid grid-cols-2 gap-2">
                <input value={c.key} onChange={(e) => setRunCtx((p) => p.map((x, idx) => idx === i ? { ...x, key: e.target.value } : x))} placeholder="src_ip"
                  className="bg-white/5 border border-white/10 rounded-lg p-2 text-[11px] text-slate-200 font-mono focus:outline-none" />
                <input value={c.value} onChange={(e) => setRunCtx((p) => p.map((x, idx) => idx === i ? { ...x, value: e.target.value } : x))} placeholder="203.0.113.9"
                  className="bg-white/5 border border-white/10 rounded-lg p-2 text-[11px] text-slate-200 font-mono focus:outline-none" />
              </div>
            ))}
            <button onClick={() => setRunCtx((p) => [...p, { key: '', value: '' }])} className="self-start text-[11px] text-cyan-400 hover:text-cyan-300 font-bold cursor-pointer flex items-center gap-1"><Plus className="w-3 h-3" /> chave</button>
          </div>
          {runResult && <p className="text-xs text-slate-300 bg-white/5 border border-white/10 rounded-lg p-2.5">{runResult}</p>}
          <DialogFooter>
            <button onClick={() => setRunTarget(null)} disabled={running} className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-xs font-bold uppercase tracking-wider cursor-pointer disabled:opacity-50">Fechar</button>
            <button onClick={doRun} disabled={running} className="px-4 py-2 rounded-lg bg-emerald-600 hover:bg-emerald-500 text-white text-xs font-bold uppercase tracking-wider cursor-pointer disabled:opacity-50 flex items-center gap-2">
              {running ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Play className="w-3.5 h-3.5" />} Iniciar
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Approve/reject dialog */}
      <Dialog open={!!confirmRun} onOpenChange={(o) => !o && setConfirmRun(null)}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <AlertTriangle className={`w-4 h-4 ${confirmIntent === 'approve' ? 'text-rose-400' : 'text-slate-400'}`} />
              {confirmIntent === 'approve' ? 'Confirmar Contenção' : 'Confirmar Rejeição'}
            </DialogTitle>
            <DialogDescription>{confirmRun && <>Run de <strong>{playbookName(confirmRun.playbook_id)}</strong></>}</DialogDescription>
          </DialogHeader>
          {confirmIntent === 'approve' && (
            <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">
              Aprovar executa imediatamente a ação de contenção pendente no fornecedor, alterando o estado da rede/endpoint, e retoma o playbook.
            </p>
          )}
          {confirmError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">{confirmError}</p>}
          <DialogFooter>
            <button onClick={() => setConfirmRun(null)} disabled={acting} className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-xs font-bold uppercase tracking-wider cursor-pointer disabled:opacity-50">Cancelar</button>
            <button onClick={handleConfirm} disabled={acting}
              className={`px-4 py-2 rounded-lg text-xs font-bold uppercase tracking-wider cursor-pointer disabled:opacity-50 flex items-center gap-2 ${confirmIntent === 'approve' ? 'bg-rose-600 hover:bg-rose-500 text-white' : 'bg-slate-600 hover:bg-slate-500 text-white'}`}>
              {acting ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : confirmIntent === 'approve' ? <CheckCircle2 className="w-3.5 h-3.5" /> : <XCircle className="w-3.5 h-3.5" />}
              {confirmIntent === 'approve' ? 'Confirmar & Executar' : 'Confirmar Rejeição'}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
