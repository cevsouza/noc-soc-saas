'use client';

import { useCallback, useEffect, useState } from 'react';
import { Check, Copy, KeyRound, Plug, Plus, RefreshCw, ShieldAlert, Trash2, Zap } from 'lucide-react';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import { API_BASE_URL } from '@/lib/env';
import type { ApiKeyInfo, CreatedApiKey } from '@/types';

// In-app connection guide (didactic onboarding). Answers the question every new user has — "how do I
// send data in?" — without reading the source: the exact ingest URL per connector, a self-service
// API key manager, and a ready-to-run example. Keys are scoped to the selected tenant.

interface ConnectorMeta {
  type: string;
  label: string;
  path: string;
  hint?: string; // where to configure the webhook inside that vendor's own UI
}

const CONNECTORS: ConnectorMeta[] = [
  { type: 'generic', label: 'Genérico (formato normalizado)', path: '/api/v1/ingest' },
  {
    type: 'prometheus', label: 'Prometheus Alertmanager', path: '/api/v1/ingest/prometheus',
    hint: 'No arquivo alertmanager.yml, adicione um receiver com webhook_configs → url apontando para esta URL.',
  },
  {
    type: 'grafana', label: 'Grafana Alerting', path: '/api/v1/ingest/grafana',
    hint: 'Grafana → Alerting → Contact points → Add contact point → Integration: Webhook → cole esta URL. Depois aponte a policy para ele.',
  },
  {
    type: 'zabbix', label: 'Zabbix', path: '/api/v1/ingest/zabbix',
    hint: 'Zabbix → Alertas → Tipos de mídia → Webhook. Parâmetros: alert_subject={ALERT.SUBJECT}, alert_message={ALERT.MESSAGE}, host={HOST.NAME}, severity={EVENT.SEVERITY}, trigger_id={TRIGGER.ID}, event_id={EVENT.ID}, event_value={EVENT.VALUE}. No script, faça req.post(<esta URL>, value).',
  },
  {
    type: 'wazuh', label: 'Wazuh SIEM', path: '/api/v1/ingest/wazuh',
    hint: 'No /var/ossec/etc/ossec.conf, adicione um bloco <integration> com <hook_url> apontando para esta URL e <alert_format>json</alert_format>. Reinicie o wazuh-manager.',
  },
  {
    type: 'uptimekuma', label: 'Uptime Kuma', path: '/api/v1/ingest/uptimekuma',
    hint: 'Uptime Kuma → Settings → Notifications → Setup Notification → Webhook. Content Type application/json e Post URL = esta URL.',
  },
  { type: 'otlp', label: 'OpenTelemetry (OTLP/HTTP)', path: '/api/v1/ingest/otlp' },
  { type: 'icinga', label: 'Icinga / Nagios', path: '/api/v1/ingest/icinga' },
  { type: 'azuremonitor', label: 'Azure Monitor', path: '/api/v1/ingest/azuremonitor' },
  { type: 'cloudwatch', label: 'AWS CloudWatch (SNS)', path: '/api/v1/ingest/cloudwatch' },
  { type: 'pagerduty', label: 'PagerDuty', path: '/api/v1/ingest/pagerduty' },
  { type: 'opsgenie', label: 'Opsgenie', path: '/api/v1/ingest/opsgenie' },
  { type: 'crowdstrike', label: 'CrowdStrike Falcon (EDR)', path: '/api/v1/ingest/crowdstrike' },
  { type: 'paloalto', label: 'Palo Alto (firewall)', path: '/api/v1/ingest/paloalto' },
  { type: 'fortinet', label: 'FortiGate (firewall)', path: '/api/v1/ingest/fortinet' },
];

function CopyButton({ text, label }: { text: string; label?: string }) {
  const [done, setDone] = useState(false);
  return (
    <button
      type="button"
      onClick={() => {
        navigator.clipboard.writeText(text).then(() => {
          setDone(true);
          setTimeout(() => setDone(false), 1400);
        });
      }}
      className="inline-flex items-center gap-1 px-2 py-1 rounded-md bg-white/5 hover:bg-white/10 border border-white/10 text-[10px] font-bold uppercase tracking-wider text-slate-300 transition-all cursor-pointer shrink-0"
    >
      {done ? <Check className="w-3 h-3 text-emerald-400" /> : <Copy className="w-3 h-3" />}
      {done ? 'copiado' : label || 'copiar'}
    </button>
  );
}

