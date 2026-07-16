'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiFetch } from './api-client';
import type { PlaybookRun } from '@/types';

interface UsePendingPlaybookCountResult {
  count: number;
  refetch: () => void;
}

// Polls playbook runs and counts those paused awaiting approval, so the "Configuração MSP" tab badge
// reflects SOAR playbook runs that need a human decision (a response_action step is gated) — the same
// idea as the runbook-approval and containment counts.
export function usePendingPlaybookCount(token: string | null): UsePendingPlaybookCountResult {
  const [count, setCount] = useState(0);

  const refetch = useCallback(async () => {
    if (!token) return;
    try {
      const res = await apiFetch('/api/v1/playbooks/runs');
      if (res.ok) {
        const data = (await res.json()) as PlaybookRun[];
        setCount(Array.isArray(data) ? data.filter((r) => r.status === 'awaiting_approval').length : 0);
      }
    } catch {
      // Silent — the badge just stays stale.
    }
  }, [token]);

  useEffect(() => {
    refetch();
    const interval = setInterval(refetch, 30_000);
    return () => clearInterval(interval);
  }, [refetch]);

  return { count, refetch };
}
