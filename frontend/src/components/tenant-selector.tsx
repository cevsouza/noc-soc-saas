'use client';

import React, { useState } from 'react';
import { ChevronDown, User } from 'lucide-react';
import { useTenantSelection } from '@/lib/tenant-context';
import { Checkbox } from '@/components/ui/checkbox';

/**
 * Multi-tenant checkbox selector, extracted from page.tsx:1850-1927. Behavior preserved
 * exactly: admin users get the full multi-select dropdown ("Marcar Todas" + per-tenant
 * checkboxes, can't deselect the last remaining tenant); everyone else just sees their single
 * fixed tenant name.
 */
export function TenantSelector({ isAdmin, singleTenantName }: { isAdmin: boolean; singleTenantName?: string }) {
  const { tenants, selectedTenantIds, toggleTenant, selectAllTenants } = useTenantSelection();
  const [isOpen, setIsOpen] = useState(false);

  if (!isAdmin) {
    return (
      <div className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-white/5 border border-white/5">
        <User className="w-4 h-4 text-slate-400" />
        <span className="text-xs text-slate-300 font-medium">Tenant:</span>
        <span className="text-xs text-violet-400 font-bold">{singleTenantName}</span>
      </div>
    );
  }

  const label =
    selectedTenantIds.length === tenants.length
      ? 'Multi-Tenant (Todos)'
      : selectedTenantIds.length === 1
        ? tenants.find((t) => t.id === selectedTenantIds[0])?.name || '1 Selecionado'
        : `${selectedTenantIds.length} Empresas`;

  return (
    <div className="relative">
      <button
        type="button"
        onClick={() => setIsOpen((v) => !v)}
        className="flex items-center gap-2 px-3 py-1.5 rounded-lg bg-white/5 border border-white/5 text-xs text-slate-300 font-bold hover:bg-white/10 transition-all select-none cursor-pointer"
      >
        <User className="w-3.5 h-3.5 text-violet-400" />
        <span>Visual Domain:</span>
        <span className="text-violet-400 font-extrabold">{label}</span>
        <ChevronDown className="w-3 h-3 text-slate-400" />
      </button>

      {isOpen && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setIsOpen(false)} />
          <div className="absolute right-0 mt-2 w-64 rounded-xl border border-white/10 bg-slate-950 p-2 shadow-2xl z-50 flex flex-col gap-1 backdrop-blur-md">
            <div className="px-3 py-1 text-[9px] font-bold text-slate-500 uppercase tracking-widest border-b border-white/5 mb-1 flex items-center justify-between">
              <span>Selecionar Empresas</span>
              <button
                type="button"
                onClick={selectAllTenants}
                className="text-[9px] text-cyan-400 hover:text-cyan-300 uppercase font-bold"
              >
                Marcar Todas
              </button>
            </div>
            <div className="flex flex-col max-h-48 overflow-y-auto pr-1">
              {tenants.map((t) => {
                const isChecked = selectedTenantIds.includes(t.id);
                return (
                  <label
                    key={t.id}
                    className={`flex items-center gap-2.5 px-2.5 py-2 rounded-lg cursor-pointer transition-all hover:bg-white/[0.03] select-none text-xs font-medium ${
                      isChecked ? 'text-white' : 'text-slate-400'
                    }`}
                  >
                    <Checkbox checked={isChecked} onCheckedChange={() => toggleTenant(t.id)} />
                    <span className="truncate">{t.name}</span>
                  </label>
                );
              })}
            </div>
          </div>
        </>
      )}
    </div>
  );
}
