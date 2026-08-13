import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';

// A client-rendered app keeps whatever <title> index.html shipped with, so every
// tab, bookmark and history entry reads the same. Set it per route instead, and
// keep a self-referential canonical so query strings don't fragment the URL.
const TITLES = [
  [/^\/login$/, 'Log in'],
  [/^\/register$/, 'Create your account'],
  [/^\/profile$/, 'Your profile'],
  [/^\/matches$/, 'Your matches'],
  [/^\/matches\/.+/, 'Match profile'],
  [/^\/conversations$/, 'Messages'],
  [/^\/conversations\/.+/, 'Chat'],
];

const FALLBACK = 'Kindred · Meet minds, not just faces';

export default function useRouteMeta() {
  const { pathname } = useLocation();

  useEffect(() => {
    const hit = TITLES.find(([pattern]) => pattern.test(pathname));
    document.title = hit ? `${hit[1]} · Kindred` : FALLBACK;

    let link = document.head.querySelector('link[rel="canonical"]');
    if (!link) {
      link = document.createElement('link');
      link.rel = 'canonical';
      document.head.appendChild(link);
    }
    link.href = new URL(pathname, window.location.origin).href;
  }, [pathname]);
}
