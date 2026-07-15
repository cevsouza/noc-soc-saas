'use client';

import { CockpitConsole } from '@/components/cockpit-console';

// Dedicated NOC console (B9): the cockpit locked to the NOC domain (network/availability), so a NOC
// team can bookmark /noc and land straight in their view.
export default function NocConsolePage() {
  return <CockpitConsole lockedConsole="noc" />;
}
