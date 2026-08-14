import axios from 'axios';
import type { InternalAxiosRequestConfig } from 'axios';
import type { AuthTokens } from './types';

declare module 'axios' {
  export interface AxiosRequestConfig {
    skipAuthRefresh?: boolean;
    skipErrorToast?: boolean;
  }
}

const baseURL = import.meta.env.VITE_API_URL ?? (import.meta.env.DEV ? '' : 'http://localhost:8080');

const ACCESS_KEY = 'access_token';
const REFRESH_KEY = 'refresh_token';
const LEGACY_TOKEN_KEY = 'token';
const SESSION_HINT = 'session';

let accessMemory: string | null = null;
let onApiError: ((message: string) => void) | null = null;

export function setApiErrorHandler(fn: ((message: string) => void) | null) {
  onApiError = fn;
}

export function getAccessToken() {
  return accessMemory || localStorage.getItem(ACCESS_KEY) || localStorage.getItem(LEGACY_TOKEN_KEY);
}

export function getRefreshToken() {
  return localStorage.getItem(REFRESH_KEY);
}

export function persistTokens({ access_token, token }: AuthTokens = {}) {
  const access = access_token || token;
  if (access) {
    accessMemory = access;
    localStorage.removeItem(ACCESS_KEY);
    localStorage.removeItem(LEGACY_TOKEN_KEY);
    localStorage.removeItem(REFRESH_KEY);
    localStorage.setItem(SESSION_HINT, '1');
  }
}

export function clearTokens() {
  accessMemory = null;
  localStorage.removeItem(ACCESS_KEY);
  localStorage.removeItem(REFRESH_KEY);
  localStorage.removeItem(LEGACY_TOKEN_KEY);
  localStorage.removeItem(SESSION_HINT);
  localStorage.removeItem('user');
}

export function hasSession() {
  return Boolean(localStorage.getItem(SESSION_HINT) || getRefreshToken() || getAccessToken());
}

export function errorCode(err: unknown): string | undefined {
  const axiosErr = err as { response?: { data?: { error?: { code?: string } | string } } };
  const body = axiosErr?.response?.data?.error;
  if (body && typeof body === 'object') return body.code;
  return undefined;
}

function errorMessage(data: unknown, fallback: string) {
  const body = (data as { error?: unknown } | undefined)?.error;
  if (typeof body === 'string' && body) return body;
  if (body && typeof body === 'object') {
    const details = (body as { details?: Record<string, unknown>; message?: string }).details;
    if (details && typeof details === 'object') {
      const first = Object.values(details).find(Boolean);
      if (first) return String(first);
    }
    if ((body as { message?: string }).message) return (body as { message: string }).message;
  }
  return fallback;
}

const api = axios.create({
  baseURL,
  withCredentials: true,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = getAccessToken();
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

const isAuthAttempt = (url = '') =>
  /\/auth\/(login|register|refresh|password\/forgot|password\/reset|email\/verify)\b/.test(url);

let refreshInFlight: Promise<string> | null = null;

export function refreshSession() {
  if (!refreshInFlight) {
    const body: { refresh_token?: string } = {};
    const leftover = getRefreshToken();
    if (leftover) body.refresh_token = leftover;
    refreshInFlight = api
      .post('/api/v1/auth/refresh', body, { skipAuthRefresh: true, skipErrorToast: true })
      .then((res) => {
        persistTokens(res.data);
        return res.data.access_token as string;
      })
      .finally(() => {
        refreshInFlight = null;
      });
  }
  return refreshInFlight;
}

export async function restoreSession() {
  try {
    await refreshSession();
    return true;
  } catch {
    const leftover = localStorage.getItem(ACCESS_KEY) || localStorage.getItem(LEGACY_TOKEN_KEY);
    if (leftover) {
      accessMemory = leftover;
      localStorage.removeItem(ACCESS_KEY);
      localStorage.removeItem(LEGACY_TOKEN_KEY);
      return true;
    }
    clearTokens();
    return false;
  }
}

api.interceptors.response.use(
  (r) => r,
  async (err) => {
    const status = err.response?.status;
    const cfg = (err.config || {}) as InternalAxiosRequestConfig & {
      skipAuthRefresh?: boolean;
      skipErrorToast?: boolean;
    };
    const fallback = err.response
      ? 'Something went wrong. Please try again.'
      : 'Cannot reach the server. Check your connection and try again.';
    err.message = errorMessage(err.response?.data, fallback);

    if (status === 401 && !cfg.skipAuthRefresh && !isAuthAttempt(cfg.url || '')) {
      try {
        const access = await refreshSession();
        cfg.headers = cfg.headers || {};
        cfg.headers.Authorization = `Bearer ${access}`;
        cfg.skipAuthRefresh = true;
        return api.request(cfg);
      } catch {
        clearTokens();
        if (!/^\/(login|register|forgot-password|reset-password|verify-email)/.test(window.location.pathname)) {
          window.location.href = '/login';
        }
      }
    }

    const skipToast = cfg.skipErrorToast || isAuthAttempt(cfg.url || '') || status === 401;
    if (onApiError && !skipToast && err.message) {
      onApiError(err.message);
    }

    return Promise.reject(err);
  }
);

export default api;
