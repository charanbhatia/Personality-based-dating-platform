// Theme has three states, not two: light, dark, and following the system.
// Light is the default — it is the palette the product was designed in, so the
// app does not change appearance on a device that happens to be set to dark.
// The other two are opt-in through the toggle.
const KEY = 'theme';
export const DEFAULT_THEME = 'light';
export const THEMES = ['light', 'dark', 'system'];

export function storedTheme() {
  try {
    const v = localStorage.getItem(KEY);
    return THEMES.includes(v) ? v : DEFAULT_THEME;
  } catch {
    // Private browsing can throw on access; the default is still usable.
    return DEFAULT_THEME;
  }
}

export function systemPrefersDark() {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ?? false;
}

export function resolveTheme(theme) {
  return theme === 'system' ? (systemPrefersDark() ? 'dark' : 'light') : theme;
}

// The stylesheet keys off data-theme; "system" removes the attribute so the
// prefers-color-scheme media query takes over.
export function applyTheme(theme) {
  const root = document.documentElement;
  if (theme === 'system') root.removeAttribute('data-theme');
  else root.setAttribute('data-theme', theme);

  // Keep the browser chrome (address bar, notch area) in step with the page.
  const meta = document.querySelector('meta[name="theme-color"]');
  if (meta) meta.setAttribute('content', resolveTheme(theme) === 'dark' ? '#16110f' : '#fff8f4');
}

export function setTheme(theme) {
  const next = THEMES.includes(theme) ? theme : 'system';
  try {
    localStorage.setItem(KEY, next);
  } catch {
    // Persistence is a nicety; applying it still works for this session.
  }
  applyTheme(next);
  return next;
}

// While on "system", track the OS switching so the page changes with it.
export function watchSystemTheme(onChange) {
  const mq = window.matchMedia?.('(prefers-color-scheme: dark)');
  if (!mq) return () => {};
  const handler = () => {
    if (storedTheme() === 'system') {
      applyTheme('system');
      onChange?.();
    }
  };
  mq.addEventListener('change', handler);
  return () => mq.removeEventListener('change', handler);
}
