'use client';

import { useEffect, useState } from 'react';
import { apiFetchJson } from './api-client';
import type { GlobalSearchResponse } from '@/types';

const EMPTY_RESULTS: GlobalSearchResponse = { alerts: [], runbooks: [], tenants: [] };

// Debounced client for GET /api/v1/search — used by the Cmd+K global search palette.
export function useGlobalSearch(query: string, tenantIds: string[]) {
  const [results, setResults] = useState<GlobalSearchResponse>(EMPTY_RESULTS);
  const [isLoading, setIsLoading] = useState(false);

  useEffect(() => {
    if (query.trim().length < 2) {
      setResults(EMPTY_RESULTS);
      return;
    }

    let cancelled = false;
    setIsLoading(true);
    const timeout = setTimeout(async () => {
      try {
        const params = new URLSearchParams({ q: query, tenants: tenantIds.join(',') });
        const data = await apiFetchJson<GlobalSearchResponse>(`/api/v1/search?${params.toString()}`);
        if (!cancelled) setResults(data);
      } catch (err) {
        if (!cancelled) {
          console.error('Global search failed:', err);
          setResults(EMPTY_RESULTS);
        }
      } finally {
        if (!cancelled) setIsLoading(false);
      }
    }, 300);

    return () => {
      cancelled = true;
      clearTimeout(timeout);
    };
  }, [query, tenantIds.join(',')]);

  return { results, isLoading };
}
