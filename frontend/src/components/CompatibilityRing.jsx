import { useEffect, useRef, useState } from 'react';

/**
 * Circular compatibility gauge. By default the stroke sweeps up from zero the
 * first time it scrolls into view; the CSS transition then carries any later
 * change, so the ring also tracks a value that updates live.
 *
 * Only the reveal is stateful — the displayed number is derived from `value`.
 * Storing it instead meant a changing value could not reach the ring once the
 * observer had disconnected, and it made every update wait on a
 * requestAnimationFrame that browsers throttle in a background tab.
 */
export default function CompatibilityRing({
  value = 0,
  size = 92,
  stroke = 6,
  label = 'match',
  animateOnView = true,
}) {
  const pct = Math.max(0, Math.min(100, Math.round(value)));
  const [revealed, setRevealed] = useState(!animateOnView);
  const ref = useRef(null);

  const shown = revealed ? pct : 0;
  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const offset = c - (shown / 100) * c;

  useEffect(() => {
    if (revealed) return;
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          setRevealed(true);
          io.disconnect();
        }
      },
      { threshold: 0.4 }
    );
    io.observe(el);
    return () => io.disconnect();
  }, [revealed]);

  // A flat band, not a gradient: the colour carries meaning, so it should be
  // readable as one of three states rather than a decorative sweep.
  const tone = pct >= 80 ? 'var(--good)' : pct >= 60 ? 'var(--accent)' : 'var(--ink-mute)';

  return (
    <div ref={ref} className="ring" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`} aria-hidden="true">
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="var(--rule)"
          strokeWidth={stroke}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={tone}
          strokeWidth={stroke}
          strokeLinecap="butt"
          strokeDasharray={c}
          strokeDashoffset={offset}
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
          style={{ transition: 'stroke-dashoffset 0.7s cubic-bezier(0.22,1,0.36,1)' }}
        />
      </svg>
      <div className="ring-center">
        <span className="ring-value" style={{ fontSize: size < 70 ? '0.95rem' : undefined }}>
          {shown}
          <i>%</i>
        </span>
        {label && size >= 70 && <span className="ring-label">{label}</span>}
      </div>
    </div>
  );
}
