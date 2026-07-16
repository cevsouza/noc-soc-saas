'use client';

import React, { createContext, useCallback, useContext, useEffect, useState } from 'react';
import { API_BASE_URL } from './env';
import type { SessionUser, Tenant } from '@/types';

interface LoginResult {
  ok: boolean;
  message?: string;
  /** The user's landing-console preference (B9), so the caller can redirect to /noc or /soc. */
  defaultConsole?: 'all' | 'noc' | 'soc';
}

interface RegisterResult {
  ok: boolean;
  autoVerified?: boolean;
  message?: string;
}

interface AuthContextValue {
  token: string | null;
  user: SessionUser | null;
  tenant: Tenant | null;
  /** True until the initial localStorage rehydration effect has run once. */
  isLoading: boolean;
  login: (email: string, password: string) => Promise<LoginResult>;
  register: (email: string, password: string, name: string) => Promise<RegisterResult>;
  setDefaultConsole: (console: 'all' | 'noc' | 'soc') => Promise<void>;
  logout: () => void;
}

const AuthContext = createContext<AuthContextValue | undefined>(undefined);

async function extractErrorMessage(response: Response, fallback: string): Promise<string> {
  const contentType = response.headers.get('content-type');
  if (contentType && contentType.includes('text/html')) {
    return 'Erro de conexão: o servidor retornou uma página HTML. Verifique a variável NEXT_PUBLIC_API_URL do frontend.';
  }
  const text = await response.text().catch(() => '');
  return text || fallback;
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [token, setToken] = useState<string | null>(null);
  const [user, setUser] = useState<SessionUser | null>(null);
  const [tenant, setTenant] = useState<Tenant | null>(null);
  const [isLoading, setIsLoading] = useState(true);

  // Rehydrate session from localStorage on mount — same three keys/behavior as the original
  // page.tsx implementation (noc_token/noc_user/noc_tenant).
  useEffect(() => {
    const storedToken = window.localStorage.getItem('noc_token');
    const storedUser = window.localStorage.getItem('noc_user');
    const storedTenant = window.localStorage.getItem('noc_tenant');
    if (storedToken) setToken(storedToken);
    if (storedUser) {
      try {
        setUser(JSON.parse(storedUser));
      } catch {
        // ignore malformed cached value
      }
    }
    if (storedTenant) {
      try {
        setTenant(JSON.parse(storedTenant));
      } catch {
        // ignore malformed cached value
      }
    }
    setIsLoading(false);
  }, []);

  const login = useCallback(async (email: string, password: string): Promise<LoginResult> => {
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password }),
      });
      if (!response.ok) {
        return { ok: false, message: await extractErrorMessage(response, 'Credenciais inválidas.') };
      }
      const data = await response.json();
      window.localStorage.setItem('noc_token', data.token);
      window.localStorage.setItem('noc_user', JSON.stringify(data.user));
      window.localStorage.setItem('noc_tenant', JSON.stringify(data.tenant));
      setToken(data.token);
      setUser(data.user);
      setTenant(data.tenant);
      return { ok: true, defaultConsole: data.user?.default_console ?? 'all' };
    } catch {
      return { ok: false, message: 'Falha ao se conectar com o servidor.' };
    }
  }, []);

  // Persist the user's landing-console preference (B9) and update the in-memory/localStorage session.
  const setDefaultConsole = useCallback(async (console: 'all' | 'noc' | 'soc') => {
    if (!token) return;
    try {
      await fetch(`${API_BASE_URL}/api/v1/users/me/console`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json', Authorization: `Bearer ${token}` },
        body: JSON.stringify({ console }),
      });
    } catch {
      // Best-effort; the optimistic local update below still reflects the choice for this session.
    }
    setUser((prev) => {
      if (!prev) return prev;
      const next = { ...prev, default_console: console };
      window.localStorage.setItem('noc_user', JSON.stringify(next));
      return next;
    });
  }, [token]);

  const register = useCallback(async (email: string, password: string, name: string): Promise<RegisterResult> => {
    try {
      const response = await fetch(`${API_BASE_URL}/api/v1/auth/register`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ email, password, name }),
      });
      if (!response.ok) {
        return { ok: false, message: await extractErrorMessage(response, 'Falha ao registrar.') };
      }
      const data = await response.json();
      return { ok: true, autoVerified: !!data.auto_verified, message: data.message };
    } catch {
      return { ok: false, message: 'Falha ao se conectar com o servidor.' };
    }
  }, []);

  const logout = useCallback(() => {
    window.localStorage.removeItem('noc_token');
    window.localStorage.removeItem('noc_user');
    window.localStorage.removeItem('noc_tenant');
    setToken(null);
    setUser(null);
    setTenant(null);
  }, []);

  return (
    <AuthContext.Provider value={{ token, user, tenant, isLoading, login, register, setDefaultConsole, logout }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth(): AuthContextValue {
  const ctx = useContext(AuthContext);
  if (!ctx) {
    throw new Error('useAuth must be used within an AuthProvider');
  }
  return ctx;
}
