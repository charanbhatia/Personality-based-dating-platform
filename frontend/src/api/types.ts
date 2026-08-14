export type Gender = 'woman' | 'man' | 'nonbinary' | 'other';

export type TraitKey =
  | 'openness'
  | 'conscientiousness'
  | 'extraversion'
  | 'agreeableness'
  | 'neuroticism';

export type Traits = Record<TraitKey, number>;
export type TraitWeights = Partial<Record<TraitKey, number>>;

export type AuthTokens = {
  access_token?: string;
  token?: string;
  refresh_token?: string;
};

export type SessionUser = {
  id: string;
  email?: string;
  name?: string;
  date_of_birth?: string;
  email_verified?: boolean;
  onboarding?: {
    quiz_done?: boolean;
    preferences_done?: boolean;
    profile_done?: boolean;
    photos_done?: boolean;
  };
  [key: string]: unknown;
};

export type DevicePlatform = 'web' | 'android' | 'ios';
