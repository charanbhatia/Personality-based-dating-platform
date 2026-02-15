import axios from 'axios';

// In dev, use same origin so Vite proxy forwards /api to backend (no CORS)
const baseURL = import.meta.env.VITE_API_URL ?? (import.meta.env.DEV ? '' : 'http://localhost:8080');

export const api = axios.create({
  baseURL,
  headers: { 'Content-Type': 'application/json' },
});

api.interceptors.request.use((config) => {
  const token = localStorage.getItem('token');
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

api.interceptors.response.use(
  (r) => r,
  (err) => {
    if (err.response?.status === 401) {
      localStorage.removeItem('token');
      localStorage.removeItem('user');
      window.location.href = '/login';
    }
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
