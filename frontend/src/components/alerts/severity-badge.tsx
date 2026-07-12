import type { AlertSeverity } from '@/types';

const SEVERITY_CLASSES: Record<AlertSeverity, string> = {
  fatal: 'bg-severity-fatal/10 text-severity-fatal border border-severity-fatal/35 neon-pulse-fatal',
  critical: 'bg-severity-critical/10 text-severity-critical border border-severity-critical/30 neon-pulse-critical',
  warning: 'bg-severity-warning/10 text-severity-warning border border-severity-warning/25',
  info: 'bg-severity-info/10 text-severity-info border border-severity-info/20',
};

export function SeverityBadge({ severity }: { severity: AlertSeverity }) {
  return (
    <span className={`inline-flex items-center gap-1.5 px-2 py-0.5 rounded text-[10px] font-bold uppercase tracking-wider ${SEVERITY_CLASSES[severity]}`}>
      {severity}
    </span>
  );
}
