import { lazy, Suspense, useEffect, useRef } from 'react';
import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  Outlet,
  useLocation,
  Link,
  NavLink,
} from 'react-router-dom';
import { QueryClientProvider } from '@tanstack/react-query';
import { queryClient } from './api';
import { AuthProvider, useAuth } from './context/AuthContext';
import { ToastProvider } from './context/ToastContext';
import Brand from './components/Brand';
import Loading from './components/Loading';
import ErrorBoundary from './components/ErrorBoundary';
import NotificationBell from './components/NotificationBell';
import ThemeToggle from './components/ThemeToggle';
import OfflineBanner from './components/OfflineBanner';
import useRouteMeta from './hooks/useRouteMeta';
import { needsOnboarding, nextOnboardingPath, afterAuthPath } from './lib/onboarding';
import {
  IconUser,
  IconCompass,
  IconHeart,
  IconChat,
  IconLogout,
  IconArrowLeft,
  IconCog,
} from './components/Icons';
import Landing from './pages/Landing';
import Login from './pages/Login';
import Dashboard from './pages/Dashboard';
import './App.css';

const Register = lazy(() => import('./pages/Register'));
const ForgotPassword = lazy(() => import('./pages/ForgotPassword'));
const ResetPassword = lazy(() => import('./pages/ResetPassword'));
const VerifyEmail = lazy(() => import('./pages/VerifyEmail'));
const Profile = lazy(() => import('./pages/Profile'));
const Matches = lazy(() => import('./pages/Matches'));
const MatchDetail = lazy(() => import('./pages/MatchDetail'));
const Conversations = lazy(() => import('./pages/Conversations'));
const Chat = lazy(() => import('./pages/Chat'));
const StartConversation = lazy(() => import('./pages/StartConversation'));
const Notifications = lazy(() => import('./pages/Notifications'));
const Settings = lazy(() => import('./pages/Settings'));
const OnboardingLayout = lazy(() => import('./pages/onboarding/OnboardingLayout'));
const OnboardingIndex = lazy(() =>
  import('./pages/onboarding/OnboardingLayout').then((m) => ({ default: m.OnboardingIndex }))
);
const Quiz = lazy(() => import('./pages/onboarding/Quiz'));
const Preferences = lazy(() => import('./pages/onboarding/Preferences'));
const ProfileSetup = lazy(() => import('./pages/onboarding/ProfileSetup'));
const Photos = lazy(() => import('./pages/onboarding/Photos'));

const LEGACY = [
  ['/profile', '/app/profile'],
  ['/matches', '/app/matches'],
  ['/matches/:id', '/app/matches/:id'],
  ['/conversations', '/app/conversations'],
  ['/conversations/start/:userId', '/app/conversations/start/:userId'],
  ['/conversations/:id', '/app/conversations/:id'],
];

function LegacyRedirect() {
  const { pathname, search } = useLocation();
  return <Navigate to={`/app${pathname}${search}`} replace />;
}

function PublicOnly({ children }) {
  const { user, loading } = useAuth();
  if (loading) return <Loading text="Getting things ready…" />;
  if (user) return <Navigate to={afterAuthPath(user)} replace />;
  return children;
}

function ProtectedLayout() {
  const { user, loading } = useAuth();
  const location = useLocation();
  if (loading)
    return (
      <div className="app-shell">
        <Loading text="Getting things ready…" />
      </div>
    );
  if (!user) return <Navigate to="/login" replace />;
  if (needsOnboarding(user) && !location.pathname.startsWith('/onboarding')) {
    return <Navigate to={nextOnboardingPath(user)} replace />;
  }
  return <Layout />;
}

function backTarget(pathname) {
  if (pathname.startsWith('/app/conversations/'))
    return { to: '/app/conversations', label: 'Back to messages' };
  if (pathname.startsWith('/app/matches/')) return { to: '/app/matches', label: 'Back to matches' };
  if (pathname === '/app') return null;
  return { to: '/app', label: 'Back to home' };
}

