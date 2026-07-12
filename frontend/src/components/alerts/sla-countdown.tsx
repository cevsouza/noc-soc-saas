'use client';

import { useEffect, useState } from 'react';
import { Clock } from 'lucide-react';
import type { Alert } from '@/types';

// Lifted as-is from page.tsx:59-117.
export function SlaCountdown({ alert }: { alert: Alert }) {
  const [timeLeft, setTimeLeft] = useState<string>('');
  const [isOverSla, setIsOverSla] = useState(false);

  useEffect(() => {
    if (alert.status === 'resolved' || alert.status === 'suppressed') {
      setTimeLeft('SLA OK');
      setIsOverSla(false);
      return;
    }

    const calculateTime = () => {
      const created = new Date(alert.created_at).getTime();
      let limitMs = 480 * 60 * 1000; // default info: 8 hours
      if (alert.severity === 'fatal') limitMs = 15 * 60 * 1000;
      else if (alert.severity === 'critical') limitMs = 30 * 60 * 1000;
      else if (alert.severity === 'warning') limitMs = 120 * 60 * 1000;

      const now = new Date().getTime();
      const diff = created + limitMs - now;

      if (diff <= 0) {
        setTimeLeft('SLA ESTOURADO');
        setIsOverSla(true);
      } else {
        const mins = Math.floor((diff % (1000 * 60 * 60)) / (1000 * 60));
        const secs = Math.floor((diff % (1000 * 60)) / 1000);

        let hrsText = '';
        const hrs = Math.floor(diff / (1000 * 60 * 60));
        if (hrs > 0) {
          hrsText = `${hrs}h `;
        }

        setTimeLeft(`${hrsText}${mins}m ${secs}s`);
        setIsOverSla(false);
      }
    };

    calculateTime();
    const interval = setInterval(calculateTime, 1000);
    return () => clearInterval(interval);
  }, [alert.created_at, alert.severity, alert.status]);

  if (alert.status === 'resolved' || alert.status === 'suppressed') {
    return (
      <span className="text-[10px] text-emerald-400 font-extrabold bg-emerald-500/10 px-2 py-0.5 rounded border border-emerald-500/20">
        RESOLVIDO
      </span>
    );
  }

  return (
    <span
      className={`text-[10px] font-mono font-bold px-2 py-0.5 rounded border flex items-center gap-1 shrink-0 ${
        isOverSla ? 'text-rose-400 bg-rose-500/10 border-rose-500/30 animate-pulse' : 'text-amber-400 bg-amber-500/10 border-amber-500/30'
      }`}
    >
      <Clock className="w-3 h-3" />
      {timeLeft}
    </span>
  );
}
