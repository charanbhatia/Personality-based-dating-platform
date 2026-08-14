import api from './client';

export const media = {
  presign: (content_type: string, byte_size: number) =>
    api.post('/api/v1/media/presign', { content_type, byte_size }),
  complete: (id: string) => api.post(`/api/v1/media/assets/${id}/complete`),
  get: (id: string) => api.get(`/api/v1/media/assets/${id}`),
};
