import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';

export default function Dashboard() {
  const { user } = useAuth();

  return (
    <div className="page">
      <h1>Welcome, {user?.name || user?.email}</h1>
      <p>Manage your profile, discover matches, and start conversations.</p>
      <nav className="nav-links">
        <Link to="/profile">My profile</Link>
        <Link to="/matches">Matches</Link>
        <Link to="/conversations">Messages</Link>
      </nav>
    </div>
  );
}
