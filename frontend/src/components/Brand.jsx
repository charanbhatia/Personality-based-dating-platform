import { Link } from 'react-router-dom';
import { IconHeart } from './Icons';

// `to` differs by context: inside the app the mark goes home to /app, on the
// public pages it goes to the landing page.
export default function Brand({ to = '/app' }) {
  return (
    <Link to={to} className="brand">
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
