'use client';

import { useCallback, useEffect, useState } from 'react';
import { BellOff, Plus, RefreshCw, Trash2 } from 'lucide-react';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import type { SuppressionRule } from '@/types';

const MATCH_FIELDS = ['event_type', 'host', 'summary', 'source', 'severity'];

// Temporal suppression rules (Fase 3/3d): silence alerts matching a field/pattern, optionally only
// within a time window (maintenance). Self-contained MSP panel, same style as the other settings
// panels. Create/delete require tenant-admin (enforced by the backend); this panel is shown to
// platform/tenant admins in the sidebar.
export function SuppressionRulesPanel({ tenantId }: { tenantId?: string }) {
  const qtenant = tenantId ? `?tenant_id=${tenantId}` : '';
  const [rules, setRules] = useState<SuppressionRule[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [name, setName] = useState('');
  const [matchField, setMatchField] = useState('event_type');
  const [matchValue, setMatchValue] = useState('');
  const [startsAt, setStartsAt] = useState('');
  const [endsAt, setEndsAt] = useState('');
  const [saving, setSaving] = useState(false);

  const fetchRules = useCallback(async () => {
    setIsLoading(true);
    try {
      const data = await apiFetchJson<SuppressionRule[]>(`/api/v1/suppression-rules${qtenant}`);
      setRules(data || []);
    } catch (err) {
      console.error('Failed to fetch suppression rules:', err);
    } finally {
      setIsLoading(false);
    }
  }, [qtenant]);

  useEffect(() => {
    fetchRules();
  }, [fetchRules]);

  const createRule = async () => {
    setError(null);
    if (!name.trim() || !matchValue.trim()) {
      setError('Nome e valor são obrigatórios.');
      return;
    }
    setSaving(true);
    try {
      const body: Record<string, unknown> = { name: name.trim(), match_field: matchField, match_value: matchValue.trim() };
      if (startsAt) body.starts_at = new Date(startsAt).toISOString();
      if (endsAt) body.ends_at = new Date(endsAt).toISOString();
      const res = await apiFetch(`/api/v1/suppression-rules${qtenant}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      if (!res.ok) {
        const data = await res.json().catch(() => ({}));
        setError(data.message || data.error || 'Falha ao criar a regra.');
        return;
      }
      setName('');
      setMatchValue('');
      setStartsAt('');
      setEndsAt('');
      fetchRules();
    } catch {
      setError('Erro de conectividade com o backend.');
    } finally {
      setSaving(false);
    }
  };

  const deleteRule = async (id: string) => {
    try {
      const sep = qtenant ? '&' : '?';
      await apiFetch(`/api/v1/suppression-rules${qtenant}${sep}id=${id}`, { method: 'DELETE' });
      fetchRules();
    } catch (err) {
      console.error('Failed to delete rule:', err);
    }
  };

  return (
    <div className="flex flex-col gap-5">
      <div className="flex items-center justify-between border-b border-white/5 pb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <BellOff className="w-4 h-4 text-amber-400" /> Regras de Supressão
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            Silencie alertas que casem um campo/padrão — opcionalmente só numa janela (manutenção)
          </p>
        </div>
        <button
          onClick={fetchRules}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-3 py-1.5 rounded-lg bg-white/5 hover:bg-white/10 border border-white/10 text-xs text-slate-300 font-bold transition-all cursor-pointer disabled:opacity-50"
        >
          <RefreshCw className={`w-3.5 h-3.5 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {/* Create form */}
      <div className="p-4 rounded-xl bg-black/40 border border-white/5 flex flex-col gap-3">
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3">
          <input
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Nome (ex: Manutenção DB)"
            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-amber-500"
          />
          <select
            value={matchField}
            onChange={(e) => setMatchField(e.target.value)}
            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-amber-500"
          >
            {MATCH_FIELDS.map((f) => (
              <option key={f} value={f}>
                {f}
              </option>
            ))}
          </select>
          <input
            value={matchValue}
            onChange={(e) => setMatchValue(e.target.value)}
            placeholder="Contém… (ex: disk_full)"
            className="bg-[#0b0f19] border border-white/10 rounded-lg p-2.5 text-xs text-white focus:outline-none focus:border-amber-500"
          />
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-3 gap-3 items-end">
          <label className="flex flex-col gap-1 text-[10px] text-slate-500 uppercase font-bold">
            Início (opcional)
            <input type="datetime-local" value={startsAt} onChange={(e) => setStartsAt(e.target.value)} className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-amber-500" />
          </label>
          <label className="flex flex-col gap-1 text-[10px] text-slate-500 uppercase font-bold">
            Fim (opcional)
            <input type="datetime-local" value={endsAt} onChange={(e) => setEndsAt(e.target.value)} className="bg-[#0b0f19] border border-white/10 rounded-lg p-2 text-xs text-white focus:outline-none focus:border-amber-500" />
          </label>
          <button
            onClick={createRule}
            disabled={saving}
            className="flex items-center justify-center gap-2 px-4 py-2.5 rounded-lg bg-amber-600 hover:bg-amber-500 disabled:opacity-50 text-slate-950 text-xs font-bold uppercase tracking-wider transition-all cursor-pointer"
          >
            {saving ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />} Criar regra
          </button>
        </div>
        {error && <p className="text-xs text-rose-400 bg-rose-500/10 border border-rose-500/20 rounded-lg p-2.5">{error}</p>}
      </div>

      {/* List */}
      {isLoading && rules.length === 0 ? (
        <div className="text-xs text-slate-500 py-6 text-center">Carregando…</div>
      ) : rules.length === 0 ? (
        <div className="text-xs text-slate-500 py-6 text-center">Nenhuma regra de supressão cadastrada.</div>
      ) : (
        <div className="flex flex-col gap-2">
          {rules.map((r) => (
            <div key={r.id} className="p-3 rounded-lg bg-white/[0.02] border border-white/5 flex items-center justify-between gap-3">
              <div className="flex flex-col gap-0.5 min-w-0">
                <div className="flex items-center gap-2 flex-wrap">
                  <span className="text-xs font-bold text-slate-200">{r.name}</span>
                  <code className="text-[10px] text-amber-300">{r.match_field} ⊃ &quot;{r.match_value}&quot;</code>
                  {!r.active && <span className="text-[9px] text-slate-500 uppercase font-bold">inativa</span>}
                </div>
                <span className="text-[10px] text-slate-500">
                  {r.starts_at || r.ends_at
                    ? `Janela: ${r.starts_at ? new Date(r.starts_at).toLocaleString() : '—'} → ${r.ends_at ? new Date(r.ends_at).toLocaleString() : '—'}`
                    : 'Sempre ativa'}
                </span>
              </div>
              <button
                onClick={() => deleteRule(r.id)}
                className="p-1.5 rounded bg-rose-500/10 hover:bg-rose-500/20 text-rose-400 border border-rose-500/20 transition-all cursor-pointer shrink-0"
                title="Excluir regra"
              >
                <Trash2 className="w-3.5 h-3.5" />
              </button>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}
