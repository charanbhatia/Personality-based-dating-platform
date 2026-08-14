import api from './client';

export const discover = {
  list: (params?: Record<string, unknown>) => api.get('/api/v1/discover', { params }),
};

export const likes = {
  swipe: (user_id: string, action: 'like' | 'pass' | string) =>
    api.post('/api/v1/likes', { user_id, action }),
};
