export function onboardingOf(user) {
  return user?.onboarding || {};
}

/** Quiz + preferences + profile + at least one photo. */
export function needsOnboarding(user) {
  const o = onboardingOf(user);
  return !o.quiz_done || !o.preferences_done || !o.profile_done || !o.photos_done;
}

export function nextOnboardingPath(user) {
  const o = onboardingOf(user);
  if (!o.quiz_done) return '/onboarding/quiz';
  if (!o.preferences_done) return '/onboarding/preferences';
  if (!o.profile_done) return '/onboarding/profile';
  if (!o.photos_done) return '/onboarding/photos';
  return '/app';
}

export function afterAuthPath(user) {
  return needsOnboarding(user) ? nextOnboardingPath(user) : '/app';
}
