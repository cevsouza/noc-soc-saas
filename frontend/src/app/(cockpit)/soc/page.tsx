'use client';

import { CockpitConsole } from '@/components/cockpit-console';

// Dedicated SOC console (B9): the cockpit locked to the SOC domain (security), so a SOC team can
// bookmark /soc and land straight in their view.
export default function SocConsolePage() {
  return <CockpitConsole lockedConsole="soc" />;
}
