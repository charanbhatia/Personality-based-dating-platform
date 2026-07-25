import { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import { matches as matchesApi } from '../api';
import CompatibilityRing from '../components/CompatibilityRing';
import Avatar from '../components/Avatar';
import { IconChat, IconPin, IconUser } from '../components/Icons';

export default function MatchDetail() {
  const { id } = useParams();
  const [match, setMatch] = useState(null);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    if (!id) return;
    matchesApi
      .get(id)
      .then((res) => setMatch(res.data))
      .catch(() => setMatch(null))
      .finally(() => setLoading(false));
  }, [id]);

  if (loading)
    return (
      <div className="loading-page">
        <div className="spinner" />
        <p>Loading profile…</p>
      </div>
    );
  if (!match)
    return (
      <div className="page">
        <div className="empty-state">
          <span className="empty-icon"><IconUser /></span>
          <h3>Profile not found</h3>
          <p>This person may no longer be available.</p>
          <Link to="/matches" className="btn btn-ghost">Back to matches</Link>
        </div>
      </div>
    );

  const score = Math.round((match.score || 0) * 100);

  return (
    <div className="page">
      <div className="match-detail">
        <div className="match-detail-media">
          {match.photo_url ? (
            <img src={match.photo_url} alt={match.name} />
          ) : (
            <div className="match-media-fallback">
              <Avatar name={match.name} seed={match.user_id} size={140} />
            </div>
          )}
        </div>

        <div className="match-detail-info">
          <h1>{match.name}</h1>
          <div className="match-meta">
            {match.gender && <span className="chip"><IconUser /> {match.gender}</span>}
            {match.location && <span className="chip"><IconPin /> {match.location}</span>}
          </div>

          <div className="md-score">
            <CompatibilityRing value={score} size={84} stroke={8} />
            <div className="md-score-text">
              <b>{score}% compatible</b>
              <span>Based on your shared personality traits and values</span>
            </div>
          </div>

          {match.bio && <p className="bio">{match.bio}</p>}

          <Link to={`/conversations/start/${match.user_id}`} className="btn btn-primary">
            <IconChat /> Send a message
          </Link>
        </div>
      </div>
    </div>
  );
}
