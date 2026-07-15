'use client';

import { CockpitConsole } from '@/components/cockpit-console';

// Unified cockpit at `/` — the console toggle is active (Unificado/NOC/SOC), with quick links to the
// dedicated /noc and /soc routes (B9).
export default function CockpitPage() {
  return <CockpitConsole />;
}
