'use client';

import { useEffect, useState } from 'react';
import { Brain, CheckCircle2, Clock, Code2, Cpu, FileText, LayoutDashboard, RefreshCw, Send, Sparkles, Zap } from 'lucide-react';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
import { useAuth } from '@/lib/auth-context';
import type { Alert, AlertStatus, Runbook } from '@/types';

interface AlertDetailSheetProps {
  alert: Alert | null;
  onOpenChange: (open: boolean) => void;
  onStatusChange: (alertId: string, newStatus: AlertStatus) => void;
  userRole?: string;
}

// Formats the AI Co-Pilot's markdown-ish diagnostic text. Lifted as-is from page.tsx:1489-1511.
function formatMarkdown(text: string) {
  if (!text) return null;
  return text.split('\n').map((line, idx) => {
    const cleanLine = line.trim();
    if (cleanLine.startsWith('### ')) {
      return <h4 key={idx} className="text-xs font-bold text-slate-200 mt-3 mb-1">{cleanLine.replace('### ', '')}</h4>;
    }
    if (cleanLine.startsWith('## ')) {
      return <h3 key={idx} className="text-xs font-extrabold text-violet-300 mt-4 mb-1.5">{cleanLine.replace('## ', '')}</h3>;
    }
    if (cleanLine.startsWith('# ')) {
      return <h2 key={idx} className="text-sm font-black text-white mt-5 mb-2">{cleanLine.replace('# ', '')}</h2>;
    }
    if (cleanLine.startsWith('- ') || cleanLine.startsWith('* ')) {
      return <li key={idx} className="text-xs text-slate-300 ml-4 list-disc space-y-1">{cleanLine.substring(2)}</li>;
    }
    if (cleanLine.startsWith('1. ') || cleanLine.startsWith('2. ') || cleanLine.startsWith('3. ')) {
      return <li key={idx} className="text-xs text-slate-300 ml-4 list-decimal space-y-1">{cleanLine.substring(cleanLine.indexOf('.') + 1).trim()}</li>;
    }
    if (!cleanLine) return <div key={idx} className="h-1.5" />;
    return <p key={idx} className="text-xs text-slate-300 mb-1 leading-relaxed">{cleanLine}</p>;
  });
}

function friendlyExplanation(eventType: string) {
  if (eventType === 'cpu' || eventType === 'HostHighCpuLoad') {
    return "A CPU é o 'cérebro' do servidor. Este alerta significa que o servidor está sobrecarregado com muitas tarefas simultâneas, o que pode deixar os serviços lentos para os usuários finais.";
  }
  if (eventType === 'memory' || eventType === 'OOMKillerTriggered') {
    return 'A memória RAM guarda dados temporários de aplicativos ativos. A falta de memória pode fazer o servidor travar ou derrubar bancos de dados críticos.';
  }
  if (eventType === 'wazuh_security_event' || eventType === 'sshd' || eventType === 'syslog') {
    return "Um sistema ou invasor tentou acessar a conta 'root' (administrador) do servidor errando a senha repetidamente. Isso é um ataque de Força Bruta por SSH.";
  }
  return 'Um evento de monitoramento reportou um comportamento fora do comum neste dispositivo. Requer atenção do operador de turno.';
}

const TAB_ITEMS = [
  { value: 'general', label: 'General' },
  { value: 'logs', label: 'Loki Logs' },
  { value: 'grafana', label: 'Grafana' },
  { value: 'raw', label: 'Raw' },
  { value: 'timeline', label: 'Timeline' },
  { value: 'chat', label: 'Co-Pilot' },
] as const;

