import api from './client';

export const personality = {
  assessment: () => api.get('/api/v1/personality/assessment'),
  submit: (answers: Array<{ question_id: string; value: number }>) =>
    api.post('/api/v1/personality/assessment/submit', { answers }),
  me: () => api.get('/api/v1/personality/me'),
};
