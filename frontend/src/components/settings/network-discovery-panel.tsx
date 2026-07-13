'use client';

import { useCallback, useEffect, useState } from 'react';
import { RefreshCw, Radar, Plus, Trash2, Router, ShieldQuestion, Share2 } from 'lucide-react';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import { useAuth } from '@/lib/auth-context';
import type { DiscoveryTarget, DiscoveredDevice, DiscoveredLink } from '@/types';

// Active network discovery (topology slice A). A tenant admin registers CIDR ranges (with an SNMP
// community) to sweep; the agent probes every host with SNMP and reports back the responders, which
// land in the discovered-device inventory. This finds gear that never sent telemetry.
export function NetworkDiscoveryPanel({ tenantId }: { tenantId?: string }) {
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';

  const [targets, setTargets] = useState<DiscoveryTarget[]>([]);
  const [devices, setDevices] = useState<DiscoveredDevice[]>([]);
  const [links, setLinks] = useState<DiscoveredLink[]>([]);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const [name, setName] = useState('');
  const [cidr, setCidr] = useState('');
  const [community, setCommunity] = useState('');
  const [saving, setSaving] = useState(false);
  const [formError, setFormError] = useState<string | null>(null);

  const qs = tenantId ? `?tenant_id=${tenantId}` : '';

  const load = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const [t, d, l] = await Promise.all([
        apiFetchJson<DiscoveryTarget[]>(`/api/v1/agent/discovery-targets${qs}`),
        apiFetchJson<DiscoveredDevice[]>(`/api/v1/discovered-devices${qs}`),
        apiFetchJson<DiscoveredLink[]>(`/api/v1/discovered-links${qs}`),
      ]);
      setTargets(t ?? []);
      setDevices(d ?? []);
      setLinks(l ?? []);
    } catch (err) {
      console.error('Failed to load network discovery:', err);
      setError('Não foi possível carregar a descoberta de rede.');
    } finally {
      setIsLoading(false);
    }
  }, [qs]);

  useEffect(() => {
    load();
  }, [load]);

  const addTarget = async () => {
    setFormError(null);
    if (!name.trim() || !cidr.trim() || !community.trim()) {
      setFormError('Nome, faixa CIDR e community são obrigatórios.');
      return;
    }
    setSaving(true);
    try {
      const res = await apiFetch(`/api/v1/agent/discovery-targets${qs}`, {
        method: 'POST',
        body: JSON.stringify({ name: name.trim(), cidr: cidr.trim(), community: community.trim() }),
      });
      if (!res.ok) {
        const txt = await res.text();
        setFormError(txt || 'Falha ao criar a faixa de descoberta.');
        return;
      }
      setName('');
      setCidr('');
      setCommunity('');
      await load();
    } catch (err) {
      console.error('Failed to add discovery target:', err);
      setFormError('Falha ao criar a faixa de descoberta.');
    } finally {
      setSaving(false);
    }
  };

  const removeTarget = async (id: string) => {
    try {
      const res = await apiFetch(`/api/v1/agent/discovery-targets?id=${id}${tenantId ? `&tenant_id=${tenantId}` : ''}`, {
        method: 'DELETE',
      });
      if (res.ok) await load();
    } catch (err) {
      console.error('Failed to delete discovery target:', err);
    }
  };

  return (
    <div className="flex flex-col gap-4">
      <div className="glass-card rounded-xl border border-white/5 p-6 bg-[#040812]">
        <div className="flex justify-between items-center mb-4">
          <div className="flex flex-col gap-0.5">
            <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider flex items-center gap-2">
              <Radar className="w-4 h-4 text-cyan-400" /> Descoberta Ativa de Rede
            </h4>
            <p className="text-[10px] text-slate-500 uppercase tracking-wider font-semibold">
              O agente varre as faixas via SNMP (saída 443) e inventaria os dispositivos que respondem
            </p>
          </div>
          <button
            onClick={load}
            disabled={isLoading}
            className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all disabled:opacity-50"
          >
            <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
          </button>
        </div>

        {error && (
          <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300 mb-3">{error}</div>
        )}

        {/* Discovery targets (CIDR ranges) */}
        <div className="mb-4">
          <span className="text-[10px] font-bold text-slate-400 uppercase tracking-widest">Faixas de varredura</span>
          <div className="flex flex-col gap-2 mt-2">
            {targets.length === 0 ? (
              <p className="text-[11px] text-slate-500 py-2">Nenhuma faixa configurada ainda.</p>
            ) : (
              targets.map((t) => (
                <div key={t.id} className="flex items-center justify-between px-3 py-2 rounded-lg bg-white/[0.03] border border-white/5">
                  <div className="flex items-center gap-2 text-xs">
                    <Router className="w-3.5 h-3.5 text-cyan-400" />
                    <strong className="text-slate-200">{t.name}</strong>
                    <span className="font-mono text-slate-400">{t.cidr}</span>
                    <span className="text-[10px] text-slate-500">:{t.port} · v{t.version}</span>
                  </div>
                  {isAdmin && (
                    <button
                      onClick={() => removeTarget(t.id)}
                      className="p-1 rounded text-slate-500 hover:text-rose-400 hover:bg-rose-500/10 transition-all"
                      title="Remover faixa"
                    >
                      <Trash2 className="w-3.5 h-3.5" />
                    </button>
                  )}
                </div>
              ))
            )}
          </div>

          {isAdmin ? (
            <div className="mt-3 flex flex-col gap-2 p-3 rounded-lg bg-white/[0.02] border border-white/5">
              <div className="flex flex-col md:flex-row gap-2">
                <input
                  value={name}
                  onChange={(e) => setName(e.target.value)}
                  placeholder="Nome (ex.: LAN Matriz)"
                  className="flex-1 px-2.5 py-1.5 rounded-md bg-black/40 border border-white/10 text-xs text-slate-200 placeholder:text-slate-600"
                />
                <input
                  value={cidr}
                  onChange={(e) => setCidr(e.target.value)}
                  placeholder="CIDR (ex.: 192.168.1.0/24)"
                  className="flex-1 px-2.5 py-1.5 rounded-md bg-black/40 border border-white/10 text-xs text-slate-200 font-mono placeholder:text-slate-600"
                />
                <input
                  value={community}
                  onChange={(e) => setCommunity(e.target.value)}
                  placeholder="SNMP community"
                  type="password"
                  className="flex-1 px-2.5 py-1.5 rounded-md bg-black/40 border border-white/10 text-xs text-slate-200 placeholder:text-slate-600"
                />
                <button
                  onClick={addTarget}
                  disabled={saving}
                  className="flex items-center justify-center gap-1.5 px-3 py-1.5 rounded-md text-xs font-bold text-cyan-200 bg-cyan-500/15 hover:bg-cyan-500/25 border border-cyan-500/30 transition-all disabled:opacity-50"
                >
                  <Plus className="w-3.5 h-3.5" /> Adicionar
                </button>
              </div>
              {formError && <p className="text-[11px] text-rose-400">{formError}</p>}
              <p className="text-[10px] text-slate-500">
                A community é armazenada criptografada por tenant e só é entregue ao agente autenticado. Máximo /20 (4096 hosts) por faixa.
              </p>
            </div>
          ) : (
            <p className="mt-2 text-[10px] text-slate-500">Somente administradores podem configurar faixas de varredura.</p>
          )}
        </div>
      </div>

      {/* Discovered device inventory */}
      <div className="glass-card rounded-xl border border-white/5 p-6 bg-[#040812]">
        <div className="flex items-center gap-2 mb-3">
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider">Inventário descoberto</h4>
          <span className="text-[10px] font-bold text-slate-500 bg-white/5 px-2 py-0.5 rounded-full">{devices.length}</span>
        </div>
        {devices.length === 0 ? (
          <div className="flex flex-col items-center gap-2 text-slate-500 py-8 text-center">
            <ShieldQuestion className="w-8 h-8 text-slate-600" />
            <p className="text-sm font-bold text-slate-400">Nenhum dispositivo descoberto ainda</p>
            <p className="text-[11px] max-w-md">
              Configure uma faixa de varredura acima e conecte um agente. Os dispositivos que responderem ao SNMP
              aparecem aqui automaticamente no próximo ciclo de descoberta.
            </p>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-[10px] uppercase tracking-wider text-slate-500 border-b border-white/5">
                  <th className="text-left font-bold py-2 px-2">IP</th>
                  <th className="text-left font-bold py-2 px-2">Nome (sysName)</th>
                  <th className="text-left font-bold py-2 px-2">Fabricante</th>
                  <th className="text-left font-bold py-2 px-2">Tipo</th>
                  <th className="text-left font-bold py-2 px-2">Origem</th>
                  <th className="text-left font-bold py-2 px-2">Visto por último</th>
                </tr>
              </thead>
              <tbody>
                {devices.map((d) => (
                  <tr key={d.id} className="border-b border-white/[0.03] hover:bg-white/[0.02]">
                    <td className="py-2 px-2 font-mono text-slate-300">{d.ip}</td>
                    <td className="py-2 px-2 text-slate-200">{d.sysname || (d.mac ? <span className="font-mono text-[10px] text-slate-400">{d.mac}</span> : '—')}</td>
                    <td className="py-2 px-2 text-slate-300">{d.vendor || (d.source === 'arp' ? '—' : 'unknown')}</td>
                    <td className="py-2 px-2">
                      <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-cyan-500/10 text-cyan-300 border border-cyan-500/20">
                        {deviceTypeLabel(d.device_type)}
                      </span>
                    </td>
                    <td className="py-2 px-2">
                      {d.source === 'arp' ? (
                        <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-violet-500/10 text-violet-300 border border-violet-500/20">ARP</span>
                      ) : (
                        <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-emerald-500/10 text-emerald-300 border border-emerald-500/20">SNMP</span>
                      )}
                    </td>
                    <td className="py-2 px-2 text-slate-500">{new Date(d.last_seen).toLocaleString()}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>

      {/* Physical neighbourhood (LLDP/CDP edges) */}
      <div className="glass-card rounded-xl border border-white/5 p-6 bg-[#040812]">
        <div className="flex items-center gap-2 mb-3">
          <Share2 className="w-4 h-4 text-cyan-400" />
          <h4 className="text-sm font-extrabold text-slate-200 uppercase tracking-wider">Vizinhança física (LLDP/CDP)</h4>
          <span className="text-[10px] font-bold text-slate-500 bg-white/5 px-2 py-0.5 rounded-full">{links.length}</span>
        </div>
        {links.length === 0 ? (
          <p className="text-[11px] text-slate-500 py-4">
            Nenhuma adjacência descoberta ainda. As arestas aparecem quando os dispositivos varridos expõem
            vizinhos por LLDP ou CDP no próximo ciclo de descoberta.
          </p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-xs">
              <thead>
                <tr className="text-[10px] uppercase tracking-wider text-slate-500 border-b border-white/5">
                  <th className="text-left font-bold py-2 px-2">Dispositivo local</th>
                  <th className="text-left font-bold py-2 px-2">Porta local</th>
                  <th className="text-left font-bold py-2 px-2">Vizinho</th>
                  <th className="text-left font-bold py-2 px-2">Porta remota</th>
                  <th className="text-left font-bold py-2 px-2">Protocolo</th>
                </tr>
              </thead>
              <tbody>
                {links.map((l) => (
                  <tr key={l.id} className="border-b border-white/[0.03] hover:bg-white/[0.02]">
                    <td className="py-2 px-2 font-mono text-slate-300">{l.local_ip}</td>
                    <td className="py-2 px-2 text-slate-400">{l.local_port || '—'}</td>
                    <td className="py-2 px-2 text-slate-200">{l.remote_sysname || l.remote_chassis_id || '—'}</td>
                    <td className="py-2 px-2 text-slate-400">{l.remote_port_id || '—'}</td>
                    <td className="py-2 px-2">
                      <span className="text-[10px] font-bold px-2 py-0.5 rounded-full bg-violet-500/10 text-violet-300 border border-violet-500/20 uppercase">
                        {l.protocol}
                      </span>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </div>
    </div>
  );
}

function deviceTypeLabel(t: string): string {
  switch (t) {
    case 'firewall':
      return 'Firewall';
    case 'switch':
      return 'Switch';
    case 'router':
      return 'Roteador';
    case 'access_point':
      return 'Access Point';
    case 'server':
      return 'Servidor';
    case 'hypervisor':
      return 'Hypervisor';
    case 'endpoint':
      return 'Endpoint';
    default:
      return 'Rede';
  }
}
