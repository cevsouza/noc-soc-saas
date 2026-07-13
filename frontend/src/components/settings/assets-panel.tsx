'use client';

import { useCallback, useEffect, useState } from 'react';
import { RefreshCw, Boxes, Pencil, Plus, Trash2, ShieldQuestion } from 'lucide-react';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import { useAuth } from '@/lib/auth-context';
import type { AssetView, BusinessCriticality } from '@/types';

// CMDB / Ativos (topology slice T2). Lists the merged inventory (discovered devices + manual assets)
// and lets a tenant admin annotate each with business criticality, owner, location, tags and notes —
// or register a manual asset that has no SNMP at all. The criticality is exactly what the dynamic risk
// score needs as "asset criticality".
export function AssetsPanel({ tenantId }: { tenantId?: string }) {
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';

  const [assets, setAssets] = useState<AssetView[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [editing, setEditing] = useState<AssetView | null>(null);
  const [isNew, setIsNew] = useState(false);

  const qs = tenantId ? `?tenant_id=${tenantId}` : '';

  const load = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const list = await apiFetchJson<AssetView[]>(`/api/v1/assets${qs}`);
      setAssets(list ?? []);
    } catch (err) {
      console.error('Failed to load assets:', err);
      setError('Não foi possível carregar o inventário de ativos.');
    } finally {
      setIsLoading(false);
    }
  }, [qs]);

  useEffect(() => {
    load();
  }, [load]);

  const managedCount = assets.filter((a) => a.managed).length;

  const openEdit = (a: AssetView) => {
    setIsNew(false);
    setEditing(a);
  };
  const openNew = () => {
    setIsNew(true);
    setEditing({
      identifier: '',
      name: '',
      asset_type: '',
      vendor: '',
      business_criticality: 'medium',
      owner: '',
      location: '',
      tags: [],
      aliases: [],
      notes: '',
      managed: true,
      discovered: false,
    });
  };

  const removeAnnotation = async (identifier: string) => {
    try {
      const res = await apiFetch(`/api/v1/assets?identifier=${encodeURIComponent(identifier)}${tenantId ? `&tenant_id=${tenantId}` : ''}`, {
        method: 'DELETE',
      });
      if (res.ok) await load();
    } catch (err) {
      console.error('Failed to delete asset annotation:', err);
    }
  };

  return (
    <div className="glass-card rounded-xl border border-white/5 p-6 bg-[#040812]">
      <div className="flex justify-between items-center mb-4">
        <div className="flex flex-col gap-0.5">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
            <Boxes className="w-4 h-4 text-cyan-400" /> CMDB &amp; Ativos
          </h4>
          <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
            {assets.length} ativo(s) · {managedCount} gerenciado(s) · inventário descoberto + registros manuais
          </p>
        </div>
        <div className="flex items-center gap-2">
          {isAdmin && (
            <button
              onClick={openNew}
              className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-cyan-200 bg-cyan-500/15 hover:bg-cyan-500/25 border border-cyan-500/30 transition-all"
            >
              <Plus className="w-3.5 h-3.5" /> Ativo manual
            </button>
          )}
          <button
            onClick={load}
            disabled={isLoading}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all disabled:opacity-50"
          >
            <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
          </button>
        </div>
      </div>

      {error && (
        <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300 mb-3">{error}</div>
      )}

      {assets.length === 0 ? (
        <div className="flex flex-col items-center gap-2 text-slate-500 py-10 text-center">
          <ShieldQuestion className="w-8 h-8 text-slate-600" />
          <p className="text-sm font-bold text-slate-400">Nenhum ativo ainda</p>
          <p className="text-[11px] max-w-md">
            Os ativos aparecem conforme a descoberta ativa inventaria dispositivos. Você também pode
            cadastrar um <strong>ativo manual</strong> (ex.: um serviço em nuvem sem SNMP) e definir sua
            criticidade de negócio, dono e local.
          </p>
        </div>
      ) : (
        <div className="overflow-x-auto">
          <table className="w-full text-xs">
            <thead>
              <tr className="text-[10px] uppercase tracking-wider text-slate-500 border-b border-white/5">
                <th className="text-left font-bold py-2 px-2">Identificador</th>
                <th className="text-left font-bold py-2 px-2">Nome</th>
                <th className="text-left font-bold py-2 px-2">Tipo</th>
                <th className="text-left font-bold py-2 px-2">Criticidade</th>
                <th className="text-left font-bold py-2 px-2">Dono</th>
                <th className="text-left font-bold py-2 px-2">Local</th>
                <th className="text-left font-bold py-2 px-2">Origem</th>
                {isAdmin && <th className="text-right font-bold py-2 px-2">Ações</th>}
              </tr>
            </thead>
            <tbody>
              {assets.map((a) => (
                <tr key={a.identifier} className="border-b border-white/[0.03] hover:bg-white/[0.02]">
                  <td className="py-2 px-2 font-mono text-slate-300">{a.identifier}</td>
                  <td className="py-2 px-2 text-slate-200">{a.name || '—'}</td>
                  <td className="py-2 px-2 text-slate-400">{a.asset_type || '—'}</td>
                  <td className="py-2 px-2"><CriticalityBadge level={a.business_criticality} /></td>
                  <td className="py-2 px-2 text-slate-400">{a.owner || '—'}</td>
                  <td className="py-2 px-2 text-slate-400">{a.location || '—'}</td>
                  <td className="py-2 px-2">
                    <div className="flex gap-1">
                      {a.discovered && (
                        <span className="text-[9px] font-bold px-1.5 py-0.5 rounded-full bg-cyan-500/10 text-cyan-300 border border-cyan-500/20">SNMP</span>
                      )}
                      {a.managed ? (
                        <span className="text-[9px] font-bold px-1.5 py-0.5 rounded-full bg-emerald-500/10 text-emerald-300 border border-emerald-500/20">GERENCIADO</span>
                      ) : (
                        <span className="text-[9px] font-bold px-1.5 py-0.5 rounded-full bg-slate-500/10 text-slate-400 border border-slate-500/20">NÃO GERENCIADO</span>
                      )}
                    </div>
                  </td>
                  {isAdmin && (
                    <td className="py-2 px-2">
                      <div className="flex items-center justify-end gap-1">
                        <button
                          onClick={() => openEdit(a)}
                          className="p-1 rounded text-slate-500 hover:text-cyan-300 hover:bg-cyan-500/10 transition-all"
                          title="Editar atributos"
                        >
                          <Pencil className="w-3.5 h-3.5" />
                        </button>
                        {a.managed && (
                          <button
                            onClick={() => removeAnnotation(a.identifier)}
                            className="p-1 rounded text-slate-500 hover:text-rose-400 hover:bg-rose-500/10 transition-all"
                            title="Remover anotação de CMDB"
                          >
                            <Trash2 className="w-3.5 h-3.5" />
                          </button>
                        )}
                      </div>
                    </td>
                  )}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {editing && (
        <AssetEditDialog
          asset={editing}
          isNew={isNew}
          tenantId={tenantId}
          onClose={() => setEditing(null)}
          onSaved={async () => {
            setEditing(null);
            await load();
          }}
        />
      )}
    </div>
  );
}

function AssetEditDialog({
  asset,
  isNew,
  tenantId,
  onClose,
  onSaved,
}: {
  asset: AssetView;
  isNew: boolean;
  tenantId?: string;
  onClose: () => void;
  onSaved: () => void;
}) {
  const [identifier, setIdentifier] = useState(asset.identifier);
  const [name, setName] = useState(asset.name);
  const [assetType, setAssetType] = useState(asset.asset_type);
  const [vendor, setVendor] = useState(asset.vendor);
  const [criticality, setCriticality] = useState<BusinessCriticality>(asset.business_criticality);
  const [owner, setOwner] = useState(asset.owner);
  const [location, setLocation] = useState(asset.location);
  const [tags, setTags] = useState(asset.tags.join(', '));
  const [aliases, setAliases] = useState(asset.aliases.join(', '));
  const [notes, setNotes] = useState(asset.notes);
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const save = async () => {
    setFormError(null);
    if (!identifier.trim() || !name.trim()) {
      setFormError('Identificador e nome são obrigatórios.');
      return;
    }
    setSaving(true);
    try {
      const body = {
        identifier: identifier.trim(),
        name: name.trim(),
        asset_type: assetType.trim(),
        vendor: vendor.trim(),
        business_criticality: criticality,
        owner: owner.trim(),
        location: location.trim(),
        tags: tags.split(',').map((t) => t.trim()).filter(Boolean),
        aliases: aliases.split(',').map((t) => t.trim()).filter(Boolean),
        notes: notes.trim(),
      };
      const qs = tenantId ? `?tenant_id=${tenantId}` : '';
      const res = await apiFetch(`/api/v1/assets${qs}`, { method: 'POST', body: JSON.stringify(body) });
      if (!res.ok) {
        const txt = await res.text();
        setFormError(txt || 'Falha ao salvar o ativo.');
        return;
      }
      onSaved();
    } catch (err) {
      console.error('Failed to save asset:', err);
      setFormError('Falha ao salvar o ativo.');
    } finally {
      setSaving(false);
    }
  };

  const inputCls = 'w-full px-2.5 py-1.5 rounded-md bg-black/40 border border-white/10 text-xs text-slate-200 placeholder:text-slate-600';

  return (
    <Dialog open onOpenChange={(o) => !o && onClose()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <Boxes className="w-4 h-4 text-cyan-400" /> {isNew ? 'Novo ativo manual' : 'Editar ativo'}
          </DialogTitle>
          <DialogDescription>
            {asset.discovered
              ? 'Ativo descoberto por SNMP — os campos manuais complementam a identidade descoberta.'
              : 'Ativo manual (sem SNMP). O identificador liga o ativo ao stream de alertas (IP ou hostname).'}
          </DialogDescription>
        </DialogHeader>

        <div className="flex flex-col gap-3 py-1">
          <div className="grid grid-cols-2 gap-2">
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Identificador (IP/hostname)
              <input
                value={identifier}
                onChange={(e) => setIdentifier(e.target.value)}
                disabled={!isNew}
                placeholder="192.168.1.1 ou svc-billing"
                className={`${inputCls} font-mono disabled:opacity-60`}
              />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Nome
              <input value={name} onChange={(e) => setName(e.target.value)} placeholder="Firewall Matriz" className={inputCls} />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Tipo
              <input value={assetType} onChange={(e) => setAssetType(e.target.value)} placeholder="firewall / switch / service" className={inputCls} />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Fabricante
              <input value={vendor} onChange={(e) => setVendor(e.target.value)} placeholder="Fortinet" className={inputCls} />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Criticidade de negócio
              <select value={criticality} onChange={(e) => setCriticality(e.target.value as BusinessCriticality)} className={inputCls}>
                <option value="low">Baixa</option>
                <option value="medium">Média</option>
                <option value="high">Alta</option>
                <option value="critical">Crítica</option>
              </select>
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Dono
              <input value={owner} onChange={(e) => setOwner(e.target.value)} placeholder="NetOps" className={inputCls} />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Local
              <input value={location} onChange={(e) => setLocation(e.target.value)} placeholder="Datacenter SP" className={inputCls} />
            </label>
            <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
              Tags (separadas por vírgula)
              <input value={tags} onChange={(e) => setTags(e.target.value)} placeholder="perímetro, produção" className={inputCls} />
            </label>
          </div>
          <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
            Aliases de host (separados por vírgula)
            <input value={aliases} onChange={(e) => setAliases(e.target.value)} placeholder="edge-fw.corp.local, fw01.mon" className={`${inputCls} font-mono`} />
            <span className="text-[10px] font-normal text-slate-500">
              Outros nomes/IPs que as ferramentas de monitoramento usam para este ativo — o grafo de
              topologia funde esses hosts neste nó (elimina duplicatas).
            </span>
          </label>
          <label className="flex flex-col gap-1 text-[11px] font-bold text-slate-400">
            Notas
            <textarea value={notes} onChange={(e) => setNotes(e.target.value)} rows={2} className={inputCls} />
          </label>
          {formError && <p className="text-[11px] text-rose-400">{formError}</p>}
        </div>

        <DialogFooter>
          <button onClick={onClose} className="px-3 py-1.5 rounded-md text-xs font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all">
            Cancelar
          </button>
          <button
            onClick={save}
            disabled={saving}
            className="px-3 py-1.5 rounded-md text-xs font-bold text-cyan-100 bg-cyan-500/20 hover:bg-cyan-500/30 border border-cyan-500/30 transition-all disabled:opacity-50"
          >
            {saving ? 'Salvando…' : 'Salvar'}
          </button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function CriticalityBadge({ level }: { level: BusinessCriticality }) {
  const map: Record<BusinessCriticality, { cls: string; label: string }> = {
    critical: { cls: 'bg-rose-500/15 text-rose-300 border-rose-500/30', label: 'Crítica' },
    high: { cls: 'bg-amber-500/15 text-amber-300 border-amber-500/30', label: 'Alta' },
    medium: { cls: 'bg-cyan-500/10 text-cyan-300 border-cyan-500/20', label: 'Média' },
    low: { cls: 'bg-slate-500/10 text-slate-400 border-slate-500/20', label: 'Baixa' },
  };
  const c = map[level] ?? map.medium;
  return <span className={`text-[10px] font-bold px-2 py-0.5 rounded-full border ${c.cls}`}>{c.label}</span>;
}
