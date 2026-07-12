'use client';

import { useEffect, useState } from 'react';
import { CheckCircle2, RefreshCw, ShieldCheck, UserCog } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { AdminUser, Tenant, TenantAccessGrant } from '@/types';

type PendingChange = {
  tenantId: string;
  tenantName: string;
  intent: 'grant' | 'revoke';
};

// Admin-only security screen (Fase 5 fatia 1): grant an operator access to specific tenants,
// one at a time, by populating tenant_users — the table every tenant-scope authorization check
// already consumes. Self-contained (fetches its own users/tenants) in the same
// local-useState + apiFetch style as RunbookApprovalsPanel, rather than living inline in the
// legacy monolith.
export function AccessControlPanel() {
  const [users, setUsers] = useState<AdminUser[]>([]);
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [selectedUserId, setSelectedUserId] = useState<string>('');
  const [grants, setGrants] = useState<TenantAccessGrant[]>([]);
  const [isLoadingLists, setIsLoadingLists] = useState(false);
  const [isLoadingGrants, setIsLoadingGrants] = useState(false);
  const [actioningTenantId, setActioningTenantId] = useState<string | null>(null);
  const [pending, setPending] = useState<PendingChange | null>(null);
  const [pendingError, setPendingError] = useState<string | null>(null);

  const selectedUser = users.find((u) => u.id === selectedUserId) || null;
  const grantedTenantIds = new Set(grants.map((g) => g.tenant_id));

  const fetchLists = async () => {
    setIsLoadingLists(true);
    try {
      const [userList, tenantList] = await Promise.all([
        apiFetchJson<AdminUser[]>('/api/v1/admin/users'),
        apiFetchJson<Tenant[]>('/api/v1/tenants'),
      ]);
      setUsers(userList || []);
      setTenants(tenantList || []);
    } catch (err) {
      console.error('Failed to load users/tenants for access control:', err);
    } finally {
      setIsLoadingLists(false);
    }
  };

  const fetchGrants = async (userId: string) => {
    if (!userId) {
      setGrants([]);
      return;
    }
    setIsLoadingGrants(true);
    try {
      const data = await apiFetchJson<TenantAccessGrant[]>(`/api/v1/admin/access?user_id=${userId}`);
      setGrants(data || []);
    } catch (err) {
      console.error('Failed to load access grants:', err);
      setGrants([]);
    } finally {
      setIsLoadingGrants(false);
    }
  };

  useEffect(() => {
    fetchLists();
  }, []);

  useEffect(() => {
    fetchGrants(selectedUserId);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedUserId]);

  const openConfirm = (tenant: Tenant, intent: 'grant' | 'revoke') => {
    setPending({ tenantId: tenant.id, tenantName: tenant.name, intent });
    setPendingError(null);
  };

  const closeConfirm = () => {
    setPending(null);
    setPendingError(null);
  };

  const handleConfirm = async () => {
    if (!pending || !selectedUserId) return;
    setActioningTenantId(pending.tenantId);
    setPendingError(null);
    try {
      const res =
        pending.intent === 'grant'
          ? await apiFetch('/api/v1/admin/access', {
              method: 'POST',
              headers: { 'Content-Type': 'application/json' },
              body: JSON.stringify({ user_id: selectedUserId, tenant_id: pending.tenantId }),
            })
          : await apiFetch(`/api/v1/admin/access?user_id=${selectedUserId}&tenant_id=${pending.tenantId}`, {
              method: 'DELETE',
            });

      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        setPendingError(data.message || data.error || 'Falha ao aplicar a alteração de acesso.');
        return;
      }
      closeConfirm();
      await fetchGrants(selectedUserId);
    } catch (err) {
      setPendingError('Erro de conectividade com o backend.');
    } finally {
      setActioningTenantId(null);
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between border-b border-white/5 pb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <ShieldCheck className="w-4 h-4 text-emerald-400" /> Segurança de Acessos
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Autorize um operador a acessar tenants específicos, um a um
          </p>
        </div>
        <button
          onClick={fetchLists}
          disabled={isLoadingLists}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoadingLists ? 'animate-spin' : ''}`} />
          <span>Atualizar</span>
        </button>
      </div>

      {/* User selector */}
      <div className="flex flex-col gap-2">
        <label className="text-[10px] uppercase font-bold text-slate-500 flex items-center gap-1.5">
          <UserCog className="w-3.5 h-3.5 text-violet-400" /> Usuário
        </label>
        <select
          value={selectedUserId}
          onChange={(e) => setSelectedUserId(e.target.value)}
          className="bg-black/40 border border-white/10 rounded-lg px-3 py-2 text-xs text-slate-200 focus:outline-none focus:border-violet-500/50 cursor-pointer"
        >
          <option value="">Selecione um usuário...</option>
          {users.map((u) => (
            <option key={u.id} value={u.id}>
              {u.name} ({u.email}){u.global_role === 'admin' ? ' — admin de plataforma' : ''}
            </option>
          ))}
        </select>
      </div>

      {!selectedUserId ? (
        <div className="p-4 rounded-lg bg-white/[0.02] border border-white/5 text-slate-500 text-xs">
          Selecione um usuário para gerenciar seus acessos por tenant.
        </div>
      ) : selectedUser?.global_role === 'admin' ? (
        <div className="p-4 rounded-lg bg-blue-950/20 border border-blue-500/15 text-blue-300 text-xs flex items-center gap-2">
          <ShieldCheck className="w-4 h-4 shrink-0" />
          Este usuário é <strong>admin de plataforma</strong> e já acessa todos os tenants — concessões individuais não são necessárias.
        </div>
      ) : (
        <div className="flex flex-col gap-2">
          <label className="text-[10px] uppercase font-bold text-slate-500">
            Tenants ({grantedTenantIds.size} de {tenants.length} autorizados)
          </label>
          {isLoadingGrants || isLoadingLists ? (
            <div className="flex items-center justify-center py-8 gap-2 text-xs text-slate-500">
              <RefreshCw className="w-4 h-4 animate-spin text-violet-500" />
              <span>Carregando acessos...</span>
            </div>
          ) : (
            <div className="flex flex-col gap-2">
              {tenants.map((t) => {
                const granted = grantedTenantIds.has(t.id);
                const busy = actioningTenantId === t.id;
                return (
                  <div
                    key={t.id}
                    className="flex items-center justify-between gap-3 p-3 rounded-lg bg-black/40 border border-white/5"
                  >
                    <div className="flex items-center gap-2 min-w-0">
                      <span className={`w-2 h-2 rounded-full shrink-0 ${granted ? 'bg-emerald-400' : 'bg-slate-600'}`} />
                      <span className="text-xs font-bold text-slate-200 truncate">{t.name}</span>
                      {granted && (
                        <span className="px-2 py-0.5 rounded text-[9px] font-extrabold uppercase tracking-wider border bg-emerald-500/10 text-emerald-400 border-emerald-500/25">
                          Autorizado
                        </span>
                      )}
                    </div>
                    <button
                      disabled={busy}
                      onClick={() => openConfirm(t, granted ? 'revoke' : 'grant')}
                      className={`px-3 py-1.5 rounded text-[10px] font-bold uppercase tracking-wider border transition-all cursor-pointer disabled:opacity-50 shrink-0 ${
                        granted
                          ? 'bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border-rose-500/20'
                          : 'bg-emerald-500/10 hover:bg-emerald-500/20 text-emerald-400 border-emerald-500/20'
                      }`}
                    >
                      {busy ? <RefreshCw className="w-3 h-3 animate-spin" /> : granted ? 'Revogar' : 'Conceder'}
                    </button>
                  </div>
                );
              })}
              {tenants.length === 0 && (
                <div className="p-4 rounded-lg bg-white/[0.02] border border-white/5 text-slate-500 text-xs">
                  Nenhum tenant cadastrado.
                </div>
              )}
            </div>
          )}
        </div>
      )}

      <Dialog open={!!pending} onOpenChange={(open) => !open && closeConfirm()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ShieldCheck className={`w-4 h-4 ${pending?.intent === 'grant' ? 'text-emerald-400' : 'text-rose-400'}`} />
              {pending?.intent === 'grant' ? 'Conceder Acesso' : 'Revogar Acesso'}
            </DialogTitle>
            <DialogDescription>
              {pending?.intent === 'grant' ? 'Conceder a ' : 'Revogar de '}
              <strong>{selectedUser?.name}</strong> o acesso ao tenant <strong>{pending?.tenantName}</strong>
              {pending?.intent === 'grant' ? ' (nível operador).' : '.'}
            </DialogDescription>
          </DialogHeader>

          {pending?.intent === 'revoke' && (
            <p className="text-xs text-amber-400 bg-amber-500/10 border border-amber-500/20 rounded-lg p-3">
              O usuário perde acesso a este tenant nas próximas requisições. Se este era o tenant principal da sessão dele, ele continuará vendo-o até o próximo login.
            </p>
          )}

          {pendingError && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-3">{pendingError}</p>}

          <DialogFooter>
            <button
              onClick={closeConfirm}
              disabled={!!actioningTenantId}
              className="px-4 py-2 rounded-lg bg-white/5 hover:bg-white/10 text-slate-300 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50"
            >
              Cancelar
            </button>
            <button
              onClick={handleConfirm}
              disabled={!!actioningTenantId}
              className={`px-4 py-2 rounded-lg text-xs font-bold uppercase tracking-wider transition-all cursor-pointer disabled:opacity-50 flex items-center justify-center gap-2 ${
                pending?.intent === 'grant' ? 'bg-emerald-600 hover:bg-emerald-500 text-slate-950' : 'bg-rose-600 hover:bg-rose-500 text-white'
              }`}
            >
              {actioningTenantId ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <CheckCircle2 className="w-3.5 h-3.5" />}
              {pending?.intent === 'grant' ? 'Confirmar Concessão' : 'Confirmar Revogação'}
            </button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
