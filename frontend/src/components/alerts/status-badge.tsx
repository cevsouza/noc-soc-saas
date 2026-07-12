import { CheckCircle2 } from 'lucide-react';
import type { AlertStatus } from '@/types';

const STATUS_CLASSES: Record<AlertStatus, string> = {
  resolved: 'bg-emerald-500/10 border-emerald-500/20 text-emerald-400',
  acknowledged: 'bg-amber-500/10 border-amber-500/20 text-amber-400',
  suppressed: 'bg-slate-500/10 border-slate-500/20 text-slate-400',
  triggered: 'bg-rose-500/10 border-rose-500/20 text-rose-400',
};

export function StatusBadge({ status }: { status: AlertStatus }) {
  return (
    <span className={`inline-flex items-center gap-1 px-2.5 py-0.5 rounded-full text-[10px] font-bold uppercase tracking-wider border ${STATUS_CLASSES[status]}`}>
      {status === 'resolved' && <CheckCircle2 className="w-2.5 h-2.5" />}
      {status}
    </span>
  );
}
