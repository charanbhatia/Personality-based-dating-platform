import api from './client';

export const profile = {
  get: () => api.get('/api/v1/profile'),
  update: (data: Record<string, unknown>) => api.put('/api/v1/profile', data),
  setPhotos: (body: { asset_ids?: Array<string | null>; photo_urls?: string[] }) =>
    api.put('/api/v1/profile/photos', body),
  options: () => api.get('/api/v1/profile/options'),
  public: (id: string) => api.get(`/api/v1/users/${id}/public`),
};
