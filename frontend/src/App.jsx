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
import { AuthProvider, useAuth } from './context/AuthContext';
import Brand from './components/Brand';
import Loading from './components/Loading';
import ErrorBoundary from './components/ErrorBoundary';
import useRouteMeta from './hooks/useRouteMeta';
import {
  IconUser,
  IconHeart,
  IconChat,
  IconLogout,
  IconArrowLeft,
} from './components/Icons';
import Landing from './pages/Landing';
import Login from './pages/Login';
import './App.css';

// Landing and Login are the first-paint entry points, so they stay eager.
// Everything else is only reachable by navigation and arrives on demand.
const Register = lazy(() => import('./pages/Register'));
const Profile = lazy(() => import('./pages/Profile'));
const Matches = lazy(() => import('./pages/Matches'));
const MatchDetail = lazy(() => import('./pages/MatchDetail'));
const Conversations = lazy(() => import('./pages/Conversations'));
const Chat = lazy(() => import('./pages/Chat'));
const StartConversation = lazy(() => import('./pages/StartConversation'));

// The app moved under /app so that / can be a public page. Old links, bookmarks
// and anything already shared keep working.
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
  if (user) return <Navigate to="/app" replace />;
  return children;
}

function ProtectedLayout() {
  const { user, loading } = useAuth();
  if (loading)
    return (
      <div className="app-shell">
        <Loading text="Getting things ready…" />
      </div>
    );
  if (!user) return <Navigate to="/login" replace />;
  return <Layout />;
}

// Only the nested screens get a back bar; the top-level ones have the nav.
function backTarget(pathname) {
  if (pathname.startsWith('/app/conversations/'))
    return { to: '/app/conversations', label: 'Back to messages' };
  if (pathname.startsWith('/app/matches/')) return { to: '/app/matches', label: 'Back to matches' };
  return null;
}

function Layout() {
  const { user, logout } = useAuth();
  const location = useLocation();
  const mainRef = useRef(null);
  const back = backTarget(location.pathname);

  // A client-side navigation leaves focus wherever it was, so keyboard and
  // screen-reader users stay stranded in the old page's tab order.
  useEffect(() => {
    mainRef.current?.focus();
  }, [location.pathname]);

  return (
    <div className="app-shell">
      <header className="app-header">
        <Brand to="/app" />
        <nav className="header-nav">
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
          {user && (
            <>
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
      <main className="app-main" ref={mainRef} tabIndex={-1}>
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
  // The boundary sits above this Suspense too: /register is the one lazy route
  // outside Layout, and a failed chunk fetch there would otherwise white-screen
  // the whole app.
  return (
    <ErrorBoundary>
      <Suspense fallback={<Loading text="Getting things ready…" />}>
        <Routes>
          <Route path="/" element={<Landing />} />
          <Route path="/login" element={<PublicOnly><Login /></PublicOnly>} />
          <Route path="/register" element={<PublicOnly><Register /></PublicOnly>} />

          <Route path="/app" element={<ProtectedLayout />}>
            <Route index element={<Navigate to="matches" replace />} />
            <Route path="profile" element={<Profile />} />
            <Route path="matches" element={<Matches />} />
            <Route path="matches/:id" element={<MatchDetail />} />
            <Route path="conversations" element={<Conversations />} />
            <Route path="conversations/start/:userId" element={<StartConversation />} />
            <Route path="conversations/:id" element={<Chat />} />
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
    <BrowserRouter>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  );
}
