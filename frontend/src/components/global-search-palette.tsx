'use client';

import { useState } from 'react';
import { AlertTriangle, Building2, Terminal } from 'lucide-react';
import { Dialog, DialogContent } from '@/components/ui/dialog';
import { Command, CommandEmpty, CommandGroup, CommandInput, CommandItem, CommandList } from '@/components/ui/command';
import { SeverityBadge } from '@/components/alerts/severity-badge';
import { useGlobalSearch } from '@/lib/use-global-search';
import type { AlertSeverity, SearchAlertResult, SearchRunbookResult, SearchTenantResult } from '@/types';

interface GlobalSearchPaletteProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tenantIds: string[];
  onSelectAlert: (result: SearchAlertResult) => void;
  onSelectTenant: (result: SearchTenantResult) => void;
  onSelectRunbook: (result: SearchRunbookResult) => void;
}

// Cmd+K (Ctrl+K on non-Mac) global command palette — searches alerts/runbooks/tenants across
// whatever the caller currently has access to, via GET /api/v1/search.
export function GlobalSearchPalette({ open, onOpenChange, tenantIds, onSelectAlert, onSelectTenant, onSelectRunbook }: GlobalSearchPaletteProps) {
  const [query, setQuery] = useState('');
  const { results, isLoading } = useGlobalSearch(query, tenantIds);

  const hasResults = results.alerts.length > 0 || results.runbooks.length > 0 || results.tenants.length > 0;

  const select = (action: () => void) => {
    action();
    onOpenChange(false);
    setQuery('');
  };

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="overflow-hidden p-0">
        {/* shouldFilter={false}: results are already filtered server-side (GET /api/v1/search);
            cmdk's default client-side fuzzy filter would otherwise match each CommandItem's
            `value` (used here as a stable React key, e.g. "alert-<uuid>") against the typed
            query and hide every item since the uuid never contains the search text. */}
        <Command
          shouldFilter={false}
          className="[&_[cmdk-group-heading]]:px-2 [&_[cmdk-group-heading]]:font-medium [&_[cmdk-group-heading]]:text-muted-foreground [&_[cmdk-group]:not([hidden])_~[cmdk-group]]:pt-0 [&_[cmdk-group]]:px-2 [&_[cmdk-input-wrapper]_svg]:h-5 [&_[cmdk-input-wrapper]_svg]:w-5 [&_[cmdk-input]]:h-12 [&_[cmdk-item]]:px-2 [&_[cmdk-item]]:py-3 [&_[cmdk-item]_svg]:h-5 [&_[cmdk-item]_svg]:w-5"
        >
          <CommandInput placeholder="Buscar alertas, runbooks, clientes..." value={query} onValueChange={setQuery} />
          <CommandList>
            {query.trim().length >= 2 && !isLoading && !hasResults && <CommandEmpty>Nenhum resultado encontrado.</CommandEmpty>}
            {query.trim().length < 2 && <CommandEmpty>Digite pelo menos 2 caracteres para buscar.</CommandEmpty>}

            {results.alerts.length > 0 && (
              <CommandGroup heading="Alertas">
                {results.alerts.map((a) => (
                  <CommandItem key={a.id} value={`alert-${a.id}`} onSelect={() => select(() => onSelectAlert(a))}>
                    <AlertTriangle className="w-4 h-4 text-slate-400" />
                    <span className="flex-1 truncate">{a.summary}</span>
                    <SeverityBadge severity={a.severity as AlertSeverity} />
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {results.runbooks.length > 0 && (
              <CommandGroup heading="Runbooks">
                {results.runbooks.map((rb) => (
                  <CommandItem key={rb.id} value={`runbook-${rb.id}`} onSelect={() => select(() => onSelectRunbook(rb))}>
                    <Terminal className="w-4 h-4 text-cyan-400" />
                    <span className="flex-1 truncate">{rb.name}</span>
                    {rb.is_global && <span className="text-[9px] uppercase font-bold text-slate-500">Global</span>}
                  </CommandItem>
                ))}
              </CommandGroup>
            )}

            {results.tenants.length > 0 && (
              <CommandGroup heading="Clientes">
                {results.tenants.map((t) => (
                  <CommandItem key={t.id} value={`tenant-${t.id}`} onSelect={() => select(() => onSelectTenant(t))}>
                    <Building2 className="w-4 h-4 text-violet-400" />
                    <span className="flex-1 truncate">{t.name}</span>
                  </CommandItem>
                ))}
              </CommandGroup>
            )}
          </CommandList>
        </Command>
      </DialogContent>
    </Dialog>
  );
}
