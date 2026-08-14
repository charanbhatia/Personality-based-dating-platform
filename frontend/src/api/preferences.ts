import api from './client';

export const preferences = {
  get: () => api.get('/api/v1/preferences'),
  update: (data: Record<string, unknown>) => api.put('/api/v1/preferences', data),
};
