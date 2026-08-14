import api from './client';
import type { DevicePlatform } from './types';

export const notifications = {
  list: (params?: Record<string, unknown>) => api.get('/api/v1/notifications', { params }),
  unreadCount: () => api.get('/api/v1/notifications/unread-count'),
  markRead: (id: string) => api.post(`/api/v1/notifications/${id}/read`),
  markAllRead: () => api.post('/api/v1/notifications/read-all'),
};

export const devices = {
  register: (token: string, platform: DevicePlatform = 'web') =>
    api.post('/api/v1/devices', { token, platform }),
  unregister: (token: string) => api.delete('/api/v1/devices', { data: { token } }),
};
