// Lightweight inline SVG icon set — stroke inherits currentColor.
// Every icon here is decorative: it either sits beside a text label or inside a
// control that carries its own aria-label, so it is hidden from assistive tech.
const base = {
  width: 24,
  height: 24,
  viewBox: '0 0 24 24',
  fill: 'none',
  stroke: 'currentColor',
  strokeWidth: 1.75,
  strokeLinecap: 'round',
  strokeLinejoin: 'round',
  'aria-hidden': 'true',
  focusable: 'false',
};

export function IconHeart(props) {
  return (
    <svg {...base} {...props}>
      <path d="M12 20.5s-7.5-4.6-9.7-9A5.2 5.2 0 0 1 12 6.2a5.2 5.2 0 0 1 9.7 5.3c-2.2 4.4-9.7 9-9.7 9Z" />
    </svg>
  );
}

export function IconSpark(props) {
  return (
    <svg {...base} {...props}>
      <path d="M12 3v4M12 17v4M3 12h4M17 12h4M6 6l2.5 2.5M15.5 15.5 18 18M18 6l-2.5 2.5M8.5 15.5 6 18" />
    </svg>
  );
}

export function IconUser(props) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="8" r="4" />
      <path d="M4 20c0-3.3 3.6-6 8-6s8 2.7 8 6" />
    </svg>
  );
}

export function IconChat(props) {
  return (
    <svg {...base} {...props}>
      <path d="M4 5h16v11H9l-5 4V5Z" />
      <path d="M8.5 10.5h.01M12 10.5h.01M15.5 10.5h.01" />
    </svg>
  );
}

export function IconCompass(props) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="m15 9-2 5-4 1 2-5 4-1Z" />
    </svg>
  );
}

export function IconSend(props) {
  return (
    <svg {...base} {...props}>
      <path d="M4 12 20 4l-6 16-3-7-7-1Z" />
    </svg>
  );
}

export function IconArrowLeft(props) {
  return (
    <svg {...base} {...props}>
      <path d="M15 5l-7 7 7 7M8 12h11" />
    </svg>
  );
}

export function IconLogout(props) {
  return (
    <svg {...base} {...props}>
      <path d="M9 5H5v14h4M15 8l4 4-4 4M19 12H9" />
    </svg>
  );
}

export function IconPin(props) {
  return (
    <svg {...base} {...props}>
      <path d="M12 21s7-6.3 7-11a7 7 0 1 0-14 0c0 4.7 7 11 7 11Z" />
      <circle cx="12" cy="10" r="2.5" />
    </svg>
  );
}

export function IconAlert(props) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="9" />
      <path d="M12 7.5v5M12 16.2h.01" />
    </svg>
  );
}

export function IconShield(props) {
  return (
    <svg {...base} {...props}>
      <path d="M12 3 5 6v6c0 4.2 3 7.7 7 9 4-1.3 7-4.8 7-9V6l-7-3Z" />
      <path d="m9 12 2 2 4-4" />
    </svg>
  );
}

export function IconBell(props) {
  return (
    <svg {...base} {...props}>
      <path d="M6 9a6 6 0 1 1 12 0c0 7 3 7 3 7H3s3 0 3-7" />
      <path d="M10 19a2 2 0 0 0 4 0" />
    </svg>
  );
}

export function IconCog(props) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="3" />
      <path d="M12 3v2M12 19v2M3 12h2M19 12h2M5.6 5.6l1.4 1.4M17 17l1.4 1.4M18.4 5.6 17 7M7 17l-1.4 1.4" />
    </svg>
  );
}

export function IconSun(props) {
  return (
    <svg {...base} {...props}>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2M12 20v2M2 12h2M20 12h2M4.9 4.9l1.4 1.4M17.7 17.7l1.4 1.4M19.1 4.9l-1.4 1.4M6.3 17.7l-1.4 1.4" />
    </svg>
  );
}

export function IconMoon(props) {
  return (
    <svg {...base} {...props}>
      <path d="M20 13.4A8.2 8.2 0 0 1 10.6 4a8.4 8.4 0 1 0 9.4 9.4Z" />
    </svg>
  );
}

export function IconMonitor(props) {
  return (
    <svg {...base} {...props}>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M9 20h6M12 16v4" />
    </svg>
  );
}
