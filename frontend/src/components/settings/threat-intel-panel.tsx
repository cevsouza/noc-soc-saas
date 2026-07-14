'use client';

import { useCallback, useEffect, useState } from 'react';
import { AlertTriangle, Globe, RefreshCw, ShieldCheck, Users, Zap } from 'lucide-react';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import { useAuth } from '@/lib/auth-context';
import type { ThreatIntelResponse } from '@/types';

// Cross-tenant threat-intel panel (Backlog B6 fatia 1). A tenant opts in to both contribute observed
// public IPs and read the anonymized shared aggregate. Opting out hides the feed. The toggle is
// admin-only (enforced server-side too). No tenant identities are ever shown — counts only.
export function ThreatIntelPanel() {
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';
  const [data, setData] = useState<ThreatIntelResponse | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchIntel = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      setData(await apiFetchJson<ThreatIntelResponse>('/api/v1/threat-intel'));
    } catch (err) {
      console.error('Failed to fetch threat intel:', err);
      setError('Não foi possível carregar a inteligência de ameaças.');
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    fetchIntel();
  }, [fetchIntel]);

  const setOptIn = useCallback(
    async (optIn: boolean) => {
      setSaving(true);
      try {
        const res = await apiFetch('/api/v1/threat-intel/settings', {
          method: 'PUT',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ opt_in: optIn }),
        });
        if (!res.ok) throw new Error(`opt-in failed: ${res.status}`);
        await fetchIntel();
      } catch (err) {
        console.error('Failed to change opt-in:', err);
      } finally {
        setSaving(false);
      }
    },
    [fetchIntel],
  );

  const optedIn = data?.opted_in ?? false;

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-start justify-between gap-3">
        <div>
          <h3 className="text-sm font-bold text-slate-200 flex items-center gap-2">
            <Globe className="w-4 h-4 text-cyan-400" /> Inteligência de Ameaças (cross-tenant)
          </h3>
          <p className="text-[11px] text-slate-500 mt-0.5 max-w-2xl">
            Efeito de rede opt-in: ao participar, você contribui os IPs públicos maliciosos que observa e
            passa a ver os indicadores que a frota inteira já viu — tudo anonimizado, sem revelar quais
            tenants viram o quê.
          </p>
        </div>
        <button
          onClick={fetchIntel}
          disabled={isLoading}
          className="px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 disabled:opacity-50 text-slate-300 text-[10px] font-bold uppercase tracking-wider border border-white/10 transition-all cursor-pointer flex items-center gap-1.5 shrink-0"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {/* Opt-in status + toggle */}
      <div className={`rounded-xl border p-4 flex items-center justify-between gap-4 ${optedIn ? 'bg-emerald-950/15 border-emerald-500/20' : 'bg-black/30 border-white/10'}`}>
        <div className="flex items-center gap-3">
          <ShieldCheck className={`w-5 h-5 ${optedIn ? 'text-emerald-400' : 'text-slate-500'}`} />
          <div>
            <div className="text-xs font-bold text-slate-200">
              {optedIn ? 'Participando da rede de inteligência' : 'Fora da rede de inteligência'}
            </div>
            <div className="text-[11px] text-slate-500">
              {optedIn
                ? 'Contribuindo e consumindo indicadores compartilhados.'
                : 'Ative para contribuir e consultar os indicadores compartilhados.'}
            </div>
          </div>
        </div>
        {isAdmin ? (
          <button
            onClick={() => setOptIn(!optedIn)}
            disabled={saving}
            className={`px-4 py-2 rounded-lg text-[10px] font-bold uppercase tracking-wider border transition-all cursor-pointer flex items-center gap-1.5 disabled:opacity-50 ${
              optedIn
                ? 'bg-white/5 hover:bg-white/10 text-slate-300 border-white/10'
                : 'bg-cyan-600/20 hover:bg-cyan-600/30 text-cyan-300 border-cyan-500/25'
            }`}
          >
            {saving && <RefreshCw className="w-3.5 h-3.5 animate-spin" />}
            {optedIn ? 'Sair da rede' : 'Entrar na rede'}
          </button>
        ) : (
          <span className="text-[10px] text-slate-600">Somente admin pode alterar</span>
        )}
      </div>

      {error && (
        <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/20 text-rose-300 text-xs flex items-center gap-2">
          <AlertTriangle className="w-4 h-4 shrink-0" /> {error}
        </div>
      )}

      {/* Shared indicators (only when opted in) */}
      {optedIn && (
        <div className="rounded-xl bg-black/30 border border-white/5 overflow-hidden">
          <div className="flex items-center gap-2 px-4 py-2.5 border-b border-white/5">
            <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">
              Indicadores compartilhados ({data?.indicators.length ?? 0})
            </span>
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-[11px]">
              <thead>
                <tr className="text-slate-500 border-b border-white/5">
                  <th className="text-left font-semibold px-4 py-2">Tipo</th>
                  <th className="text-left font-semibold px-3 py-2">Indicador</th>
                  <th className="text-right font-semibold px-3 py-2">Tenants</th>
                  <th className="text-right font-semibold px-3 py-2">Observações</th>
                  <th className="text-right font-semibold px-4 py-2">Visto por último</th>
                </tr>
              </thead>
              <tbody>
                {(data?.indicators ?? []).map((ind) => (
                  <tr key={`${ind.indicator_type}:${ind.indicator_value}`} className={`border-b border-white/[0.03] hover:bg-white/[0.02] ${ind.tenant_count > 1 ? 'bg-amber-950/10' : ''}`}>
                    <td className="text-left px-4 py-2">
                      <span className="uppercase text-[9px] font-bold text-slate-400 bg-white/5 rounded px-1.5 py-0.5">{ind.indicator_type}</span>
                    </td>
                    <td className="text-left px-3 py-2 font-mono text-slate-300">{ind.indicator_value}</td>
                    <td className="text-right px-3 py-2">
                      <span className={`inline-flex items-center gap-1 ${ind.tenant_count > 1 ? 'text-amber-400 font-bold' : 'text-slate-400'}`}>
                        <Users className="w-3 h-3" /> {ind.tenant_count}
                      </span>
                    </td>
                    <td className="text-right px-3 py-2 text-slate-500">
                      <span className="inline-flex items-center gap-1"><Zap className="w-3 h-3" /> {ind.observation_count}</span>
                    </td>
                    <td className="text-right px-4 py-2 text-slate-500">{new Date(ind.last_seen).toLocaleString()}</td>
                  </tr>
                ))}
                {(data?.indicators.length ?? 0) === 0 && (
                  <tr>
                    <td colSpan={5} className="text-center px-4 py-6 text-slate-600">
                      Nenhum indicador compartilhado ainda. Conforme os tenants participantes observam IPs públicos maliciosos, eles aparecem aqui.
                    </td>
                  </tr>
                )}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}