function Layout() {
  const { user, logout } = useAuth();
  const location = useLocation();
  const mainRef = useRef(null);
  const back = backTarget(location.pathname);

  useEffect(() => {
    mainRef.current?.focus();
  }, [location.pathname]);

  return (
    <div className="app-shell">
      <a className="skip-link" href="#main-content">Skip to content</a>
      <OfflineBanner />
      <header className="app-header">
        <Brand to="/app" />
        <nav className="header-nav">
          <NavLink to="/app" end className="nav-item">
            <IconCompass />
            <span>Home</span>
          </NavLink>
          <NavLink to="/app/matches" className="nav-item">
            <IconHeart />
            <span>Matches</span>
          </NavLink>
          <NavLink to="/app/conversations" className="nav-item">
            <IconChat />
            <span>Messages</span>
          </NavLink>
          <NavLink to="/app/profile" className="nav-item">
            <IconUser />
            <span>Profile</span>
          </NavLink>
        </nav>
        <div className="header-right">
          <ThemeToggle />
          {user && (
            <>
              <NotificationBell />
              <NavLink to="/app/settings" className="nav-item" aria-label="Settings">
                <IconCog />
              </NavLink>
              <span className="user-chip">
                <span className="user-name">{user.name || user.email}</span>
              </span>
              <button
                type="button"
                className="btn-logout"
                onClick={logout}
                aria-label="Log out"
                title="Log out"
              >
                <IconLogout />
              </button>
            </>
          )}
        </div>
      </header>
      <main id="main-content" className="app-main" ref={mainRef} tabIndex={-1}>
        {back && (
          <div className="back-bar">
            <Link to={back.to} className="btn-back">
              <IconArrowLeft /> {back.label}
            </Link>
          </div>
        )}
        <div key={location.pathname} className="route-fade">
          <ErrorBoundary>
            <Suspense fallback={<Loading />}>
              <Outlet />
            </Suspense>
          </ErrorBoundary>
        </div>
      </main>
    </div>
  );
}

function AppRoutes() {
  useRouteMeta();
  return (
    <ErrorBoundary>
      <Suspense fallback={<Loading text="Getting things ready…" />}>
        <Routes>
          <Route path="/" element={<Landing />} />
          <Route path="/login" element={<PublicOnly><Login /></PublicOnly>} />
          <Route path="/register" element={<PublicOnly><Register /></PublicOnly>} />
          <Route path="/forgot-password" element={<PublicOnly><ForgotPassword /></PublicOnly>} />
          <Route path="/reset-password" element={<ResetPassword />} />
          <Route path="/verify-email" element={<VerifyEmail />} />

          <Route path="/onboarding" element={<OnboardingLayout />}>
            <Route index element={<OnboardingIndex />} />
            <Route path="quiz" element={<Quiz />} />
            <Route path="preferences" element={<Preferences />} />
            <Route path="profile" element={<ProfileSetup />} />
            <Route path="photos" element={<Photos />} />
          </Route>

          <Route path="/app" element={<ProtectedLayout />}>
            <Route index element={<Dashboard />} />
            <Route path="profile" element={<Profile />} />
            <Route path="matches" element={<Matches />} />
            <Route path="matches/:id" element={<MatchDetail />} />
            <Route path="conversations" element={<Conversations />} />
            <Route path="conversations/start/:userId" element={<StartConversation />} />
            <Route path="conversations/:id" element={<Chat />} />
            <Route path="notifications" element={<Notifications />} />
            <Route path="settings" element={<Settings />} />
          </Route>

          {LEGACY.map(([from]) => (
            <Route key={from} path={from} element={<LegacyRedirect />} />
          ))}

          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </Suspense>
    </ErrorBoundary>
  );
}

export default function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <AuthProvider>
          <ToastProvider>
            <AppRoutes />
          </ToastProvider>
        </AuthProvider>
      </BrowserRouter>
    </QueryClientProvider>
  );
}