export function ConnectionGuidePanel({ tenantId, connectorType }: { tenantId?: string; connectorType?: string }) {
  // When rendered inside a specific connector ("Configurar Zabbix"), focus the guide on just that
  // connector; on the standalone "Como Conectar" tab, show the full multi-connector reference.
  const focused = connectorType ? CONNECTORS.find((c) => c.type === connectorType) : undefined;
  const [keys, setKeys] = useState<ApiKeyInfo[]>([]);
  const [loading, setLoading] = useState(false);
  const [creating, setCreating] = useState(false);
  const [newName, setNewName] = useState('');
  const [freshKey, setFreshKey] = useState<CreatedApiKey | null>(null);
  const [error, setError] = useState<string | null>(null);

  const q = tenantId ? `?tenant_id=${tenantId}` : '';

  const fetchKeys = useCallback(async () => {
    setLoading(true);
    setError(null);
    try {
      const data = await apiFetchJson<{ keys: ApiKeyInfo[] }>(`/api/v1/integrations/api-keys${q}`);
      setKeys(data.keys || []);
    } catch (err) {
      console.error('Failed to list API keys:', err);
      setError('Não foi possível carregar as chaves. É necessário ser administrador do cliente.');
    } finally {
      setLoading(false);
    }
  }, [q]);

  useEffect(() => {
    fetchKeys();
  }, [fetchKeys]);

  const createKey = async () => {
    setCreating(true);
    setError(null);
    try {
      const created = await apiFetchJson<CreatedApiKey>(`/api/v1/integrations/api-keys${q}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: newName.trim() || 'Chave de ingestão' }),
      });
      setFreshKey(created);
      setNewName('');
      await fetchKeys();
    } catch (err) {
      console.error('Failed to create API key:', err);
      setError('Não foi possível criar a chave.');
    } finally {
      setCreating(false);
    }
  };

  const revokeKey = async (id: string) => {
    try {
      const res = await apiFetch(`/api/v1/integrations/api-keys?id=${id}${tenantId ? `&tenant_id=${tenantId}` : ''}`, { method: 'DELETE' });
      if (!res.ok) throw new Error(`revoke failed: ${res.status}`);
      if (freshKey?.id === id) setFreshKey(null);
      await fetchKeys();
    } catch (err) {
      console.error('Failed to revoke API key:', err);
    }
  };

  const sampleKey = freshKey?.api_key || 'SUA_API_KEY';
  // The ready URL each monitoring tool should POST to (credential inline as ?token=, which works for
  // tools that only let you paste a URL). For the standalone tab, show the generic curl instead.
  const focusedUrl = focused ? `${API_BASE_URL}${focused.path}?token=${sampleKey}` : '';
  const curlExample = `curl -X POST "${API_BASE_URL}/api/v1/ingest" \\
  -H "X-API-Key: ${sampleKey}" \\
  -H "Content-Type: application/json" \\
  -d '{"event_type":"cpu_high","summary":"CPU 98% em web-01","severity":"critical","host":"web-01"}'`;

  return (
    <div className="flex flex-col gap-5">
      <div>
        <h3 className="text-sm font-bold text-slate-200 flex items-center gap-2">
          <Plug className="w-4 h-4 text-cyan-400" /> {focused ? `Como conectar o ${focused.label}` : 'Como conectar suas fontes'}
        </h3>
        <p className="text-[11px] text-slate-500 mt-0.5 max-w-2xl">
          {focused
            ? 'Gere uma chave abaixo e aponte o webhook desta ferramenta para a URL indicada. A chave pertence ao cliente selecionado no topo.'
            : 'Envie eventos para a plataforma em 3 passos. As chaves abaixo pertencem ao cliente selecionado no topo.'}
        </p>
      </div>

      {/* STEP 1 */}
      <div className="rounded-xl bg-black/30 border border-white/5 p-4">
        <div className="flex items-center gap-2 mb-1.5">
          <span className="w-5 h-5 rounded-md bg-cyan-600/20 text-cyan-300 text-[11px] font-bold grid place-items-center">1</span>
          <span className="text-xs font-bold text-slate-200">Ative a integração</span>
        </div>
        <p className="text-[11px] text-slate-400 ml-7">
          {focused
            ? 'Ative este conector para o cliente na lista abaixo (um endpoint só aceita eventos com a integração ativa).'
            : <>Em <span className="text-slate-300 font-semibold">Integrações</span>, ative o conector do tipo de fonte que você vai usar (Zabbix, Prometheus, …). Um endpoint só aceita eventos com a integração ativa.</>}
        </p>
      </div>

      {/* STEP 2 — API keys */}
      <div className="rounded-xl bg-black/30 border border-white/5 p-4">
        <div className="flex items-center justify-between gap-2 mb-2">
          <div className="flex items-center gap-2">
            <span className="w-5 h-5 rounded-md bg-cyan-600/20 text-cyan-300 text-[11px] font-bold grid place-items-center">2</span>
            <span className="text-xs font-bold text-slate-200 flex items-center gap-1.5"><KeyRound className="w-3.5 h-3.5 text-amber-400" /> Gere uma chave de ingestão</span>
          </div>
          <button onClick={fetchKeys} disabled={loading} className="text-slate-400 hover:text-slate-200 transition-all cursor-pointer disabled:opacity-50" title="Atualizar">
            <RefreshCw className={`w-3.5 h-3.5 ${loading ? 'animate-spin' : ''}`} />
          </button>
        </div>
        <p className="text-[11px] text-slate-400 ml-7 mb-3">
          A chave vai no header <code className="text-cyan-300 bg-white/5 border border-white/10 rounded px-1 py-0.5 text-[10px]">X-API-Key</code>. Ela é exibida <strong className="text-slate-300">uma única vez</strong> na criação — copie e guarde.
        </p>

        <div className="ml-7 flex items-center gap-2 mb-3">
          <input
            value={newName}
            onChange={(e) => setNewName(e.target.value)}
            placeholder="Nome da chave (ex.: zabbix-produção)"
            className="flex-1 rounded-lg bg-black/40 border border-white/10 px-3 py-2 text-[11px] text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-cyan-500/40"
          />
          <button
            onClick={createKey}
            disabled={creating}
            className="px-3 py-2 rounded-lg bg-cyan-600/20 hover:bg-cyan-600/30 disabled:opacity-50 text-cyan-300 text-[10px] font-bold uppercase tracking-wider border border-cyan-500/25 transition-all cursor-pointer flex items-center gap-1.5 shrink-0"
          >
            {creating ? <RefreshCw className="w-3.5 h-3.5 animate-spin" /> : <Plus className="w-3.5 h-3.5" />}
            Gerar chave
          </button>
        </div>

        {/* Freshly created key — shown once */}
        {freshKey && (
          <div className="ml-7 mb-3 rounded-lg bg-amber-950/20 border border-amber-500/25 p-3">
            <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase tracking-wider text-amber-300 mb-2">
              <ShieldAlert className="w-3.5 h-3.5" /> Copie agora — não será exibida de novo
            </div>
            <div className="flex items-center gap-2">
              <code className="flex-1 font-mono text-[11px] text-amber-100 bg-black/40 border border-amber-500/20 rounded px-2 py-1.5 break-all">{freshKey.api_key}</code>
              <CopyButton text={freshKey.api_key} label="copiar chave" />
            </div>
          </div>
        )}

        {/* Existing keys */}
        <div className="ml-7 flex flex-col gap-1.5">
          {loading && keys.length === 0 ? (
            <div className="text-[11px] text-slate-500">Carregando…</div>
          ) : keys.length === 0 ? (
            <div className="text-[11px] text-slate-600">Nenhuma chave ainda. Gere a primeira acima.</div>
          ) : (
            keys.map((k) => (
              <div key={k.id} className="flex items-center justify-between gap-2 text-[11px] bg-black/20 border border-white/5 rounded-lg px-3 py-2">
                <div className="min-w-0">
                  <span className="text-slate-300 font-semibold truncate">{k.name}</span>
                  <span className="text-slate-600 ml-2">criada {new Date(k.created_at).toLocaleDateString()}</span>
                </div>
                <button onClick={() => revokeKey(k.id)} className="text-slate-500 hover:text-rose-400 transition-all cursor-pointer shrink-0 flex items-center gap-1 text-[10px] font-bold uppercase tracking-wider" title="Revogar">
                  <Trash2 className="w-3.5 h-3.5" /> Revogar
                </button>
              </div>
            ))
          )}
        </div>
        {error && <div className="ml-7 mt-2 text-[11px] text-rose-400">{error}</div>}
      </div>

      {/* STEP 3 — endpoint(s) + example */}
      <div className="rounded-xl bg-black/30 border border-white/5 p-4">
        <div className="flex items-center gap-2 mb-2">
          <span className="w-5 h-5 rounded-md bg-cyan-600/20 text-cyan-300 text-[11px] font-bold grid place-items-center">3</span>
          <span className="text-xs font-bold text-slate-200 flex items-center gap-1.5"><Zap className="w-3.5 h-3.5 text-violet-400" /> Aponte a fonte para o endpoint</span>
        </div>

        {focused ? (
          <div className="ml-7">
            <p className="text-[11px] text-slate-400 mb-2">
              Cole esta URL no webhook do <strong className="text-slate-300">{focused.label}</strong> (a chave já vai embutida como <code className="text-cyan-300 bg-white/5 border border-white/10 rounded px-1 py-0.5 text-[10px]">?token=</code>):
            </p>
            <div className="flex items-center gap-2 mb-3">
              <code className="flex-1 font-mono text-[11px] text-cyan-300 bg-black/50 border border-white/10 rounded-lg px-3 py-2 break-all">{focusedUrl}</code>
              <CopyButton text={focusedUrl} label="copiar url" />
            </div>
            {focused.hint && (
              <div className="rounded-lg bg-cyan-950/15 border border-cyan-500/15 p-3 text-[11px] text-slate-300 leading-relaxed">
                <span className="text-[10px] font-bold uppercase tracking-wider text-cyan-400/80">Onde configurar</span>
                <p className="mt-1 mb-0 text-slate-400">{focused.hint}</p>
              </div>
            )}
            <p className="text-[10px] text-slate-600 mt-2">
              Se a ferramenta permitir cabeçalhos HTTP, você pode omitir o <code className="text-slate-400">?token=</code> e enviar <code className="text-slate-400">X-API-Key: {sampleKey === 'SUA_API_KEY' ? 'SUA_CHAVE' : sampleKey}</code>.
            </p>
          </div>
        ) : (
          <>
            <p className="text-[11px] text-slate-400 ml-7 mb-3">
              Cada fonte envia o <strong className="text-slate-300">formato nativo do próprio vendor</strong> para o endpoint correspondente.
            </p>
            <div className="ml-7 mb-4">
              <div className="flex items-center justify-between mb-1.5">
                <span className="text-[10px] font-bold uppercase tracking-wider text-slate-500">Exemplo pronto (genérico)</span>
                <CopyButton text={curlExample} label="copiar comando" />
              </div>
              <pre className="text-[11px] font-mono text-slate-300 bg-black/50 border border-white/10 rounded-lg p-3 overflow-x-auto whitespace-pre">{curlExample}</pre>
              <p className="text-[10px] text-slate-600 mt-1.5">
                Em ferramentas que só aceitam URL, use <code className="text-slate-400">?token=SUA_CHAVE</code> no lugar do cabeçalho.
              </p>
            </div>
            <div className="ml-7 overflow-x-auto">
              <table className="w-full text-[11px]">
                <thead>
                  <tr className="text-slate-500 border-b border-white/5">
                    <th className="text-left font-semibold py-1.5 pr-3">Conector</th>
                    <th className="text-left font-semibold py-1.5">Endpoint</th>
                  </tr>
                </thead>
                <tbody>
                  {CONNECTORS.map((c) => {
                    const url = `${API_BASE_URL}${c.path}`;
                    return (
                      <tr key={c.type} className="border-b border-white/[0.03]">
                        <td className="py-1.5 pr-3 text-slate-400 whitespace-nowrap">{c.label}</td>
                        <td className="py-1.5">
                          <div className="flex items-center gap-2">
                            <code className="font-mono text-[10px] text-cyan-300/90 break-all">{c.path}</code>
                            <CopyButton text={url} />
                          </div>
                        </td>
                      </tr>
                    );
                  })}
                </tbody>
              </table>
            </div>
          </>
        )}
      </div>
    </div>
  );
}
