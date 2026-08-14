import axios from 'axios';

// In dev, use same origin so Vite proxy forwards /api to backend (no CORS)
const baseURL = import.meta.env.VITE_API_URL ?? (import.meta.env.DEV ? '' : 'http://localhost:8080');

const api = axios.create({
  baseURL,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// A 401 from the login/register endpoints means "bad credentials", not "session
// expired" — redirecting there would reload the page and wipe the error the form
// just set, leaving the user staring at a blank form.
const isAuthAttempt = (url = '') =>
  url.endsWith('/auth/login') || url.endsWith('/auth/register');

api.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response?.status === 401 && !isAuthAttempt(err.config?.url)) {
      localStorage.removeItem('token');
      window.location.href = '/login';
    }
    // Normalize once here so callers can just render err.message instead of each
    // re-deriving the same response/network/fallback ladder.
    err.message =
      err.response?.data?.error ||
      (err.response
        ? 'Something went wrong. Please try again.'
        : 'Cannot reach the server. Check your connection and try again.');
    return Promise.reject(err);
  }
);

export const auth = {
  register: (data) => api.post('/api/auth/register', data),
  login: (data) => api.post('/api/auth/login', data),
  me: () => api.get('/api/auth/me'),
};

export const profile = {
  get: () => api.get('/api/profile'),
  update: (data) => api.put('/api/profile', data),
};

export const matches = {
  list: (params) => api.get('/api/matches', { params }),
  get: (id) => api.get(`/api/matches/${id}`),
};

export const conversations = {
  list: () => api.get('/api/conversations'),
  start: (userId) => api.post('/api/conversations', { user_id: userId }),
  getMessages: (id, params) => api.get(`/api/conversations/${id}/messages`, { params }),
  sendMessage: (id, content) => api.post(`/api/conversations/${id}/messages`, { content }),
};
