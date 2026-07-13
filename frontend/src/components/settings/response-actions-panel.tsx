'use client';

import { useEffect, useState } from 'react';
import { AlertTriangle, Ban, CheckCircle2, RefreshCw, ShieldX, XCircle } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { ResponseAction } from '@/types';

type Filter = 'pending' | 'all';
type Intent = 'approve' | 'reject';

const STATUS_BADGE_CLASS: Record<string, string> = {
  pending: 'bg-amber-500/10 text-amber-400 border-amber-500/25',
  approved: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/25',
  failed: 'bg-rose-500/10 text-rose-400 border-rose-500/25',
  rejected: 'bg-slate-500/10 text-slate-400 border-slate-500/25',
};

// Human-readable label for each vendor action type. Falls back to the raw value so a new action
// added to the backend still renders sensibly before this map is updated.
const ACTION_LABEL: Record<string, string> = {
  block_ip: 'Bloquear IP',
  unblock_ip: 'Desbloquear IP',
  contain_host: 'Conter Host',
  lift_containment: 'Liberar Host',
};

function actionLabel(action: string): string {
  return ACTION_LABEL[action] || action;
}

// UI for `response_action_requests` — the outbound firewall/EDR containment queue. The backend
// (Fase 5 fatia 4) has recorded and executed these on approval for several sessions with zero
// frontend visibility until now. Sibling of runbook-approvals-panel.tsx: same
// local-useState-per-concern, apiFetch-based style, but each row is a vendor containment action
// (block/unblock an IP, contain/lift a host) instead of an SSH runbook.
export function ResponseActionsPanel() {
  const [filter, setFilter] = useState<Filter>('pending');
  const [actions, setActions] = useState<ResponseAction[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [actioningId, setActioningId] = useState<string | null>(null);
  const [confirmTarget, setConfirmTarget] = useState<ResponseAction | null>(null);
  const [confirmIntent, setConfirmIntent] = useState<Intent | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<{ id: string; status: string; output: string } | null>(null);

  const fetchActions = async () => {
    setIsLoading(true);
    try {
      const data = await apiFetchJson<ResponseAction[]>(`/api/v1/response/requests?status=${filter}`);
      setActions(data || []);
    } catch (err) {
      console.error('Failed to fetch response actions:', err);
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchActions();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);

  const openConfirm = (action: ResponseAction, intent: Intent) => {
    setConfirmTarget(action);
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
      const endpoint = confirmIntent === 'approve' ? '/api/v1/response/approve' : '/api/v1/response/reject';
      const res = await apiFetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ request_id: confirmTarget.id }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setConfirmError(data.message || data.error || 'Falha ao processar a solicitação.');
        return;
      }
      setActionResult({
        id: confirmTarget.id,
        status: data.status || (confirmIntent === 'approve' ? 'approved' : 'rejected'),
        output: data.output || data.message || '',
      });
      closeConfirm();
      fetchActions();
    } catch (err) {
      setConfirmError('Erro de conectividade com o backend.');
    } finally {
      setActioningId(null);
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between border-b border-white/5 pb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <ShieldX className="w-4 h-4 text-amber-400" /> Fila de Contenção
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Ações outbound de firewall/EDR retidas para revisão humana antes de disparar no fornecedor
          </p>
        </div>
        <button
          onClick={fetchActions}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} />
          <span>Atualizar</span>
        </button>
      </div>

      <div className="flex gap-2">
        <button
          onClick={() => setFilter('pending')}
          className={`px-3 py-1.5 rounded-lg text-[10px] font-bold uppercase tracking-wider transition-all cursor-pointer ${
            filter === 'pending' ? 'bg-amber-500/15 text-amber-400 border border-amber-500/30' : 'bg-white/5 text-slate-400 border border-transparent hover:text-slate-200'
          }`}
        >
          Pendentes
        </button>
        <button
          onClick={() => setFilter('all')}
          className={`px-3 py-1.5 rounded-lg text-[10px] font-bold uppercase tracking-wider transition-all cursor-pointer ${
            filter === 'all' ? 'bg-violet-500/15 text-violet-400 border border-violet-500/30' : 'bg-white/5 text-slate-400 border border-transparent hover:text-slate-200'
          }`}
        >
          Histórico Completo
        </button>
      </div>

      {isLoading ? (
        <div className="flex items-center justify-center py-10 gap-2 text-xs text-slate-500">
          <RefreshCw className="w-4 h-4 animate-spin text-violet-500" />
          <span>Carregando ações de contenção...</span>
        </div>
      ) : actions.length === 0 ? (
        <div className="p-4 rounded-lg bg-emerald-950/10 border border-emerald-500/10 text-emerald-400 text-xs font-sans flex items-center gap-2">
          <CheckCircle2 className="w-4 h-4 shrink-0" />
          Nenhuma ação {filter === 'pending' ? 'pendente' : 'registrada'} no momento.
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {actions.map((a) => (
            <div key={a.id} className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-3">
              <div className="flex items-start justify-between gap-4">
                <div className="flex flex-col gap-1 min-w-0">
                  <div className="flex items-center gap-2 flex-wrap">
                    <Ban className="w-3.5 h-3.5 text-rose-400 shrink-0" />
                    <span className="text-sm font-bold text-slate-200">{actionLabel(a.action_type)}</span>
                    <code className="px-1.5 py-0.5 rounded bg-white/5 text-[11px] font-mono text-cyan-300">{a.target}</code>
                    <span className="text-[10px] text-slate-500 uppercase font-bold tracking-wider">via {a.integration_type}</span>
                    <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${STATUS_BADGE_CLASS[a.status] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>
                      {a.status}
                    </span>
                  </div>
                  {a.incident_id && <span className="text-[10px] text-slate-500 font-mono">Incidente: {a.incident_id}</span>}
                  <span className="text-[10px] text-slate-500">
                    Solicitado por <strong className="text-slate-400">{a.requested_by}</strong> em {new Date(a.created_at).toLocaleString()}
                  </span>
                </div>
                {a.status === 'pending' && (
                  <div className="flex items-center gap-2 shrink-0">
                    <button
                      disabled={actioningId === a.id}
                      onClick={() => openConfirm(a, 'reject')}
                      className="px-2.5 py-1.5 rounded bg-slate-500/10 hover:bg-slate-500/20 disabled:opacity-50 text-slate-300 text-[10px] font-bold uppercase tracking-wider border border-slate-500/20 transition-all cursor-pointer"
                    >
                      Rejeitar
                    </button>
                    <button
                      disabled={actioningId === a.id}
                      onClick={() => openConfirm(a, 'approve')}
                      className="px-2.5 py-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 disabled:opacity-50 text-rose-400 text-[10px] font-bold uppercase tracking-wider border border-rose-500/20 transition-all cursor-pointer"
                    >
                      Aprovar & Executar
                    </button>
                  </div>
                )}
              </div>

              {a.reason && (
                <textarea
                  readOnly
                  value={a.reason}
                  rows={2}
                  className="bg-white/[0.02] border border-white/5 rounded-lg p-2.5 text-[11px] text-slate-400 font-sans resize-none focus:outline-none"
                />
              )}

              {(actionResult?.id === a.id || (a.status !== 'pending' && a.output)) && (
                <div className="flex flex-col gap-2">
                  <label className="text-[10px] uppercase font-bold text-slate-500">
                    Resultado ({actionResult?.id === a.id ? actionResult.status : a.status}):
                  </label>
                  <pre className="bg-black border border-white/5 rounded-lg p-3 text-[10px] font-mono text-emerald-400 overflow-x-auto max-h-48 whitespace-pre-wrap select-text leading-relaxed">
                    {(actionResult?.id === a.id ? actionResult.output : a.output) || '(sem saída)'}
                  </pre>
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
              <AlertTriangle className={`w-4 h-4 ${confirmIntent === 'approve' ? 'text-rose-400' : 'text-slate-400'}`} />
              {confirmIntent === 'approve' ? 'Confirmar Contenção' : 'Confirmar Rejeição'}
            </DialogTitle>
            <DialogDescription>
              {confirmTarget && (
                <>
                  <strong>{actionLabel(confirmTarget.action_type)}</strong> em <code className="text-cyan-300">{confirmTarget.target}</code> via {confirmTarget.integration_type}
                </>
              )}
            </DialogDescription>
          </DialogHeader>

          {confirmTarget?.reason && (
            <textarea readOnly value={confirmTarget.reason} rows={3} className="bg-white/5 border border-white/10 rounded-lg p-3 text-xs text-slate-300 font-sans resize-none focus:outline-none" />
          )}

          {confirmIntent === 'approve' && (
            <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">
              Esta ação dispara imediatamente a chamada ao fornecedor ({confirmTarget?.integration_type}), alterando o estado da rede/endpoint. Reverta com a ação inversa (desbloquear/liberar) se necessário.
            </p>
          )}

          {confirmError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">{confirmError}</p>}

          <DialogFooter>
            <button
              onClick={closeConfirm}
              disabled={!!actioningId}
              className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50"
            >
              Cancelar
            </button>
            <button
              onClick={handleConfirm}
              disabled={!!actioningId}
              className={`px-4 py-2 rounded-lg text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50 flex items-center justify-center gap-2 ${
                confirmIntent === 'approve' ? 'bg-rose-600 hover:bg-rose-500 text-white' : 'bg-slate-600 hover:bg-slate-500 text-white'
              }`}
            >
              {actioningId ? (
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              ) : confirmIntent === 'approve' ? (
                <CheckCircle2 className="w-3.5 h-3.5" />
              ) : (
                <XCircle className="w-3.5 h-3.5" />
              )}
              {confirmIntent === 'approve' ? 'Confirmar & Executar' : 'Confirmar Rejeição'}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
