const LABELS = {
  openness: 'Openness',
  conscientiousness: 'Drive',
  extraversion: 'Extraversion',
  agreeableness: 'Warmth',
  neuroticism: 'Sensitivity',
};

export function topTraits(traits, n = 3) {
  if (!traits || typeof traits !== 'object') return [];
  return Object.entries(traits)
    .filter(([, v]) => typeof v === 'number')
    .sort((a, b) => b[1] - a[1])
    .slice(0, n)
    .map(([key, value]) => ({
      key,
      label: LABELS[key] || key,
      value,
    }));
}

export function traitPercent(value) {
  return `${Math.round(value * 100)}`;
}
