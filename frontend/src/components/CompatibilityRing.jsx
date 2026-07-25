import { useEffect, useRef, useState } from 'react';

/**
 * Animated circular compatibility gauge. The stroke sweeps from 0 to the given
 * percentage the first time it scrolls into view.
 */
export default function CompatibilityRing({ value = 0, size = 92, stroke = 8, label = 'match' }) {
  const pct = Math.max(0, Math.min(100, Math.round(value)));
  const [shown, setShown] = useState(0);
  const ref = useRef(null);

  const r = (size - stroke) / 2;
  const c = 2 * Math.PI * r;
  const offset = c - (shown / 100) * c;

  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const io = new IntersectionObserver(
      ([entry]) => {
        if (entry.isIntersecting) {
          requestAnimationFrame(() => setShown(pct));
          io.disconnect();
        }
      },
      { threshold: 0.4 }
    );
    io.observe(el);
    return () => io.disconnect();
  }, [pct]);

  const tone = pct >= 80 ? 'var(--green)' : pct >= 60 ? 'var(--violet)' : 'var(--pink)';

  return (
    <div ref={ref} className="ring" style={{ width: size, height: size }}>
      <svg width={size} height={size} viewBox={`0 0 ${size} ${size}`}>
        <defs>
          <linearGradient id={`ring-grad-${size}`} x1="0%" y1="0%" x2="100%" y2="100%">
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
          stroke={`url(#ring-grad-${size})`}
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
