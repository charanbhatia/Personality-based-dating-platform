import { useRef, useCallback } from 'react';

/**
 * 3D tilt wrapper. Tracks the pointer and applies a perspective rotation plus a
 * light "glare" that follows the cursor. Falls back gracefully (no motion) for
 * users who prefer reduced motion, and is disabled on touch/coarse pointers.
 */
export default function TiltCard({
  children,
  className = '',
  max = 10,
  glare = true,
  style,
  ...rest
}) {
  const ref = useRef(null);
  const frame = useRef(0);

  const prefersReduced =
    typeof window !== 'undefined' &&
    window.matchMedia?.('(prefers-reduced-motion: reduce)').matches;
  const coarsePointer =
    typeof window !== 'undefined' && window.matchMedia?.('(pointer: coarse)').matches;
  const enabled = !prefersReduced && !coarsePointer;

  const handleMove = useCallback(
    (e) => {
      if (!enabled) return;
      const el = ref.current;
      if (!el) return;
      cancelAnimationFrame(frame.current);
      frame.current = requestAnimationFrame(() => {
        const rect = el.getBoundingClientRect();
        const px = (e.clientX - rect.left) / rect.width;
        const py = (e.clientY - rect.top) / rect.height;
        const rx = (0.5 - py) * max * 2;
        const ry = (px - 0.5) * max * 2;
        el.style.setProperty('--rx', `${rx.toFixed(2)}deg`);
        el.style.setProperty('--ry', `${ry.toFixed(2)}deg`);
        el.style.setProperty('--mx', `${(px * 100).toFixed(1)}%`);
        el.style.setProperty('--my', `${(py * 100).toFixed(1)}%`);
      });
    },
    [enabled, max]
  );

  const reset = useCallback(() => {
    const el = ref.current;
    if (!el) return;
    cancelAnimationFrame(frame.current);
    el.style.setProperty('--rx', '0deg');
    el.style.setProperty('--ry', '0deg');
  }, []);

  return (
    <div
      ref={ref}
      className={`tilt ${enabled ? 'tilt--on' : ''} ${className}`}
      onMouseMove={handleMove}
      onMouseLeave={reset}
      style={style}
      {...rest}
    >
      <div className="tilt-inner">
        {children}
        {glare && enabled && <span className="tilt-glare" />}
      </div>
    </div>
  );
}
