// Single source of truth for the backend base URL and WebSocket URL resolution — previously
// duplicated independently in page.tsx's `API_BASE_URL` (top-level) and `getWSUrl` (which
// re-derived the same hostname/protocol logic on its own).

function resolveApiBaseUrl(): string {
  let base = process.env.NEXT_PUBLIC_API_URL || 'http://localhost:8080';

  if (typeof window !== 'undefined') {
    const hostname = window.location.hostname;
    const isLocalhost =
      hostname === 'localhost' ||
      hostname === '127.0.0.1' ||
      hostname.startsWith('192.168.') ||
      hostname.startsWith('10.');
    // Runtime fallback for when NEXT_PUBLIC_API_URL wasn't baked in at build time but the page
    // is clearly not running locally — kept as-is from the original implementation (ported
    // behavior, not a new decision) since removing it silently could break the deployed
    // cockpit-ui Railway service if its env var is ever missing.
    if (!isLocalhost && base.includes('localhost')) {
      base = 'https://noc-soc-saas-production.up.railway.app';
    }
  }

  return base;
}

export const API_BASE_URL = resolveApiBaseUrl();

export function getWSUrl(token: string, tenantIds: string[]): string {
  const base = resolveApiBaseUrl();
  const host = base.replace(/^https?:\/\//, '');

  let wsProtocol = 'ws';
  if (base.startsWith('https') || (typeof window !== 'undefined' && window.location.protocol === 'https:')) {
    wsProtocol = 'wss';
  }

  return `${wsProtocol}://${host}/api/v1/ws?token=${encodeURIComponent(token)}&tenants=${tenantIds.join(',')}`;
}
