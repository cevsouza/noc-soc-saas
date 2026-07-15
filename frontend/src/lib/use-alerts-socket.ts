'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { getWSUrl } from './env';
import { apiFetchJson } from './api-client';
import { compareByPriority } from './alert-priority';
import type { Alert } from '@/types';

export type ConnectionStatus = 'connecting' | 'connected' | 'disconnected';

interface UseAlertsSocketResult {
  alerts: Alert[];
  setAlerts: React.Dispatch<React.SetStateAction<Alert[]>>;
  connStatus: ConnectionStatus;
}

/**
 * Wraps the live-alerts WebSocket connection (`/api/v1/ws`) — connect/reconnect lifecycle
 * ported as-is from the original page.tsx implementation (getWSUrl + connectWebSocket,
 * lines 988-1066): auto-reconnects 3s after a close, tears down and reopens whenever the
 * token or tenant selection changes, and merges incoming alert messages into local state
 * (update-in-place by id, otherwise prepend).
 */
export function useAlertsSocket(token: string | null, tenantIds: string[]): UseAlertsSocketResult {
  const [alerts, setAlerts] = useState<Alert[]>([]);
  const [connStatus, setConnStatus] = useState<ConnectionStatus>('disconnected');
  const wsRef = useRef<WebSocket | null>(null);
  const reconnectTimeoutRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const connect = useCallback(() => {
    if (!token || tenantIds.length === 0) return;

    if (wsRef.current) {
      wsRef.current.close();
    }

    setConnStatus('connecting');
    const ws = new WebSocket(getWSUrl(token, tenantIds));
    wsRef.current = ws;

    ws.onopen = () => {
      setConnStatus('connected');
    };

    ws.onmessage = (event) => {
      try {
        const incoming: Alert = JSON.parse(event.data);
        setAlerts((prev) => {
          const existingIndex = prev.findIndex((a) => a.id === incoming.id);
          if (existingIndex >= 0) {
            const next = [...prev];
            next[existingIndex] = incoming;
            return next;
          }
          return [incoming, ...prev];
        });
      } catch (err) {
        console.error('[use-alerts-socket] Failed to parse incoming message:', err);
      }
    };

    ws.onclose = () => {
      setConnStatus('disconnected');
      reconnectTimeoutRef.current = setTimeout(connect, 3000);
    };

    ws.onerror = () => {
      ws.close();
    };
  }, [token, tenantIds]);

  // Reset the accumulated feed only when the USER (token) changes — never on a mere domain-scope
  // change. The consumer already filters `alerts` by the selected tenants client-side, so keeping
  // the list means narrowing the domain shows those alerts instantly instead of blanking out and
  // waiting for the next live message (the WS sends no historical backlog on connect).
  useEffect(() => {
    setAlerts([]);
  }, [token]);

  useEffect(() => {
    connect();
    return () => {
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      wsRef.current?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, tenantIds.join(',')]);

  // Seed the list from the REST backlog (GET /api/v1/alerts?status=open) so the cockpit opens already
  // populated with the OPEN working set for the selected domain — the WS only streams new events,
  // never a snapshot on connect. `status=open` is what guarantees an old-but-still-open alert is
  // seeded even when it falls outside the recency window (the operational-console principle). Runs
  // per selected tenant, merges without clobbering fresher live state (existing ids win), and orders
  // by urgency (severity then SLA burn) so the console leads with what needs action.
  useEffect(() => {
    if (!token || tenantIds.length === 0) return;
    let cancelled = false;
    (async () => {
      try {
        const perTenant = await Promise.all(
          tenantIds.map((id) =>
            apiFetchJson<Alert[]>(`/api/v1/alerts?tenant_id=${id}&status=open`, { token }).catch(() => [] as Alert[]),
          ),
        );
        if (cancelled) return;
        const seed = perTenant.flat();
        if (seed.length === 0) return;
        setAlerts((prev) => {
          const byId = new Map(prev.map((a) => [a.id, a]));
          for (const a of seed) if (!byId.has(a.id)) byId.set(a.id, a);
          return Array.from(byId.values()).sort(compareByPriority);
        });
      } catch (err) {
        console.error('[use-alerts-socket] backlog seed failed:', err);
      }
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, tenantIds.join(',')]);

  return { alerts, setAlerts, connStatus };
}
