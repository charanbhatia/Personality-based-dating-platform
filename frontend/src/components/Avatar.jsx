// Avatar with graceful fallback: shows the photo when available, otherwise a
// gradient tile with the person's initials (hue derived from the seed).
function initials(name = '') {
  const parts = name.trim().split(/\s+/).filter(Boolean);
  if (parts.length === 0) return '?';
  if (parts.length === 1) return parts[0].slice(0, 2).toUpperCase();
  return (parts[0][0] + parts[parts.length - 1][0]).toUpperCase();
}

function hue(seed = '') {
  let h = 0;
  for (let i = 0; i < seed.length; i++) h = (h * 31 + seed.charCodeAt(i)) % 360;
  return h;
}

export default function Avatar({ name, src, seed, size = 56, className = '', ring = false }) {
  const s = seed ?? name ?? '';
  const h = hue(s);
  const dim = { width: size, height: size, fontSize: size * 0.36 };

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
          style={{
            background: `linear-gradient(135deg, hsl(${h} 85% 62%), hsl(${(h + 60) % 360} 80% 55%))`,
          }}
        >
          {initials(name)}
        </span>
      )}
    </div>
  );
}
