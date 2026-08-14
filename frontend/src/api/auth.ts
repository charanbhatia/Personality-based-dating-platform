import api from './client';
import type { SessionUser } from './types';

export const auth = {
  register: (data: Record<string, unknown>) => api.post('/api/v1/auth/register', data),
  login: (data: { email: string; password: string }) => api.post('/api/v1/auth/login', data),
  me: () => api.get('/api/v1/auth/me'),
  logout: () => api.post('/api/v1/auth/logout', undefined, { skipAuthRefresh: true, skipErrorToast: true }),
  refresh: (refresh_token: string) =>
    api.post('/api/v1/auth/refresh', { refresh_token }, { skipAuthRefresh: true, skipErrorToast: true }),
  forgotPassword: (email: string) => api.post('/api/v1/auth/password/forgot', { email }),
  resetPassword: (token: string, password: string) =>
    api.post('/api/v1/auth/password/reset', { token, password }),
  verifyEmail: (token: string) => api.post('/api/v1/auth/email/verify', { token }),
  resendEmail: () => api.post('/api/v1/auth/email/resend'),
  sessions: () => api.get('/api/v1/auth/sessions'),
  revokeSession: (id: string) => api.delete(`/api/v1/auth/sessions/${id}`),
};

export function sessionUser(body: unknown): SessionUser | null {
  if (!body || typeof body !== 'object') return null;
  const rec = body as { user?: SessionUser; onboarding?: SessionUser['onboarding'] };
  if (rec.user) return { ...rec.user, onboarding: rec.onboarding ?? rec.user.onboarding };
  return rec as SessionUser;
}
