'use client';

import { useCallback, useEffect, useState } from 'react';
import { apiFetch } from './api-client';

interface UsePendingResponseCountResult {
  count: number;
  refetch: () => void;
}

// Polls the outbound-containment queue for its pending count so the "Configuração MSP" tab can
// surface a badge without the operator having to open Settings first — firewall/EDR containment
// requests sit in `response_action_requests` waiting on human approval otherwise.
export function usePendingResponseCount(token: string | null): UsePendingResponseCountResult {
  const [count, setCount] = useState(0);

  const refetch = useCallback(async () => {
    if (!token) return;
    try {
      const res = await apiFetch('/api/v1/response/requests?status=pending');
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
