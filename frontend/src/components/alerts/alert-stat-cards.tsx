import { AlertOctagon, AlertTriangle, Info, Layers } from 'lucide-react';
import type { AlertStats } from '@/types';

type SeverityFilterValue = 'all' | 'fatal' | 'critical' | 'warning' | 'info';

interface AlertStatCardsProps {
  stats: AlertStats;
  severityFilter: SeverityFilterValue;
  onSelectFilter: (filter: SeverityFilterValue) => void;
}

// Ported from page.tsx:2038-2099 (the 5 KPI tiles only — the adjacent AIOps/simulator/metrics
// widgets stay in the legacy tree for a later pass, per the Fase 2 scope). Clicking a card
// still updates the severity filter; the drill-down summary modal it used to also open is out
// of scope for this pass.
export function AlertStatCards({ stats, severityFilter, onSelectFilter }: AlertStatCardsProps) {
  return (
    <div className="grid grid-cols-5 gap-4">
      <div
        className="glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer hover:border-violet-500/35 transition-all"
        onClick={() => onSelectFilter('all')}
      >
        <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
          <Layers className="w-3.5 h-3.5 text-violet-400" /> Active Alerts
        </span>
        <span className="text-3xl font-extrabold tracking-tight text-white">{stats.total}</span>
        <div className="h-1 bg-violet-600/30 rounded mt-2 overflow-hidden">
          <div className="h-full bg-violet-500 rounded" style={{ width: '100%' }} />
        </div>
      </div>

      <div
        className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-rose-500/35 ${
          severityFilter === 'fatal' ? 'glass-card-active border-severity-fatal/50' : ''
        }`}
        onClick={() => onSelectFilter('fatal')}
      >
        <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
          <AlertOctagon className="w-3.5 h-3.5 text-severity-fatal" /> Fatal Issues
        </span>
        <span className={`text-3xl font-extrabold tracking-tight ${stats.fatal > 0 ? 'text-severity-fatal animate-pulse' : 'text-white'}`}>
          {stats.fatal}
        </span>
        <div className="h-1 bg-severity-fatal/20 rounded mt-2 overflow-hidden">
          <div className="h-full bg-severity-fatal rounded" style={{ width: stats.total > 0 ? `${(stats.fatal / stats.total) * 100}%` : '0%' }} />
        </div>
      </div>

      <div
        className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-orange-500/35 ${
          severityFilter === 'critical' ? 'glass-card-active border-severity-critical/50' : ''
        }`}
        onClick={() => onSelectFilter('critical')}
      >
        <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
          <AlertOctagon className="w-3.5 h-3.5 text-severity-critical" /> Critical
        </span>
        <span className="text-3xl font-extrabold tracking-tight text-white">{stats.critical}</span>
        <div className="h-1 bg-severity-critical/20 rounded mt-2 overflow-hidden">
          <div className="h-full bg-severity-critical rounded" style={{ width: stats.total > 0 ? `${(stats.critical / stats.total) * 100}%` : '0%' }} />
        </div>
      </div>

      <div
        className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-amber-500/35 ${
          severityFilter === 'warning' ? 'glass-card-active border-severity-warning/50' : ''
        }`}
        onClick={() => onSelectFilter('warning')}
      >
        <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
          <AlertTriangle className="w-3.5 h-3.5 text-severity-warning" /> Warnings
        </span>
        <span className="text-3xl font-extrabold tracking-tight text-white">{stats.warning}</span>
        <div className="h-1 bg-severity-warning/20 rounded mt-2 overflow-hidden">
          <div className="h-full bg-severity-warning rounded" style={{ width: stats.total > 0 ? `${(stats.warning / stats.total) * 100}%` : '0%' }} />
        </div>
      </div>

      <div
        className={`glass-card p-4 rounded-xl flex flex-col gap-1 cursor-pointer transition-all hover:border-blue-500/35 ${
          severityFilter === 'info' ? 'glass-card-active border-severity-info/50' : ''
        }`}
        onClick={() => onSelectFilter('info')}
      >
        <span className="text-[10px] text-slate-400 uppercase tracking-widest font-semibold flex items-center gap-1.5">
          <Info className="w-3.5 h-3.5 text-severity-info" /> Informational
        </span>
        <span className="text-3xl font-extrabold tracking-tight text-white">{stats.info}</span>
        <div className="h-1 bg-severity-info/20 rounded mt-2 overflow-hidden">
          <div className="h-full bg-severity-info rounded" style={{ width: stats.total > 0 ? `${(stats.info / stats.total) * 100}%` : '0%' }} />
        </div>
      </div>
    </div>
  );
}
