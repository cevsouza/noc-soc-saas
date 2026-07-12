'use client';

import { useEffect, useState } from 'react';
import { Brain, CheckCircle2, Cpu, LayoutDashboard, RefreshCw, Zap } from 'lucide-react';
import { Sheet, SheetContent, SheetHeader, SheetTitle } from '@/components/ui/sheet';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { apiFetch, apiFetchJson } from '@/lib/api-client';
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
                <div className="flex flex-col items-center justify-center gap-2 py-16 text-slate-500">
                  <RefreshCw className="w-6 h-6 text-slate-600" />
                  <p className="text-xs">{tab.label} — em breve nesta nova interface.</p>
                </div>
              </TabsContent>
            ))}
          </div>
        </Tabs>
      </SheetContent>
    </Sheet>
  );
}
