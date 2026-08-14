import { NavLink, Outlet, Navigate } from 'react-router-dom';
import { useAuth } from '../../context/AuthContext';
import Brand from '../../components/Brand';
import Loading from '../../components/Loading';
import { nextOnboardingPath, onboardingOf } from '../../lib/onboarding';

const STEPS = [
  ['quiz', 'Quiz', '/onboarding/quiz'],
  ['preferences', 'Preferences', '/onboarding/preferences'],
  ['profile', 'Profile', '/onboarding/profile'],
  ['photos', 'Photos', '/onboarding/photos'],
];

export default function OnboardingLayout() {
  const { user, loading } = useAuth();

  if (loading) return <Loading text="Getting things ready…" />;
  if (!user) return <Navigate to="/login" replace />;

  const o = onboardingOf(user);

  return (
    <div className="onboard-shell">
      <header className="onboard-header">
        <Brand to="/app" />
        <nav className="onboard-steps" aria-label="Onboarding">
          {STEPS.map(([key, label, to], i) => {
            const done =
              (key === 'quiz' && o.quiz_done) ||
              (key === 'preferences' && o.preferences_done) ||
              (key === 'profile' && o.profile_done) ||
              (key === 'photos' && o.photos_done);
            return (
              <NavLink key={key} to={to} className={({ isActive }) => `onboard-step${isActive ? ' is-active' : ''}${done ? ' is-done' : ''}`}>
                <span className="onboard-num">{i + 1}</span>
                {label}
              </NavLink>
            );
          })}
        </nav>
      </header>
      <main className="onboard-main">
        <Outlet />
      </main>
    </div>
  );
}

export function OnboardingIndex() {
  const { user } = useAuth();
  return <Navigate to={nextOnboardingPath(user)} replace />;
}
