import { useState } from 'react';

// Avatar with graceful fallback: shows the photo when available, otherwise a
// flat ink tile with the person's initials. A photo that fails to load falls
// back too — a stored URL can outlive the object it points at, and a broken
// image icon is worse than initials.
function initials(name = '') {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

// Four inks rather than a full hue wheel — a rainbow of avatars would fight the
// one-accent palette, but a single flat colour makes every stranger look alike.
const INKS = ['#6d2637', '#2f5d50', '#3f4a63', '#7a5233'];

function pick(seed = '') {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) % 997;
  return INKS[h % INKS.length];
}

export default function Avatar({
  name,
  src,
  seed,
  size = 56,
  className = '',
  ring = false,
  fill = false,
}) {
  // `fill` lets a container size the tile instead — a match card wants the ink
  // edge to edge so the name scrim has something solid to sit on.
  // A new src deserves a fresh attempt; otherwise one bad URL would poison the
  // tile for every person rendered through the same slot. Reset during render
  // rather than in an effect, which would cascade an extra render.
  const [failed, setFailed] = useState(false);
  const [lastSrc, setLastSrc] = useState(src);
  if (src !== lastSrc) {
    setLastSrc(src);
    setFailed(false);
  }

  const dim = fill
    ? { fontSize: size * 0.36 }
    : { width: size, height: size, fontSize: size * 0.36 };

  // No aria-label on the wrapper: ARIA forbids naming a generic <div>, so screen
  // readers drop it. The <img alt> names the photo branch; the initials tile is
  // decorative because a real name always sits next to it.
  return (
    <div className={`avatar ${ring ? 'avatar--ring' : ''} ${className}`} style={dim}>
      {src && !failed ? (
        <img src={src} alt={name || ''} loading="lazy" onError={() => setFailed(true)} />
      ) : (
        <span
          aria-hidden="true"
          className="avatar-fallback"
          style={{ background: pick(seed ?? name ?? '') }}
        >
          {initials(name)}
        </span>
      )}
    </div>
  );
}
