import { useEffect, useId, useRef, useState } from 'react';

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
  stroke = 8,
  label = 'match',
  animateOnView = true,
}) {
  const pct = Math.max(0, Math.min(100, Math.round(value)));
  const [revealed, setRevealed] = useState(!animateOnView);
  const ref = useRef(null);
  // Keyed on size, every card in a grid emitted the same id and they all
  // resolved to the first gradient in the document.
  const gradId = useId();

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

  const tone = pct >= 80 ? 'var(--green)' : pct >= 60 ? 'var(--violet)' : 'var(--pink)';

  return (
    <div ref={ref} className="ring" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <defs>
          <linearGradient id={gradId} x1="0%" y1="0%" x2="100%" y2="100%">
            <stop offset="0%" stopColor="#ff4d7d" />
            <stop offset="55%" stopColor="#b14bff" />
            <stop offset="100%" stopColor="#6366f1" />
          </linearGradient>
        </defs>
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke="rgba(255,255,255,0.08)"
          strokeWidth={stroke}
        />
        <circle
          cx={size / 2}
          cy={size / 2}
          r={r}
          fill="none"
          stroke={`url(#${gradId})`}
          strokeWidth={stroke}
          strokeLinecap="round"
          strokeDasharray={c}
          strokeDashoffset={offset}
          transform={`rotate(-90 ${size / 2} ${size / 2})`}
          style={{ transition: 'stroke-dashoffset 1.2s cubic-bezier(0.22,1,0.36,1)' }}
        />
      </svg>
      <div className="ring-center">
        <span className="ring-value" style={{ color: tone }}>
          {shown}
          <i>%</i>
        </span>
        <span className="ring-label">{label}</span>
      </div>
    </div>
  );
}
