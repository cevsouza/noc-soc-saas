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

// Persist the domain (tenant) selection across reloads so the cockpit reopens on the same scope the
// operator was working in, instead of snapping back to "all tenants".
const SELECTION_STORAGE_KEY = 'noc_selected_tenants';

function loadStoredSelection(): string[] {
  if (typeof window === 'undefined') return [];
  try {
    const raw = window.localStorage.getItem(SELECTION_STORAGE_KEY);
    const parsed = raw ? JSON.parse(raw) : [];
    return Array.isArray(parsed) ? parsed.filter((x): x is string => typeof x === 'string') : [];
  } catch {
    return [];
  }
}

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
        // On first load prefer the persisted selection (restored across reloads); fall back to all.
        const source = prev.length > 0 ? prev : loadStoredSelection();
        const validIds = source.filter((id) => data.some((t) => t.id === id));
        return validIds.length > 0 ? validIds : data.map((t) => t.id);
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

  // Persist every non-empty selection so a reload restores it (see loadStoredSelection).
  useEffect(() => {
    if (typeof window === 'undefined' || selectedTenantIds.length === 0) return;
    try {
      window.localStorage.setItem(SELECTION_STORAGE_KEY, JSON.stringify(selectedTenantIds));
    } catch {
      /* ignore quota/serialization errors */
    }
  }, [selectedTenantIds]);

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
