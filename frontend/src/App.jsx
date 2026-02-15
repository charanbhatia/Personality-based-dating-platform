import { BrowserRouter, Routes, Route, Navigate, useLocation, Link } from 'react-router-dom';
import { AuthProvider, useAuth } from './context/AuthContext';
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

function ProtectedRoute({ children }) {
  const { user, loading } = useAuth();
  if (loading) return <div className="app-shell"><div className="loading-page">Loading...</div></div>;
  if (!user) return <Navigate to="/login" replace />;
  return children;
}

function Layout({ children }) {
  const { user, logout } = useAuth();
  const location = useLocation();
  const isHome = location.pathname === '/';
  const showBack = !isHome && location.pathname !== '/login' && location.pathname !== '/register';

  const backLabel = () => {
    if (location.pathname.startsWith('/conversations/')) return { to: '/conversations', label: 'Back to messages' };
    if (location.pathname.startsWith('/matches/')) return { to: '/matches', label: 'Back to matches' };
    if (location.pathname === '/profile' || location.pathname === '/matches' || location.pathname === '/conversations') return { to: '/', label: 'Back to home' };
    return { to: '/', label: 'Back to home' };
  };

  return (
    <div className="app-shell">
      <header className="app-header">
        <Link to="/" className="brand">Dating Platform</Link>
        <div className="header-right">
          {user && (
            <>
              <span className="user-name">{user.name}</span>
              <button type="button" className="btn-logout" onClick={logout}>Logout</button>
            </>
          )}
        </div>
      </header>
      <main className="app-main">
        {showBack && (
          <div className="back-bar">
            <Link to={backLabel().to} className="btn-back">← {backLabel().label}</Link>
          </div>
        )}
        {children}
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
        <AppRoutes />
      </AuthProvider>
    </BrowserRouter>
  );
}