// Ported from page.tsx:3963-4211 as a shadcn Sheet (was an inline sidebar `<aside>` before).
// Only the "General" tab's real content is migrated this pass; the other 5 (Loki Logs, Grafana,
// Raw, Timeline, Co-Pilot) show a placeholder — full migration is deferred to a follow-up.
export function AlertDetailSheet({ alert, onOpenChange, onStatusChange, userRole }: AlertDetailSheetProps) {
  const [runbooks, setRunbooks] = useState<Runbook[]>([]);
  const [runbookLogs, setRunbookLogs] = useState('');
  const [isExecutingRunbook, setIsExecutingRunbook] = useState(false);

  useEffect(() => {
    setRunbookLogs('');
    if (!alert) return;

    apiFetchJson<Runbook[]>(`/api/v1/runbooks?tenant_id=${alert.tenant_id}`)
      .then((data) => setRunbooks(data || []))
      .catch((err) => console.error('Failed to fetch runbooks:', err));
  }, [alert]);

  if (!alert) return null;

  const handleExecuteRunbook = async (runbookId: string) => {
    setIsExecutingRunbook(true);
    setRunbookLogs('Iniciando conexão remota via túnel seguro SSH...\nExecutando playbook de auto-cura...\n');

    try {
      const res = await apiFetch(`/api/v1/runbooks/execute?tenant_id=${alert.tenant_id}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ runbook_id: runbookId, incident_id: alert.id }),
      });
      const data = await res.json();
      if (res.ok) {
        setRunbookLogs((prev) => prev + `[Sucesso] Executado com sucesso.\n\nRetorno SSH:\n${data.output}`);
      } else {
        setRunbookLogs((prev) => prev + `[Falha] Erro na execução:\n${data.message || data.output}`);
      }
    } catch (err) {
      setRunbookLogs((prev) => prev + '[Erro de Rede] Não foi possível conectar ao backend.');
    } finally {
      setIsExecutingRunbook(false);
    }
  };

  const handleUpdateStatus = async (newStatus: AlertStatus) => {
    onStatusChange(alert.id, newStatus);

    const endpoint = newStatus === 'acknowledged' ? '/api/v1/incidents/acknowledge' : '/api/v1/incidents/resolve';
    try {
      const res = await apiFetch(`${endpoint}?tenant_id=${alert.tenant_id}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ id: alert.id, created_at: alert.created_at }),
      });
      if (!res.ok) {
        console.error('Failed to update status on server:', await res.text());
      }
    } catch (err) {
      console.error('Network error updating incident status:', err);
    }
  };

  const severityBadgeClass =
    alert.severity === 'fatal'
      ? 'bg-severity-fatal/15 text-severity-fatal'
      : alert.severity === 'critical'
        ? 'bg-severity-critical/15 text-severity-critical'
        : alert.severity === 'warning'
          ? 'bg-severity-warning/15 text-severity-warning'
          : 'bg-severity-info/15 text-severity-info';

  return (
    <Sheet open onOpenChange={(open) => !open && onOpenChange(false)}>
      <SheetContent className="w-[450px] sm:max-w-[450px] p-0 flex flex-col gap-0">
        <SheetHeader className="px-6 py-5 border-b border-white/5 shrink-0">
          <SheetTitle className="flex items-center gap-2 text-sm uppercase tracking-wider">
            <Cpu className="w-4 h-4 text-violet-400" /> Alert Details
          </SheetTitle>
        </SheetHeader>

        <Tabs defaultValue="general" className="flex flex-col flex-1 overflow-hidden">
          <TabsList className="w-full justify-start rounded-none border-b border-white/5 bg-surface/20 h-auto p-0 shrink-0">
            {TAB_ITEMS.map((tab) => (
              <TabsTrigger
                key={tab.value}
                value={tab.value}
                className="flex-1 rounded-none py-3 text-xs font-semibold data-[state=active]:bg-transparent data-[state=active]:shadow-none border-b-2 border-transparent data-[state=active]:border-violet-500 data-[state=active]:text-violet-400"
              >
                {tab.label}
              </TabsTrigger>
            ))}
          </TabsList>

          <div className="flex-1 overflow-y-auto p-6">
            <TabsContent value="general" className="mt-0 flex flex-col gap-6">
              <div className="flex flex-col gap-2">
                <div className="flex items-center gap-2">
                  <span className={`text-[10px] font-bold uppercase px-2 py-0.5 rounded ${severityBadgeClass}`}>
                    {alert.severity} Severity
                  </span>
                  <span className="text-xs text-slate-500 font-mono">{alert.id}</span>
                </div>
                <h4 className="text-lg font-bold text-white leading-tight">{alert.summary}</h4>
                <p className="text-xs text-slate-400">Received: {new Date(alert.created_at).toLocaleString()}</p>
                {alert.resolved_at && (
                  <p className="text-xs text-emerald-400">Resolved: {new Date(alert.resolved_at).toLocaleString()}</p>
                )}
              </div>

              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex items-center justify-between">
                <div className="flex items-center gap-2.5">
                  <LayoutDashboard className="w-5 h-5 text-violet-400" />
                  <div>
                    <h5 className="text-xs font-bold text-slate-300">Incident Source</h5>
                    <p className="text-[10px] text-slate-500 uppercase font-semibold">Normalized alert origin</p>
                  </div>
                </div>
                <span className="text-sm font-extrabold text-violet-400 block uppercase">
                  {(alert.ai_analysis?.source as string) || 'generic'}
                </span>
              </div>

              <div className="grid grid-cols-2 gap-3 shrink-0">
                <button
                  disabled={alert.status === 'acknowledged' || alert.status === 'resolved' || alert.status === 'suppressed' || userRole === 'viewer'}
                  onClick={() => handleUpdateStatus('acknowledged')}
                  className="bg-amber-500/10 hover:bg-amber-500/20 disabled:opacity-40 disabled:hover:bg-amber-500/10 border border-amber-500/30 text-amber-300 py-2 rounded-lg text-xs font-bold uppercase tracking-wider flex items-center justify-center gap-2 transition-all"
                >
                  Acknowledge
                </button>
                <button
                  disabled={alert.status === 'resolved' || userRole === 'viewer'}
                  onClick={() => handleUpdateStatus('resolved')}
                  className="bg-emerald-500/10 hover:bg-emerald-500/20 disabled:opacity-40 disabled:hover:bg-emerald-500/10 border border-emerald-500/30 text-emerald-300 py-2 rounded-lg text-xs font-bold uppercase tracking-wider flex items-center justify-center gap-2 transition-all"
                >
                  Resolve Alert
                </button>
              </div>

              <div className="flex flex-col gap-3 p-5 rounded-xl bg-violet-950/20 border border-violet-500/25">
                <div className="flex items-center gap-2">
                  <Brain className="w-4 h-4 text-violet-400 animate-pulse" />
                  <h5 className="text-xs font-extrabold uppercase text-violet-300 tracking-wider">💡 IA Co-Pilot Diagnostics</h5>
                </div>
                {alert.ai_diagnostic ? (
                  <div className="text-slate-300 select-text flex flex-col gap-1.5 max-h-64 overflow-y-auto pr-1">
                    {formatMarkdown(alert.ai_diagnostic)}
                  </div>
                ) : (
                  <div className="flex items-center gap-2 text-xs text-slate-400">
                    <RefreshCw className="w-3.5 h-3.5 animate-spin text-violet-400" />
                    <span>Gerando diagnóstico e sugestões causa raiz via Gemini...</span>
                  </div>
                )}
              </div>

              <div className="flex flex-col gap-3.5 p-5 rounded-xl bg-slate-900/40 border border-white/5">
                <div className="flex items-center gap-2">
                  <Zap className="w-4 h-4 text-amber-400" />
                  <h5 className="text-xs font-extrabold uppercase text-slate-300 tracking-wider">⚡ Playbooks de Auto-Cura</h5>
                </div>
                <p className="text-[11px] text-slate-400 leading-normal">
                  Execute scripts remotos de mitigação no host afetado utilizando credenciais seguras do Vault.
                </p>

                {runbooks.length === 0 ? (
                  <div className="text-xs text-slate-500 italic bg-white/[0.01] p-3 rounded-lg border border-white/5">
                    Nenhum playbook SSH configurado para este cliente. Adicione na aba Admin.
                  </div>
                ) : (
                  <div className="flex flex-col gap-2">
                    {runbooks.map((rb) => (
                      <div key={rb.id} className="flex items-center justify-between p-2 rounded-lg bg-white/[0.02] border border-white/5">
                        <span className="text-xs font-medium text-slate-300">{rb.name}</span>
                        <button
                          disabled={isExecutingRunbook || userRole === 'viewer'}
                          onClick={() => handleExecuteRunbook(rb.id)}
                          className="px-2.5 py-1 rounded bg-amber-500/10 hover:bg-amber-500/20 disabled:opacity-50 text-amber-300 text-[10px] font-bold uppercase tracking-wider border border-amber-500/20 transition-all flex items-center gap-1 cursor-pointer"
                        >
                          <Zap className="w-2.5 h-2.5" />
                          Executar
                        </button>
                      </div>
                    ))}
                  </div>
                )}

                {runbookLogs && (
                  <div className="flex flex-col gap-2 mt-2">
                    <label className="text-[10px] uppercase font-bold text-slate-500">Terminal SSH Output:</label>
                    <pre className="bg-black border border-white/5 rounded-lg p-3 text-[10px] font-mono text-emerald-400 overflow-x-auto max-h-48 whitespace-pre-wrap select-text leading-relaxed">
                      {runbookLogs}
                    </pre>
                  </div>
                )}
              </div>

              <div className="flex flex-col gap-2.5 p-4 rounded-xl bg-violet-950/10 border border-violet-500/10">
                <div className="flex items-center gap-2">
                  <Brain className="w-4 h-4 text-violet-400" />
                  <h5 className="text-xs font-extrabold uppercase text-violet-300 tracking-wider">🔬 O que significa este alerta?</h5>
                </div>
                <p className="text-xs text-slate-300 leading-relaxed font-sans">{friendlyExplanation(alert.event_type)}</p>
              </div>

              <div className="flex flex-col gap-3 p-4 rounded-xl bg-slate-900/40 border border-white/5">
                <h5 className="text-xs font-extrabold uppercase text-slate-300 tracking-wider flex items-center gap-1.5">
                  <CheckCircle2 className="w-3.5 h-3.5 text-emerald-400" /> Guia de Operação (Passo a Passo)
                </h5>
                <div className="flex flex-col gap-2 text-slate-400 font-sans leading-relaxed">
                  <div className="flex items-start gap-2">
                    <span className="w-4 h-4 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-[10px] font-bold text-slate-300 shrink-0 mt-0.5">1</span>
                    <p>Analise a gravidade do alerta e verifique a aba de <b>Loki Logs</b> para ver logs do host no momento do incidente.</p>
                  </div>
                  <div className="flex items-start gap-2">
                    <span className="w-4 h-4 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-[10px] font-bold text-slate-300 shrink-0 mt-0.5">2</span>
                    <p>Cheque a aba <b>Grafana</b> para validar o uso de recursos do host em tempo real.</p>
                  </div>
                  <div className="flex items-start gap-2">
                    <span className="w-4 h-4 rounded-full bg-white/5 border border-white/10 flex items-center justify-center text-[10px] font-bold text-slate-300 shrink-0 mt-0.5">3</span>
                    <p>Se o problema persistir após a auto-cura automática, clique em <b>Acknowledge</b> para assumir o chamado.</p>
                  </div>
                </div>
              </div>

              <div className="p-4 rounded-xl bg-white/5 border border-white/5 flex items-center justify-between">
                <div className="flex items-center gap-2.5">
                  <RefreshCw className="w-5 h-5 text-violet-400" />
                  <div>
                    <h5 className="text-xs font-bold text-slate-300">Redis Debounce Engine</h5>
                    <p className="text-[10px] text-slate-500 uppercase font-semibold">Cascade protection</p>
                  </div>
                </div>
                <div className="text-right">
                  <span className="text-xl font-extrabold text-white block">{(alert.payload?.occurrences as number) || 1}x</span>
                  <span className="text-[9px] text-slate-400 uppercase font-bold tracking-wider">Occurrences</span>
                </div>
              </div>
            </TabsContent>

            {TAB_ITEMS.filter((t) => t.value !== 'general').map((tab) => (
              <TabsContent key={tab.value} value={tab.value} className="mt-0">
                {tab.value === 'logs' ? (
                  <LokiLogsTab alert={alert} />
                ) : tab.value === 'grafana' ? (
                  <GrafanaTab alert={alert} />
                ) : tab.value === 'raw' ? (
                  <RawTab alert={alert} />
                ) : tab.value === 'timeline' ? (
                  <TimelineTab alert={alert} />
                ) : tab.value === 'chat' ? (
                  <CoPilotTab alert={alert} />
                ) : null}
              </TabsContent>
            ))}
          </div>
        </Tabs>
      </SheetContent>
    </Sheet>
  );
}

