import { API_BASE_URL } from './env';

const SESSION_KEYS = ['noc_token', 'noc_user', 'noc_tenant'] as const;

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
    this.name = 'ApiError';
  }
}

function clearSessionAndRedirect() {
  if (typeof window === 'undefined') return;
  SESSION_KEYS.forEach((key) => window.localStorage.removeItem(key));
  if (window.location.pathname !== '/login') {
    window.location.href = '/login';
  }
}

function getStoredToken(): string | null {
  if (typeof window === 'undefined') return null;
  return window.localStorage.getItem('noc_token');
}

interface ApiFetchOptions extends RequestInit {
  /** Explicit token override. Defaults to reading `noc_token` from localStorage. */
  token?: string | null;
  /** Skip the automatic 401 -> clear-session-and-redirect behavior (e.g. for the login call itself). */
  skipAuthRedirect?: boolean;
}

/**
 * Centralized fetch wrapper for all backend calls.
 *
 * - Auto-attaches `Authorization: Bearer <token>` (from localStorage unless overridden).
 * - Retries transient 502/503/504 responses and network errors with exponential backoff
 *   (2 retries, 500ms -> 1000ms) — ported as-is from the original `window.fetch` monkey-patch
 *   that used to live at module scope in page.tsx.
 * - On a 401, clears the stored session and redirects to /login — this is NEW behavior; no
 *   401 handling existed before this client (every call site used to just show a generic error).
 */
export async function apiFetch(path: string, options: ApiFetchOptions = {}): Promise<Response> {
  const { token, skipAuthRedirect, headers, ...rest } = options;
  const url = path.startsWith('http') ? path : `${API_BASE_URL}${path}`;
  const authToken = token !== undefined ? token : getStoredToken();

  const finalHeaders = new Headers(headers);
  if (authToken) {
    finalHeaders.set('Authorization', `Bearer ${authToken}`);
  }

  const response = await fetchWithRetry(url, { ...rest, headers: finalHeaders });

  if (response.status === 401 && !skipAuthRedirect) {
    clearSessionAndRedirect();
  }

  return response;
}

async function fetchWithRetry(url: string, init: RequestInit, retries = 2, delay = 500): Promise<Response> {
  try {
    const response = await fetch(url, init);
    if (!response.ok && [502, 503, 504].includes(response.status) && retries > 0) {
      console.warn(`[api-client] Received status ${response.status} from ${url}. Retrying in ${delay}ms... (${retries} attempts left)`);
      await new Promise((resolve) => setTimeout(resolve, delay));
      return fetchWithRetry(url, init, retries - 1, delay * 2);
    }
    return response;
  } catch (err) {
    if (retries > 0) {
      console.warn(`[api-client] Network error on ${url}. Retrying in ${delay}ms... (${retries} attempts left):`, err);
      await new Promise((resolve) => setTimeout(resolve, delay));
      return fetchWithRetry(url, init, retries - 1, delay * 2);
    }
    console.error(`[api-client] Fatal network error on ${url}:`, err);
    throw err;
  }
}

/** Convenience helper: apiFetch + JSON parse + throws ApiError on non-ok responses. */
export async function apiFetchJson<T>(path: string, options?: ApiFetchOptions): Promise<T> {
  const response = await apiFetch(path, options);
  if (!response.ok) {
    const text = await response.text().catch(() => '');
    throw new ApiError(response.status, text || `Request failed with status ${response.status}`);
  }
  return response.json() as Promise<T>;
}
