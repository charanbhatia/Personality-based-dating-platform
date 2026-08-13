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
import BackgroundFX from './components/BackgroundFX';
import Brand from './components/Brand';
import Loading from './components/Loading';
import ErrorBoundary from './components/ErrorBoundary';
import {
  IconUser,
  IconHeart,
  IconCompass,
  IconChat,
  IconLogout,
  IconArrowLeft,
} from './components/Icons';
import Login from './pages/Login';
import Dashboard from './pages/Dashboard';
import './App.css';

// Login and Dashboard are the two first-paint entry points, so they stay eager.
// Everything else is only reachable by navigation and can arrive on demand.
const Register = lazy(() => import('./pages/Register'));
const Profile = lazy(() => import('./pages/Profile'));
const Matches = lazy(() => import('./pages/Matches'));
const MatchDetail = lazy(() => import('./pages/MatchDetail'));
const Conversations = lazy(() => import('./pages/Conversations'));
const Chat = lazy(() => import('./pages/Chat'));
const StartConversation = lazy(() => import('./pages/StartConversation'));

function PublicOnly({ children }) {
  const { user, loading } = useAuth();
  if (loading) return <Loading text="Getting things ready…" />;
  if (user) return <Navigate to="/" replace />;
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

function backTarget(pathname) {
  if (pathname.startsWith('/conversations/')) return { to: '/conversations', label: 'Back to messages' };
  if (pathname.startsWith('/matches/')) return { to: '/matches', label: 'Back to matches' };
  return { to: '/', label: 'Back to home' };
}

function Layout() {
  const { user, logout } = useAuth();
  const location = useLocation();
  const mainRef = useRef(null);
  const showBack = location.pathname !== '/';
  const back = backTarget(location.pathname);

  // A client-side navigation leaves focus wherever it was, so keyboard and
  // screen-reader users stay stranded in the old page's tab order.
  useEffect(() => {
    mainRef.current?.focus();
  }, [location.pathname]);

  return (
    <div className="app-shell">
      <header className="app-header">
        <Brand />
        <nav className="header-nav">
          <NavLink to="/" end className="nav-item">
            <IconCompass />
            <span>Home</span>
          </NavLink>
          <NavLink to="/matches" className="nav-item">
            <IconHeart />
            <span>Matches</span>
          </NavLink>
          <NavLink to="/conversations" className="nav-item">
            <IconChat />
            <span>Messages</span>
          </NavLink>
          <NavLink to="/profile" className="nav-item">
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
        {showBack && (
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
  // The boundary sits above this Suspense too: /register is the one lazy route
  // outside Layout, and a failed chunk fetch there would otherwise white-screen
  // the whole app.
  return (
    <ErrorBoundary>
      <Suspense fallback={<Loading text="Getting things ready…" />}>
        <Routes>
          <Route path="/login" element={<PublicOnly><Login /></PublicOnly>} />
          <Route path="/register" element={<PublicOnly><Register /></PublicOnly>} />
          <Route element={<ProtectedLayout />}>
            <Route path="/" element={<Dashboard />} />
            <Route path="/profile" element={<Profile />} />
            <Route path="/matches" element={<Matches />} />
            <Route path="/matches/:id" element={<MatchDetail />} />
            <Route path="/conversations" element={<Conversations />} />
            <Route path="/conversations/start/:userId" element={<StartConversation />} />
            <Route path="/conversations/:id" element={<Chat />} />
          </Route>
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
        <BackgroundFX />
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  );
}
