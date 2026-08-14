import api from './client';

export const matches = {
  list: (params?: Record<string, unknown>) => api.get('/api/v1/matches', { params }),
  get: (id: string) => api.get(`/api/v1/users/${id}/public`),
};

export const safety = {
  block: (user_id: string) => api.post('/api/v1/blocks', { user_id }),
  unblock: (id: string) => api.delete(`/api/v1/blocks/${id}`),
  listBlocks: () => api.get('/api/v1/blocks'),
  report: (user_id: string, reason: string, details?: string) =>
    api.post('/api/v1/reports', { user_id, reason, details }),
};
