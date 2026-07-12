'use client';

import React, { useEffect } from 'react';
import { useRouter } from 'next/navigation';
import { useAuth } from '@/lib/auth-context';
import { TenantProvider } from '@/lib/tenant-context';

export default function CockpitLayout({ children }: { children: React.ReactNode }) {
  const router = useRouter();
  const { token, isLoading } = useAuth();

  useEffect(() => {
    if (!isLoading && !token) {
      router.replace('/login');
    }
  }, [isLoading, token, router]);

  if (isLoading) {
    return (
      <div className="min-h-screen bg-background flex items-center justify-center text-slate-400 text-sm">
        Carregando sessão...
      </div>
    );
  }

  if (!token) {
    // Redirect effect above is in flight — render nothing to avoid a flash of protected content.
    return null;
  }

  return <TenantProvider>{children}</TenantProvider>;
}
