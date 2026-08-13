import {
  BrowserRouter,
  Routes,
  Route,
  Navigate,
  useLocation,
  Link,
  NavLink,
} from 'react-router-dom';
import { AuthProvider, useAuth } from './context/AuthContext';
import BackgroundFX from './components/BackgroundFX';
import Loading from './components/Loading';
import {
  IconHeart,
  IconUser,
  IconCompass,
  IconChat,
  IconLogout,
  IconArrowLeft,
} from './components/Icons';
import Login from './pages/Login';
import Register from './pages/Register';
import Dashboard from './pages/Dashboard';
import Profile from './pages/Profile';
import Matches from './pages/Matches';
import MatchDetail from './pages/MatchDetail';
import Conversations from './pages/Conversations';
import Chat from './pages/Chat';
import StartConversation from './pages/StartConversation';
import './App.css';

function Brand() {
  return (
    <Link to="/" className="brand">
      <span className="brand-mark">
        <IconHeart />
      </span>
      <span className="brand-name">
        <b>Kin</b>
        <span>dred</span>
      </span>
    </Link>
  );
}

function ProtectedRoute({ children }) {
  const { user, loading } = useAuth();
  if (loading)
    return (
      <div className="app-shell">
        <Loading text="Getting things ready…" />
      </div>
    );
  if (!user) return <Navigate to="/login" replace />;
  return children;
}

function Layout({ children }) {
  const { user, logout } = useAuth();
  const location = useLocation();
  const isHome = location.pathname === '/';
  const showBack = !isHome;

  const backTarget = () => {
    if (location.pathname.startsWith('/conversations/')) return { to: '/conversations', label: 'Back to messages' };
    if (location.pathname.startsWith('/matches/')) return { to: '/matches', label: 'Back to matches' };
    return { to: '/', label: 'Back to home' };
  };

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
              <button type="button" className="btn-logout" onClick={logout} title="Log out">
                <IconLogout />
              </button>
            </>
          )}
        </div>
      </header>
      <main className="app-main">
        {showBack && (
          <div className="back-bar">
            <Link to={backTarget().to} className="btn-back">
              <IconArrowLeft /> {backTarget().label}
            </Link>
          </div>
        )}
        <div key={location.pathname} className="route-fade">
          {children}
        </div>
      </main>
    </div>
  );
}

function AppRoutes() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/register" element={<Register />} />
      <Route
        path="/"
        element={
          <ProtectedRoute>
            <Layout><Dashboard /></Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/profile"
        element={
          <ProtectedRoute>
            <Layout><Profile /></Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/matches"
        element={
          <ProtectedRoute>
            <Layout><Matches /></Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/matches/:id"
        element={
          <ProtectedRoute>
            <Layout><MatchDetail /></Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/conversations"
        element={
          <ProtectedRoute>
            <Layout><Conversations /></Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/conversations/start/:userId"
        element={
          <ProtectedRoute>
            <Layout><StartConversation /></Layout>
          </ProtectedRoute>
        }
      />
      <Route
        path="/conversations/:id"
        element={
          <ProtectedRoute>
            <Layout><Chat /></Layout>
          </ProtectedRoute>
        }
      />
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
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
