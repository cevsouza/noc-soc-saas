'use client';

import { useEffect, useRef, useState, useCallback } from 'react';
import { getWSUrl } from './env';
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

  useEffect(() => {
    setAlerts([]);
    connect();
    return () => {
      if (reconnectTimeoutRef.current) clearTimeout(reconnectTimeoutRef.current);
      wsRef.current?.close();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token, tenantIds.join(',')]);

  return { alerts, setAlerts, connStatus };
}
