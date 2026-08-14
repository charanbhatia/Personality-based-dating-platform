import { useRef, useCallback, useEffect } from 'react';

/**
 * 3D tilt wrapper. Tracks the pointer and applies a perspective rotation.
 *
 * Whether it runs at all is decided in CSS (`@media (hover: hover) and
 * (prefers-reduced-motion: no-preference)`), not here: matchMedia read once
 * during render goes stale the moment someone toggles reduced motion or picks
 * up a touchscreen, and it left the CSS hover states running on touch anyway.
 */
export default function TiltCard({ children, className = '', max = 7, style, ...rest }) {
  const ref = useRef(null);
  const frame = useRef(0);

  useEffect(() => () => cancelAnimationFrame(frame.current), []);

  const handleMove = useCallback(
    (e) => {
      const el = ref.current;
      if (!el) return;
      cancelAnimationFrame(frame.current);
      frame.current = requestAnimationFrame(() => {
        const rect = el.getBoundingClientRect();
        const px = (e.clientX - rect.left) / rect.width;
        const py = (e.clientY - rect.top) / rect.height;
        el.style.setProperty('--rx', `${((0.5 - py) * max * 2).toFixed(2)}deg`);
        el.style.setProperty('--ry', `${((px - 0.5) * max * 2).toFixed(2)}deg`);
      });
    },
    [max]
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
      className={`tilt ${className}`}
      onMouseMove={handleMove}
      onMouseLeave={reset}
      style={style}
      {...rest}
    >
      <div className="tilt-inner">{children}</div>
    </div>
  );
}
