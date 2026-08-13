// Avatar with graceful fallback: shows the photo when available, otherwise a
// flat ink tile with the person's initials.
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
  const dim = fill
    ? { fontSize: size * 0.36 }
    : { width: size, height: size, fontSize: size * 0.36 };

  // No aria-label on the wrapper: ARIA forbids naming a generic <div>, so screen
  // readers drop it. The <img alt> names the photo branch; the initials tile is
  // decorative because a real name always sits next to it.
  return (
    <div className={`avatar ${ring ? 'avatar--ring' : ''} ${className}`} style={dim}>
      {src ? (
        <img src={src} alt={name || ''} loading="lazy" />
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
