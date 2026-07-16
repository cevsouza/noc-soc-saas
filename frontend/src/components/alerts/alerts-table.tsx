'use client';

import { Activity, Target, Terminal } from 'lucide-react';
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table';
import { SeverityBadge } from './severity-badge';
import { SourceBadge } from './source-badge';
import { StatusBadge } from './status-badge';
import { SlaCountdown } from './sla-countdown';
import type { Alert, Tenant } from '@/types';

interface AlertsTableProps {
  alerts: Alert[];
  tenants: Tenant[];
  selectedAlertId?: string;
  onSelectAlert: (alert: Alert) => void;
  onFocusTenant: (tenantId: string) => void;
}

// Ported from page.tsx:2349-2479 — reimplemented on top of shadcn's semantic <Table> (was a
// grid-cols-12 <div> layout before) for real row/cell semantics and better screen-reader
// support, one of the Fase 2 accessibility goals.
export function AlertsTable({ alerts, tenants, selectedAlertId, onSelectAlert, onFocusTenant }: AlertsTableProps) {
  if (alerts.length === 0) {
    return (
      <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5">
        <div className="flex flex-col items-center justify-center py-16 gap-3 text-slate-500">
          <Activity className="w-10 h-10 text-slate-600 animate-pulse" />
          <p className="text-sm">No active alerts reporting in this domain context.</p>
          <p className="text-xs text-slate-600 bg-white/5 px-3 py-1 rounded">Webhook listener active on port 8080</p>
        </div>
      </div>
    );
  }

  return (
    <div className="glass-card rounded-xl overflow-hidden flex flex-col border border-white/5">
      <div className="max-h-[500px] overflow-y-auto">
        <Table>
          <TableHeader className="sticky top-0 bg-surface/95 backdrop-blur-sm">
            <TableRow>
              <TableHead>Severity</TableHead>
              <TableHead className="text-center">Source</TableHead>
              <TableHead>Visual Domain</TableHead>
              <TableHead>Event Type</TableHead>
              <TableHead>Summary</TableHead>
              <TableHead className="text-center">Focar</TableHead>
              <TableHead className="text-center">Time / SLA</TableHead>
              <TableHead className="text-right">Status</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {alerts.map((alert) => (
              <TableRow
                key={alert.id}
                onClick={() => onSelectAlert(alert)}
                className={`cursor-pointer ${selectedAlertId === alert.id ? 'bg-violet-950/15 border-l-2 border-violet-500' : ''}`}
              >
                <TableCell>
                  <SeverityBadge severity={alert.severity} />
                </TableCell>
                <TableCell className="text-center">
                  <SourceBadge source={alert.ai_analysis?.source as string | undefined} />
                </TableCell>
                <TableCell className="truncate max-w-[10rem]">
                  <span className="inline-flex items-center gap-1 px-2.5 py-0.5 rounded text-[10px] font-extrabold uppercase tracking-wider bg-violet-500/10 text-violet-400 border border-violet-500/20">
                    {tenants.find((t) => t.id === alert.tenant_id)?.name || 'Default Tenant'}
                  </span>
                </TableCell>
                <TableCell className="font-mono text-xs text-slate-300 font-bold">
                  <span className="flex items-center gap-1.5 truncate">
                    <Terminal className="w-3.5 h-3.5 text-slate-500 shrink-0" />
                    {alert.event_type}
                  </span>
                </TableCell>
                <TableCell className="text-slate-200 font-medium truncate max-w-[14rem]">{alert.summary}</TableCell>
                <TableCell className="text-center">
                  <button
                    type="button"
                    onClick={(e) => {
                      e.stopPropagation();
                      onFocusTenant(alert.tenant_id);
                    }}
                    title="Isolar foco neste cliente"
                    aria-label="Isolar foco neste cliente"
                    className="p-1 rounded bg-violet-600/15 hover:bg-violet-600/40 text-violet-400 border border-violet-500/20 hover:text-white transition-all cursor-pointer inline-flex items-center justify-center"
                  >
                    <Target className="w-3.5 h-3.5" />
                  </button>
                </TableCell>
                <TableCell className="text-center">
                  <div className="flex flex-col items-center gap-1">
                    <span className="text-xs text-slate-400 font-mono">
                      {new Date(alert.created_at).toLocaleDateString('pt-BR', { day: '2-digit', month: '2-digit' })}{' '}
                      {new Date(alert.created_at).toLocaleTimeString('pt-BR')}
                    </span>
                    <SlaCountdown alert={alert} />
                  </div>
                </TableCell>
                <TableCell className="text-right">
                  <StatusBadge status={alert.status} />
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
    </div>
  );
}
