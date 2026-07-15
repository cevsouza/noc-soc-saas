'use client';

import { useCallback, useEffect, useState } from 'react';
import { ShieldCheck, RefreshCw, Lock, FileCheck2, Database, Clock, Filter } from 'lucide-react';
import { apiFetchJson } from '@/lib/api-client';
import type { ComplianceReport } from '@/types';

// Compliance & governance panel (B4). A read-only posture summary an MSSP can show a client:
// data-retention policy, data inventory, audit-log integrity, and the platform's isolation/encryption
// guarantees. Self-contained, mirrors the other settings panels; accepts an optional tenantId.
export function CompliancePanel({ tenantId }: { tenantId?: string }) {
  const [report, setReport] = useState<ComplianceReport | null>(null);
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const fetchReport = useCallback(async () => {
    setIsLoading(true);
    setError(null);
    try {
      const qs = tenantId ? `?tenant_id=${tenantId}` : '';
      setReport(await apiFetchJson<ComplianceReport>(`/api/v1/reports/compliance${qs}`));
    } catch (err) {
      console.error('Failed to fetch compliance report:', err);
      setError('Não foi possível carregar o relatório de compliance.');
    } finally {
      setIsLoading(false);
    }
  }, [tenantId]);

  useEffect(() => {
    fetchReport();
  }, [fetchReport]);

  const fmtDate = (iso: string | null) => (iso ? new Date(iso).toLocaleString() : '—');

  return (
    <div className="flex flex-col gap-4">
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-1.5 text-emerald-400 font-extrabold uppercase text-[10px]">
          <ShieldCheck className="w-3.5 h-3.5" /> Compliance & Governança
        </div>
        <button
          onClick={fetchReport}
          disabled={isLoading}
          className="flex items-center gap-1.5 px-2.5 py-1.5 rounded-lg text-[11px] font-bold text-slate-300 bg-white/5 hover:bg-white/10 transition-all disabled:opacity-50"
        >
          <RefreshCw className={`w-3 h-3 ${isLoading ? 'animate-spin' : ''}`} /> Atualizar
        </button>
      </div>

      {error && <div className="p-3 rounded-lg bg-rose-950/20 border border-rose-500/25 text-xs text-rose-300">{error}</div>}
      {!report && isLoading && <div className="text-xs text-slate-400 py-8 text-center">Carregando…</div>}

      {report && (
        <>
          {/* Security guarantees */}
          <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
            <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase text-slate-400 mb-3">
              <Lock className="w-3.5 h-3.5 text-emerald-400" /> Garantias de Segurança
            </div>
            <div className="flex flex-col gap-2">
              <GuaranteeRow ok={report.tenant_isolation_rls} label="Isolamento por tenant (Row-Level Security forçado)" />
              <GuaranteeRow ok={report.per_tenant_encryption} label="Cofre criptografado com chave derivada por tenant (HKDF)" />
              <GuaranteeRow ok={report.audit_append_only} label="Trilha de auditoria imutável (append-only, sem edição/exclusão pela aplicação)" />
            </div>
          </div>

          {/* Retention */}
          <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
            <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase text-slate-400 mb-3">
              <Clock className="w-3.5 h-3.5 text-cyan-400" /> Política de Retenção
            </div>
            {report.alerts_retention_enabled ? (
              <p className="text-xs text-slate-300">
                Alertas e incidentes resolvidos são retidos por <strong className="text-slate-100">{report.alerts_retention_days} dias</strong> e purgados depois disso.
              </p>
            ) : (
              <p className="text-xs text-slate-400">
                Sem política de retenção configurada — dados são retidos <strong className="text-slate-200">indefinidamente</strong>. Configure em Métricas & SLA / retenção.
              </p>
            )}
            <p className="text-[11px] text-slate-500 mt-1.5">A trilha de auditoria é retida indefinidamente por padrão (registro de conformidade).</p>
          </div>

          {/* Data inventory */}
          <div className="grid grid-cols-1 lg:grid-cols-2 gap-3">
            <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
              <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase text-slate-400 mb-3">
                <Database className="w-3.5 h-3.5 text-violet-400" /> Inventário de Dados
              </div>
              <div className="flex flex-col gap-2 text-xs">
                <StatRow label="Alertas armazenados" value={report.total_alerts.toLocaleString()} />
                <StatRow label="Alerta mais antigo" value={fmtDate(report.oldest_alert)} />
                <StatRow label="Incidentes" value={report.total_incidents.toLocaleString()} />
              </div>
            </div>
            <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
              <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase text-slate-400 mb-3">
                <FileCheck2 className="w-3.5 h-3.5 text-emerald-400" /> Trilha de Auditoria
              </div>
              <div className="flex flex-col gap-2 text-xs">
                <StatRow label="Registros de auditoria" value={report.audit_entries.toLocaleString()} />
                <StatRow label="Registro mais antigo" value={fmtDate(report.oldest_audit)} />
                <StatRow label="Imutável" value={report.audit_append_only ? 'Sim' : 'Não'} good={report.audit_append_only} />
              </div>
            </div>
          </div>

          {/* Governance surface */}
          <div className="p-4 rounded-xl bg-white/[0.02] border border-white/5">
            <div className="flex items-center gap-1.5 text-[10px] font-bold uppercase text-slate-400 mb-3">
              <Filter className="w-3.5 h-3.5 text-amber-400" /> Governança
            </div>
            <div className="flex flex-col gap-2 text-xs">
              <StatRow label="Regras de supressão ativas" value={`${report.suppression_rules}`} />
              <StatRow label="Metas de SLA customizadas" value={report.sla_customized ? 'Sim' : 'Padrão da plataforma'} />
            </div>
          </div>

          <p className="text-[10px] text-slate-600 text-right">Gerado em {fmtDate(report.generated_at)}</p>
        </>
      )}
    </div>
  );
}

function GuaranteeRow({ ok, label }: { ok: boolean; label: string }) {
  return (
    <div className="flex items-center gap-2 text-xs">
      <span className={`w-4 h-4 rounded-full flex items-center justify-center text-[10px] font-bold shrink-0 ${ok ? 'bg-emerald-500/20 text-emerald-300' : 'bg-rose-500/20 text-rose-300'}`}>
        {ok ? '✓' : '✕'}
      </span>
      <span className="text-slate-300">{label}</span>
    </div>
  );
}

function StatRow({ label, value, good }: { label: string; value: string; good?: boolean }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-slate-400">{label}</span>
      <span className={`font-bold ${good ? 'text-emerald-400' : 'text-slate-200'}`}>{value}</span>
    </div>
  );
}
