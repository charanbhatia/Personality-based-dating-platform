import { useEffect, useState } from 'react';
import { THEMES, applyTheme, resolveTheme, setTheme, storedTheme, watchSystemTheme } from '../lib/theme';
import { IconSun, IconMoon, IconMonitor } from './Icons';

const LABELS = { system: 'Match system', light: 'Light', dark: 'Dark' };
const ICONS = { system: IconMonitor, light: IconSun, dark: IconMoon };

export default function ThemeToggle() {
  const [theme, setThemeState] = useState(storedTheme);
  // Re-render when the OS flips while we are following it, so the icon stays honest.
  const [, force] = useState(0);

  useEffect(() => {
    applyTheme(theme);
    return watchSystemTheme(() => force((n) => n + 1));
  }, [theme]);

  function cycle() {
    const next = THEMES[(THEMES.indexOf(theme) + 1) % THEMES.length];
    setThemeState(setTheme(next));
  }

  const Icon = ICONS[theme];
  const resolved = resolveTheme(theme);

  return (
    <button
      type="button"
      className="theme-toggle"
      onClick={cycle}
      title={`Theme: ${LABELS[theme]}${theme === 'system' ? ` (${resolved})` : ''}`}
      aria-label={`Theme: ${LABELS[theme]}. Activate to change.`}
    >
      <Icon />
    </button>
  );
}
