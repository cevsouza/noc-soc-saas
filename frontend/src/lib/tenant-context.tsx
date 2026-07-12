'use client';

import React, { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { useAuth } from './auth-context';
import { apiFetchJson } from './api-client';
import type { Tenant } from '@/types';

interface TenantContextValue {
  tenants: Tenant[];
  selectedTenantIds: string[];
  /** The first of selectedTenantIds, resolved to a full Tenant — used by single-tenant admin
   * forms (e.g. which tenant an SSH vault secret gets saved under). */
  selectedTenant: Tenant | null;
  setSelectedTenantIds: (ids: string[]) => void;
  toggleTenant: (id: string) => void;
  selectAllTenants: () => void;
  /** Re-fetches the tenant list from the API — used after admin create/delete tenant mutations. */
  refetchTenants: () => Promise<void>;
  /** Raw setter — used by the white-label editor for optimistic local edits (logo/color) ahead
   * of an explicit save, matching the original page.tsx behavior of live-previewing edits. */
  setTenants: React.Dispatch<React.SetStateAction<Tenant[]>>;
  isLoading: boolean;
}

const TenantContext = createContext<TenantContextValue | undefined>(undefined);

export function TenantProvider({ children }: { children: React.ReactNode }) {
  const { token } = useAuth();
  const [tenants, setTenants] = useState<Tenant[]>([]);
  const [selectedTenantIds, setSelectedTenantIdsState] = useState<string[]>([]);
  const [isLoading, setIsLoading] = useState(true);

  const refetchTenants = useCallback(async () => {
    if (!token) return;
    try {
      const data = await apiFetchJson<Tenant[]>('/api/v1/tenants', { token });
      if (!Array.isArray(data) || data.length === 0) return;
      setTenants(data);
      setSelectedTenantIdsState((prev) => {
        if (prev.length === 0) return data.map((t) => t.id);
        const validIds = prev.filter((id) => data.some((t) => t.id === id));
        return validIds.length > 0 ? validIds : [data[0].id];
      });
    } catch (err) {
      console.error('[tenant-context] Failed to fetch tenants:', err);
    }
  }, [token]);

  useEffect(() => {
    if (!token) {
      setIsLoading(false);
      return;
    }
    let cancelled = false;
    (async () => {
      await refetchTenants();
      if (!cancelled) setIsLoading(false);
    })();
    return () => {
      cancelled = true;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [token]);

  const setSelectedTenantIds = useCallback((ids: string[]) => {
    setSelectedTenantIdsState(ids);
  }, []);

  const toggleTenant = useCallback((id: string) => {
    setSelectedTenantIdsState((prev) => {
      const isChecked = prev.includes(id);
      if (isChecked) {
        // Guard: never allow deselecting the last remaining tenant (parity with the original
        // page.tsx behavior at line ~1904-1907).
        return prev.length > 1 ? prev.filter((x) => x !== id) : prev;
      }
      return [...prev, id];
    });
  }, []);

  const selectAllTenants = useCallback(() => {
    setSelectedTenantIdsState(tenants.map((t) => t.id));
  }, [tenants]);

  const selectedTenant = tenants.find((t) => t.id === selectedTenantIds[0]) ?? null;

  return (
    <TenantContext.Provider
      value={{ tenants, selectedTenantIds, selectedTenant, setSelectedTenantIds, toggleTenant, selectAllTenants, refetchTenants, setTenants, isLoading }}
    >
      {children}
    </TenantContext.Provider>
  );
}

export function useTenantSelection(): TenantContextValue {
  const ctx = useContext(TenantContext);
  if (!ctx) {
    throw new Error('useTenantSelection must be used within a TenantProvider');
  }
  return ctx;
}
