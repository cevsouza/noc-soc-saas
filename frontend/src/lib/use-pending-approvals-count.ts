'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiFetch } from './api-client';

interface UsePendingApprovalsCountResult {
  count: number;
  refetch: () => void;
}

// Polls the runbook-approval queue for its pending count so the "Configuração MSP" tab can
// show a badge without the operator having to open Settings first — fatal-severity alerts
// generate these approval requests today with zero visibility otherwise.
export function usePendingApprovalsCount(token: string | null): UsePendingApprovalsCountResult {
  const [count, setCount] = useState(0);

  const refetch = useCallback(async () => {
    if (!token) return;
    try {
      const res = await apiFetch('/api/v1/runbooks/approvals?status=pending');
      if (res.ok) {
        const data = (await res.json()) as unknown[];
        setCount(Array.isArray(data) ? data.length : 0);
      }
    } catch {
      // Silent — the badge just stays stale, not worth surfacing an error toast for.
    }
  }, [token]);

  useEffect(() => {
    refetch();
    const interval = setInterval(refetch, 30_000);
    return () => clearInterval(interval);
  }, [refetch]);

  return { count, refetch };
}
