import { Link } from 'react-router-dom';
import { IconHeart } from './Icons';

export default function MatchCelebration({ name, photo, to, state, onClose }) {
  return (
    <div className="modal-backdrop" role="presentation" onClick={onClose}>
      <div className="modal match-pop" role="dialog" aria-modal="true" onClick={(e) => e.stopPropagation()}>
        <span className="empty-icon"><IconHeart /></span>
        <h2>It&apos;s a match</h2>
        <p>You and {name} liked each other.</p>
        {photo && <img src={photo} alt="" className="match-pop-photo" />}
        <div className="match-actions">
          <button type="button" className="btn btn-ghost" onClick={onClose}>
            Keep browsing
          </button>
          <Link to={to} state={state} className="btn btn-primary" onClick={onClose}>
            Send a message
          </Link>
        </div>
      </div>
    </div>
  );
}
