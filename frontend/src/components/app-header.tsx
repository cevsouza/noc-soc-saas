'use client';

import React from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/lib/auth-context';
import { useTenantSelection } from '@/lib/tenant-context';
import { TenantSelector } from '@/components/tenant-selector';
import { ThemeToggle } from '@/components/theme-toggle';
import type { ConnectionStatus } from '@/lib/use-alerts-socket';

const DEFAULT_LOGO =
  'https://lirp.cdn-website.com/2cf4cfdc/dms3rep/multi/opt/IT+Facil+-+logo+-+alta-47c0885e-158w.png';

export function AppHeader({ connStatus }: { connStatus: ConnectionStatus }) {
  const router = useRouter();
  const { user, logout } = useAuth();
  const { tenants, selectedTenantIds } = useTenantSelection();

  const isAdmin = user?.role === 'admin';
  const singleTenant = tenants.find((t) => t.id === selectedTenantIds[0]);
  const logoUrl = (selectedTenantIds.length === 1 && singleTenant?.logo_url) || DEFAULT_LOGO;

  const handleLogout = () => {
    logout();
    router.push('/login');
  };

  return (
    <header className="h-16 shrink-0 flex items-center justify-between px-6 border-b border-white/5 bg-surface/50 backdrop-blur-md sticky top-0 z-50">
      <div className="flex items-center gap-3">
        <div className="relative flex items-center justify-center h-11 w-36 overflow-hidden rounded-lg bg-white/5 p-1 border border-white/10">
          {/* eslint-disable-next-line @next/next/no-img-element */}
          <img src={logoUrl} alt="Brand Logo" className="h-full w-auto object-contain" />
        </div>
        <div>
          <h1 className="text-sm font-bold tracking-wider text-slate-100 flex items-center gap-2">
            ITFácil NOC{' '}
            <span className="text-xs px-2 py-0.5 rounded-full bg-violet-900/60 border border-violet-500/30 text-violet-300">
              2.0 ENGINE
            </span>
          </h1>
          <p className="text-[10px] text-slate-400 tracking-wide uppercase">Real-Time Cockpit</p>
        </div>
      </div>

      <div className="flex items-center gap-4">
        <TenantSelector isAdmin={isAdmin} singleTenantName={singleTenant?.name} />

        <ThemeToggle />

        <div className="flex items-center gap-3 px-3 py-1.5 rounded-lg bg-white/5 border border-white/5 ml-2">
          <div className="flex flex-col text-right">
            <span className="text-[10px] text-white font-bold leading-none">{user?.name}</span>
            <div className="flex items-center gap-1.5 justify-end mt-0.5">
              <span className="text-[8px] text-slate-400 uppercase tracking-widest font-semibold">{user?.role}</span>
              <span className="text-[8px] text-slate-500">•</span>
              <span
                className={`text-[8px] font-bold uppercase tracking-wider flex items-center gap-1 ${
                  connStatus === 'connected'
                    ? 'text-emerald-400'
                    : connStatus === 'connecting'
                      ? 'text-amber-400'
                      : 'text-rose-400'
                }`}
              >
                <span
                  className={`w-1 h-1 rounded-full ${
                    connStatus === 'connected'
                      ? 'bg-emerald-400 animate-pulse'
                      : connStatus === 'connecting'
                        ? 'bg-amber-400 animate-pulse'
                        : 'bg-rose-400'
                  }`}
                />
                {connStatus === 'connected' ? 'On' : connStatus === 'connecting' ? '...' : 'Off'}
              </span>
            </div>
          </div>
          <button
            type="button"
            onClick={handleLogout}
            className="text-[9px] text-rose-400 hover:text-rose-300 bg-rose-500/10 hover:bg-rose-500/20 px-2 py-1 rounded transition-all font-bold cursor-pointer"
          >
            Sair
          </button>
        </div>
      </div>
    </header>
  );
}
