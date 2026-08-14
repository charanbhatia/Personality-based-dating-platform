import api from './client';

export const conversations = {
  list: (params?: Record<string, unknown>) => api.get('/api/v1/conversations', { params }),
  get: (id: string) => api.get(`/api/v1/conversations/${id}`),
  start: (match_id: string) => api.post('/api/v1/conversations', { match_id }),
  getMessages: (id: string, params?: Record<string, unknown>) =>
    api.get(`/api/v1/conversations/${id}/messages`, { params }),
  sendMessage: (id: string, content: string, client_msg_id?: string) =>
    api.post(`/api/v1/conversations/${id}/messages`, {
      content,
      client_msg_id: client_msg_id || crypto.randomUUID(),
    }),
  markRead: (id: string) => api.post(`/api/v1/conversations/${id}/read`),
};
