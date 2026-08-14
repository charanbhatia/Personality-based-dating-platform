// How a compatibility score is allowed to be described.
//
// The score is a mean of (1 - |difference|) across the Big Five traits both
// people have answered. It is one number derived from five self-reported
// values, so it does not resolve to a percentage point — everything here
// coarsens deliberately.

// The server returns a literal 0.5 when two people share no answered traits
// (backend/internal/handlers/match.go). A real mean can land on 0.5 too, but
// treating the sentinel as unscored fails safe: better to under-claim than to
// present a confident number computed from nothing.
export const UNSCORED = 0.5;

export function isScored(score) {
  return typeof score === 'number' && score !== UNSCORED;
}

// The band's colour is information, not decoration: closest reads teal,
// middling reads brand pink, thin reads muted.
export function bandTone(score) {
  if (!isScored(score)) return 'tone-none';
  if (score >= 0.85) return 'tone-high';
  if (score >= 0.75) return 'tone-mid';
  return 'tone-low';
}

export function band(score) {
  if (!isScored(score)) return 'Not scored yet';
  if (score >= 0.85) return 'Unusually close';
  if (score >= 0.75) return 'Strong overlap';
  if (score >= 0.65) return 'Some common ground';
  return 'Little in common';
}

// Rounded to fives — the underlying metric cannot justify finer.
export function coarse(score) {
  return Math.round(score * 20) * 5;
}

export function ordinal(n) {
  const s = ['th', 'st', 'nd', 'rd'];
  const v = n % 100;
  return n + (s[(v - 20) % 10] || s[v] || s[0]);
}

/**
 * Highest first. Also reports whether ranking is meaningful at all: if every
 * score is the same, or they are all the unscored sentinel, presenting an order
 * would invent a hierarchy the data does not contain.
 */
export function scoreOf(item) {
  const s = item?.compatibility_score ?? item?.score;
  return typeof s === 'number' ? s : 0;
}

export function photoOf(item) {
  if (!item) return '';
  return item.photo_url || item.primary_photo_url || item.photo_urls?.[0] || '';
}

export function rank(matches = []) {
  const list = [...matches].sort((a, b) => scoreOf(b) - scoreOf(a));
  const scores = list.map((m) => scoreOf(m));
  const spread = scores.length ? scores[0] - scores[scores.length - 1] : 0;
  return { list, scores, spread, ranked: list.some((m) => isScored(scoreOf(m))) && spread >= 0.02 };
}