// LokiLogsTab renders the host log lines the worker captured from Grafana Loki at the moment the
// alert fired (stored on ai_analysis.loki_logs for warning/critical/fatal alerts). Empty when Loki
// isn't configured for the tenant or no matching lines were found for the host.
function LokiLogsTab({ alert }: { alert: Alert }) {
  const snapshot = Array.isArray(alert.ai_analysis?.loki_logs)
    ? (alert.ai_analysis!.loki_logs as unknown[]).filter((l): l is string => typeof l === 'string')
    : [];
  const host = String((alert.ai_analysis?.host as string) || alert.payload?.host || '');
  const [logs, setLogs] = useState<string[]>(snapshot);
  const [live, setLive] = useState(false);
  const [loading, setLoading] = useState(false);

  const reload = async () => {
    if (!host) return;
    setLoading(true);
    try {
      const res = await apiFetchJson<{ logs: string[] }>(
        `/api/v1/loki/logs?host=${encodeURIComponent(host)}&tenant_id=${alert.tenant_id}`,
      );
      setLogs(res.logs || []);
      setLive(true);
    } catch {
      /* keep snapshot on failure */
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5 min-w-0">
          <FileText className="w-3.5 h-3.5 text-orange-400 shrink-0" />
          <span className="truncate">
            Logs de <b className="text-slate-300">{host || '—'}</b> · {live ? 'ao vivo' : 'no momento do incidente'} ({logs.length})
          </span>
        </span>
        <button
          type="button"
          onClick={reload}
          disabled={loading || !host}
          className="px-2 py-1 rounded-md bg-white/5 hover:bg-white/10 disabled:opacity-40 border border-white/10 text-[10px] font-bold uppercase tracking-wider text-slate-300 transition-all cursor-pointer flex items-center gap-1.5 shrink-0"
        >
          <RefreshCw className={`w-3 h-3 ${loading ? 'animate-spin' : ''}`} /> Recarregar
        </button>
      </div>

      {logs.length > 0 ? (
        <pre className="bg-black border border-white/5 rounded-lg p-3 text-[10px] font-mono text-emerald-300/90 overflow-x-auto max-h-[58vh] whitespace-pre-wrap select-text leading-relaxed">
          {logs.join('\n')}
        </pre>
      ) : (
        <div className="flex flex-col items-center justify-center gap-2 py-12 text-center text-slate-500">
          <FileText className="w-6 h-6 text-slate-600" />
          <p className="text-xs">Nenhum log do Loki para <b className="text-slate-300">{host || 'este host'}</b>.</p>
          <p className="text-[11px] text-slate-600 max-w-sm leading-relaxed">
            <b>Como usar:</b> configure o conector em <b>Central de Conectores → Grafana Loki</b> (URL, usuário e senha).
            Os logs são coletados automaticamente quando o alerta dispara (warning/critical/fatal); use <b>Recarregar</b>
            para buscar a janela atual ao vivo.
          </p>
        </div>
      )}
    </div>
  );
}

// RawTab shows the full alert object as pretty-printed JSON — the ground truth for debugging and for
// copying into a ticket.
function RawTab({ alert }: { alert: Alert }) {
  const json = JSON.stringify(alert, null, 2);
  const [copied, setCopied] = useState(false);
  return (
    <div className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <span className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5">
          <Code2 className="w-3.5 h-3.5 text-cyan-400" /> JSON bruto do alerta
        </span>
        <button
          type="button"
          onClick={() => {
            navigator.clipboard.writeText(json).then(() => {
              setCopied(true);
              setTimeout(() => setCopied(false), 1400);
            });
          }}
          className="px-2 py-1 rounded-md bg-white/5 hover:bg-white/10 border border-white/10 text-[10px] font-bold uppercase tracking-wider text-slate-300 transition-all cursor-pointer"
        >
          {copied ? 'copiado ✓' : 'copiar'}
        </button>
      </div>
      <pre className="bg-black border border-white/5 rounded-lg p-3 text-[10px] font-mono text-slate-300 overflow-auto max-h-[62vh] whitespace-pre select-text leading-relaxed">
        {json}
      </pre>
    </div>
  );
}

// TimelineTab reconstructs the alert lifecycle from its timestamps — created → recurrences → ack →
// resolved — plus the current status, so an operator sees the sequence at a glance.
function TimelineTab({ alert }: { alert: Alert }) {
  const fmt = (t?: string) => (t ? new Date(t).toLocaleString() : '—');
  const occ = (alert.payload?.occurrences as number) || 1;

  const events: { label: string; time?: string; tone: 'sky' | 'amber' | 'emerald' | 'slate' }[] = [
    { label: 'Alerta criado', time: alert.created_at, tone: 'sky' },
  ];
  if (occ > 1) events.push({ label: `Recorrência — ${occ} ocorrências (dedupe)`, time: alert.updated_at, tone: 'amber' });
  if (alert.acknowledged_at) events.push({ label: 'Reconhecido (Acknowledge)', time: alert.acknowledged_at, tone: 'amber' });
  if (alert.resolved_at) events.push({ label: 'Resolvido', time: alert.resolved_at, tone: 'emerald' });
  events.push({ label: `Status atual: ${alert.status}`, time: alert.updated_at, tone: alert.status === 'resolved' ? 'emerald' : 'slate' });

  const dot: Record<string, string> = {
    sky: 'bg-sky-400', amber: 'bg-amber-400', emerald: 'bg-emerald-400', slate: 'bg-slate-500',
  };

  return (
    <div className="flex flex-col gap-1 py-2">
      <span className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5 mb-2">
        <Clock className="w-3.5 h-3.5 text-violet-400" /> Linha do tempo do alerta
      </span>
      <div className="relative pl-5">
        <div className="absolute left-[6px] top-1 bottom-1 w-px bg-white/10" />
        {events.map((e, i) => (
          <div key={i} className="relative flex items-start gap-3 pb-4 last:pb-0">
            <span className={`absolute -left-5 top-1 w-3 h-3 rounded-full border-2 border-black ${dot[e.tone]}`} />
            <div className="flex flex-col">
              <span className="text-xs text-slate-200 font-semibold">{e.label}</span>
              <span className="text-[10px] font-mono text-slate-500">{fmt(e.time)}</span>
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}

// GrafanaTab surfaces the numeric metrics carried on the alert payload (CPU, memory, latency, …). A
// live embedded Grafana panel would need a configured dashboard URL — noted here — so meanwhile this
// gives the operator the values captured at incident time.
function GrafanaTab({ alert }: { alert: Alert }) {
  const { user } = useAuth();
  const isAdmin = user?.role === 'admin';
  const host = String((alert.ai_analysis?.host as string) || alert.payload?.host || '');
  const metrics = Object.entries(alert.payload || {}).filter(
    ([k, v]) => typeof v === 'number' && k !== 'occurrences',
  ) as [string, number][];

  const [embedUrl, setEmbedUrl] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [dashInput, setDashInput] = useState('');
  const [saving, setSaving] = useState(false);
  const [iframeFailed, setIframeFailed] = useState(false);

  const loadEmbed = async () => {
    setLoading(true);
    setIframeFailed(false);
    try {
      const created = new Date(alert.created_at).getTime();
      const from = created - 30 * 60 * 1000;
      const to = created + 30 * 60 * 1000;
      const res = await apiFetchJson<{ url: string }>(
        `/api/v1/integrations/grafana-embed?host=${encodeURIComponent(host)}&from=${from}&to=${to}&tenant_id=${alert.tenant_id}`,
      );
      setEmbedUrl(res.url || '');
    } catch {
      setEmbedUrl('');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    loadEmbed();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [alert.id]);

  const saveDashboard = async () => {
    if (!dashInput.trim()) return;
    setSaving(true);
    try {
      await apiFetch(`/api/v1/vault/secret?tenant_id=${alert.tenant_id}`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ key: 'grafana_dashboard_url', value: dashInput.trim() }),
      });
      setDashInput('');
      await loadEmbed();
    } catch {
      /* ignore */
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <div className="flex items-center justify-between gap-2">
        <span className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5">
          <LayoutDashboard className="w-3.5 h-3.5 text-orange-400" /> Painel do host {host && <>· <b className="text-slate-300">{host}</b></>}
        </span>
        {embedUrl && (
          <a
            href={embedUrl}
            target="_blank"
            rel="noreferrer"
            className="px-2 py-1 rounded-md bg-white/5 hover:bg-white/10 border border-white/10 text-[10px] font-bold uppercase tracking-wider text-slate-300 transition-all"
          >
            Abrir no Grafana ↗
          </a>
        )}
      </div>

      {/* Live embedded panel when a dashboard URL is configured */}
      {embedUrl && !iframeFailed ? (
        <iframe
          src={embedUrl}
          title="Grafana"
          className="w-full rounded-lg border border-white/10 bg-black"
          style={{ height: 360 }}
          onError={() => setIframeFailed(true)}
        />
      ) : embedUrl && iframeFailed ? (
        <div className="rounded-lg bg-amber-950/15 border border-amber-500/20 p-3 text-[11px] text-amber-200">
          O Grafana bloqueou a exibição embutida. Use <b>Abrir no Grafana ↗</b> acima, ou habilite o embedding no
          Grafana (<code>allow_embedding = true</code> + acesso anônimo ou painel público).
        </div>
      ) : null}

      {/* Metrics captured at incident time (always useful, and the fallback when there's no panel) */}
      {metrics.length > 0 && (
        <div>
          <span className="text-[10px] uppercase font-bold tracking-wider text-slate-500">Métricas no momento do incidente</span>
          <div className="grid grid-cols-2 md:grid-cols-3 gap-2.5 mt-1.5">
            {metrics.map(([k, v]) => (
              <div key={k} className="rounded-xl bg-black/30 border border-white/5 p-3">
                <div className="text-[10px] font-mono uppercase tracking-wider text-slate-500 truncate">{k}</div>
                <div className="text-lg font-extrabold text-slate-100 mt-0.5">{v}</div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Setup: shown when no dashboard is configured (procedure in-app) */}
      {!loading && !embedUrl && (
        <div className="rounded-xl bg-black/30 border border-white/5 p-3 flex flex-col gap-2">
          <span className="text-[10px] uppercase font-bold tracking-wider text-cyan-400/80">Como habilitar o painel ao vivo</span>
          <ol className="text-[11px] text-slate-400 leading-relaxed list-decimal pl-4 flex flex-col gap-1">
            <li>No Grafana, abra o painel do host → <b>Share → Embed</b> e copie a URL (algo como <code>.../d-solo/UID/slug?panelId=1</code>).</li>
            <li>Troque o host e o tempo por variáveis: use <code>{'{host}'}</code>, <code>{'{from}'}</code> e <code>{'{to}'}</code> (ex.: <code>&amp;var-instance={'{host}'}&amp;from={'{from}'}&amp;to={'{to}'}</code>).</li>
            <li>No Grafana, habilite <code>allow_embedding=true</code> (e acesso anônimo ou painel público) para a exibição embutida funcionar.</li>
            {isAdmin ? <li>Cole a URL abaixo e salve — vale para todos os alertas deste cliente.</li> : <li>Peça a um administrador para colar essa URL na configuração do cliente.</li>}
          </ol>
          {isAdmin && (
            <div className="flex items-center gap-2 mt-1">
              <input
                value={dashInput}
                onChange={(e) => setDashInput(e.target.value)}
                placeholder="https://grafana.suaempresa.com/d-solo/UID/host?panelId=1&var-instance={host}&from={from}&to={to}&theme=dark"
                className="flex-1 rounded-lg bg-black/40 border border-white/10 px-3 py-2 text-[11px] text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-orange-500/40"
              />
              <button
                type="button"
                onClick={saveDashboard}
                disabled={saving || !dashInput.trim()}
                className="px-3 py-2 rounded-lg bg-orange-600/20 hover:bg-orange-600/30 disabled:opacity-40 text-orange-300 text-[10px] font-bold uppercase tracking-wider border border-orange-500/25 transition-all cursor-pointer shrink-0"
              >
                {saving ? '…' : 'Salvar'}
              </button>
            </div>
          )}
        </div>
      )}
      {loading && <div className="text-[11px] text-slate-500 flex items-center gap-1.5 py-2"><RefreshCw className="w-3 h-3 animate-spin" /> carregando painel…</div>}
    </div>
  );
}

// CoPilotTab is an interactive AI assistant over the incident (POST /api/v1/incidents/chat). It opens
// with the AI diagnostic (when the Python worker produced one) and lets the operator ask follow-ups;
// each exchange is also saved to the incident's investigation timeline server-side.
function CoPilotTab({ alert }: { alert: Alert }) {
  const opening = typeof alert.ai_diagnostic === 'string' && alert.ai_diagnostic
    ? alert.ai_diagnostic
    : (alert.ai_analysis?.description as string) || '';
  const [messages, setMessages] = useState<{ role: 'user' | 'ai'; text: string }[]>(
    opening ? [{ role: 'ai', text: opening }] : [],
  );
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);

  const send = async () => {
    const prompt = input.trim();
    if (!prompt || sending) return;
    setMessages((m) => [...m, { role: 'user', text: prompt }]);
    setInput('');
    setSending(true);
    try {
      const res = await apiFetchJson<{ response: string }>(
        `/api/v1/incidents/chat?tenant_id=${alert.tenant_id}`,
        {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ incident_id: alert.id, created_at: alert.created_at, prompt }),
        },
      );
      setMessages((m) => [...m, { role: 'ai', text: res.response }]);
    } catch {
      setMessages((m) => [...m, { role: 'ai', text: 'Não consegui responder agora. Verifique se a IA (Gemini) está configurada no ambiente.' }]);
    } finally {
      setSending(false);
    }
  };

  return (
    <div className="flex flex-col gap-3">
      <span className="text-[10px] uppercase font-bold tracking-wider text-slate-500 flex items-center gap-1.5">
        <Sparkles className="w-3.5 h-3.5 text-violet-400" /> Co-Pilot — assistente de investigação
      </span>

      <div className="flex flex-col gap-2 max-h-[46vh] overflow-y-auto pr-1">
        {messages.length === 0 && (
          <p className="text-[11px] text-slate-600 py-6 text-center">
            Pergunte ao Co-Pilot sobre este incidente — ex.: “qual a causa provável?” ou “que passos de contenção você sugere?”.
          </p>
        )}
        {messages.map((m, i) => (
          <div
            key={i}
            className={`text-xs leading-relaxed rounded-xl px-3 py-2 border whitespace-pre-wrap break-words ${
              m.role === 'user'
                ? 'bg-cyan-600/10 border-cyan-500/20 text-slate-200 self-end max-w-[85%]'
                : 'bg-violet-950/20 border-violet-500/15 text-slate-300 self-start max-w-[92%]'
            }`}
          >
            {m.text}
          </div>
        ))}
        {sending && <div className="text-[11px] text-slate-500 self-start flex items-center gap-1.5"><RefreshCw className="w-3 h-3 animate-spin" /> pensando…</div>}
      </div>

      <div className="flex items-end gap-2 border-t border-white/5 pt-3">
        <textarea
          value={input}
          onChange={(e) => setInput(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === 'Enter' && !e.shiftKey) {
              e.preventDefault();
              send();
            }
          }}
          placeholder="Pergunte ao Co-Pilot… (Enter envia, Shift+Enter quebra linha)"
          rows={2}
          className="flex-1 resize-y rounded-lg bg-black/40 border border-white/10 px-3 py-2 text-[11px] text-slate-200 placeholder:text-slate-600 focus:outline-none focus:border-violet-500/40"
        />
        <button
          type="button"
          onClick={send}
          disabled={sending || !input.trim()}
          className="px-3 py-2 rounded-lg bg-violet-600/20 hover:bg-violet-600/30 disabled:opacity-40 text-violet-300 text-[10px] font-bold uppercase tracking-wider border border-violet-500/25 transition-all cursor-pointer flex items-center gap-1.5 shrink-0"
        >
          <Send className="w-3.5 h-3.5" /> Enviar
        </button>
      </div>
    </div>
  );
}
