'use client';

import { useEffect, useState } from 'react';
import { AlertTriangle, CheckCircle2, RefreshCw, ShieldAlert, XCircle } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { RunbookApproval } from '@/types';

type Filter = 'pending' | 'all';
type Intent = 'approve' | 'reject';

const STATUS_BADGE_CLASS: Record<string, string> = {
  pending: 'bg-amber-500/10 text-amber-400 border-amber-500/25',
  approved: 'bg-emerald-500/10 text-emerald-400 border-emerald-500/25',
  rejected: 'bg-rose-500/10 text-rose-400 border-rose-500/25',
};

// UI for `runbook_approval_requests` — the backend has generated these for fatal-severity (and
// unsafe critical-severity) alerts for several sessions with zero frontend visibility until
// now. Follows the same local-useState-per-concern, apiFetch-based style as the rest of
// legacy-cockpit-panels.tsx's sub-panels.
export function RunbookApprovalsPanel() {
  const [filter, setFilter] = useState<Filter>('pending');
  const [approvals, setApprovals] = useState<RunbookApproval[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [actioningId, setActioningId] = useState<string | null>(null);
  const [confirmTarget, setConfirmTarget] = useState<RunbookApproval | null>(null);
  const [confirmIntent, setConfirmIntent] = useState<Intent | null>(null);
  const [confirmError, setConfirmError] = useState<string | null>(null);
  const [actionResult, setActionResult] = useState<{ id: string; status: string; output: string } | null>(null);

  const fetchApprovals = async () => {
    setIsLoading(true);
    try {
      const data = await apiFetchJson<RunbookApproval[]>(`/api/v1/runbooks/approvals?status=${filter}`);
      setApprovals(data || []);
    } catch (err) {
      console.error('Failed to fetch runbook approvals:', err);
    } finally {
      setIsLoading(false);
    }
  };

  useEffect(() => {
    fetchApprovals();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [filter]);

  const openConfirm = (approval: RunbookApproval, intent: Intent) => {
    setConfirmTarget(approval);
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
      const endpoint = confirmIntent === 'approve' ? '/api/v1/runbooks/approvals/approve' : '/api/v1/runbooks/approvals/reject';
      const res = await apiFetch(endpoint, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ approval_id: confirmTarget.id }),
      });
      const data = await res.json().catch(() => ({}));
      if (!res.ok) {
        setConfirmError(data.message || data.error || 'Falha ao processar a solicitação.');
        return;
      }
      setActionResult({
        id: confirmTarget.id,
        status: data.status || (confirmIntent === 'approve' ? 'aprovado' : 'rejeitado'),
        output: data.output || data.message || '',
      });
      closeConfirm();
      fetchApprovals();
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
            <ShieldAlert className="w-4 h-4 text-amber-400" /> Aprovações Pendentes
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Auto-remediação crítica/fatal retida para revisão humana antes de executar via SSH
          </p>
        </div>
        <button
          onClick={fetchApprovals}
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
          <span>Carregando aprovações...</span>
        </div>
      ) : approvals.length === 0 ? (
        <div className="p-4 rounded-lg bg-emerald-950/10 border border-emerald-500/10 text-emerald-400 text-xs font-sans flex items-center gap-2">
          <CheckCircle2 className="w-4 h-4 shrink-0" />
          Nenhuma solicitação {filter === 'pending' ? 'pendente' : 'registrada'} no momento.
        </div>
      ) : (
        <div className="flex flex-col gap-3">
          {approvals.map((a) => (
            <div key={a.id} className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-3">
              <div className="flex items-start justify-between gap-4">
                <div className="flex flex-col gap-1 min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="text-sm font-bold text-slate-200">{a.runbook_name}</span>
                    <span className={`px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border ${STATUS_BADGE_CLASS[a.status] || 'bg-slate-500/10 text-slate-400 border-slate-500/25'}`}>
                      {a.status}
                    </span>
                  </div>
                  <span className="text-[10px] text-slate-500 font-mono">Incidente: {a.incident_id}</span>
                  <span className="text-[10px] text-slate-500">
                    Solicitado por <strong className="text-slate-400">{a.requested_by}</strong> em {new Date(a.created_at).toLocaleString()}
                  </span>
                </div>
                {a.status === 'pending' && (
                  <div className="flex items-center gap-2 shrink-0">
                    <button
                      disabled={actioningId === a.id}
                      onClick={() => openConfirm(a, 'reject')}
                      className="px-2.5 py-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 disabled:opacity-50 text-rose-400 text-[10px] font-bold uppercase tracking-wider border border-rose-500/20 transition-all cursor-pointer"
                    >
                      Rejeitar
                    </button>
                    <button
                      disabled={actioningId === a.id}
                      onClick={() => openConfirm(a, 'approve')}
                      className="px-2.5 py-1.5 rounded bg-emerald-500/10 hover:bg-emerald-500/20 disabled:opacity-50 text-emerald-400 text-[10px] font-bold uppercase tracking-wider border border-emerald-500/20 transition-all cursor-pointer"
                    >
                      Aprovar
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

              {actionResult?.id === a.id && (
                <div className="flex flex-col gap-2">
                  <label className="text-[10px] uppercase font-bold text-slate-500">Resultado ({actionResult.status}):</label>
                  <pre className="bg-black border border-white/5 rounded-lg p-3 text-[10px] font-mono text-emerald-400 overflow-x-auto max-h-48 whitespace-pre-wrap select-text leading-relaxed">
                    {actionResult.output || '(sem saída)'}
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
              <AlertTriangle className={`w-4 h-4 ${confirmIntent === 'approve' ? 'text-amber-400' : 'text-rose-400'}`} />
              {confirmIntent === 'approve' ? 'Confirmar Aprovação' : 'Confirmar Rejeição'}
            </DialogTitle>
            <DialogDescription>
              Runbook <strong>{confirmTarget?.runbook_name}</strong> — incidente {confirmTarget?.incident_id}
            </DialogDescription>
          </DialogHeader>

          {confirmTarget?.reason && (
            <textarea readOnly value={confirmTarget.reason} rows={3} className="bg-white/5 border border-white/10 rounded-lg p-3 text-xs text-slate-300 font-sans resize-none focus:outline-none" />
          )}

          {confirmIntent === 'approve' && (
            <p className="text-xs text-amber-400 bg-amber-500/10 border border-amber-500/20 rounded-lg p-3">
              Esta ação executará o script via SSH imediatamente no host do runbook. Esta operação não pode ser desfeita.
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
                confirmIntent === 'approve' ? 'bg-emerald-600 hover:bg-emerald-500 text-slate-950' : 'bg-rose-600 hover:bg-rose-500 text-white'
              }`}
            >
              {actioningId ? (
                <RefreshCw className="w-3.5 h-3.5 animate-spin" />
              ) : confirmIntent === 'approve' ? (
                <CheckCircle2 className="w-3.5 h-3.5" />
              ) : (
                <XCircle className="w-3.5 h-3.5" />
              )}
              {confirmIntent === 'approve' ? 'Confirmar Aprovação' : 'Confirmar Rejeição'}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
