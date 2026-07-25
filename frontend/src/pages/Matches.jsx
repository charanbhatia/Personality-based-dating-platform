import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { matches as matchesApi } from '../api';
import TiltCard from '../components/TiltCard';
import CompatibilityRing from '../components/CompatibilityRing';
import Avatar from '../components/Avatar';
import { IconHeart, IconChat, IconPin, IconUser, IconSpark } from '../components/Icons';

export default function Matches() {
  const [list, setList] = useState([]);
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    matchesApi
      .list()
      .then((res) => setList(res.data.matches || []))
      .catch(() => setList([]))
      .finally(() => setLoading(false));
  }, []);

  if (loading)
    return (
      <div className="loading-page">
        <div className="spinner" />
        <p>Finding your people…</p>
      </div>
    );

  return (
    <div className="page">
      <span className="section-kicker">
        <IconSpark /> Curated for you
      </span>
      <h1>Your matches</h1>
      <p>People whose personality and preferences align with yours.</p>

      {list.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconHeart /></span>
          <h3>No matches yet</h3>
          <p>Complete your profile to help us find people you'll genuinely click with.</p>
          <Link to="/profile" className="btn btn-primary">Complete profile</Link>
        </div>
      ) : (
        <div className="match-grid stagger">
          {list.map((m) => {
            const score = Math.round((m.score || 0) * 100);
            return (
              <TiltCard key={m.user_id} max={8}>
                <article className="match-card">
                  <div className="match-media">
                    <span className="match-ring-badge">
                      <CompatibilityRing value={score} size={64} stroke={6} />
                    </span>
                    {m.photo_url ? (
                      <img src={m.photo_url} alt={m.name} loading="lazy" />
                    ) : (
                      <div className="match-media-fallback">
                        <Avatar name={m.name} seed={m.user_id} size={96} />
                      </div>
                    )}
                    <h3 className="match-name">{m.name}</h3>
                  </div>
                  <div className="match-body">
                    <div className="match-meta">
                      {m.gender && (
                        <span className="chip"><IconUser /> {m.gender}</span>
                      )}
                      {m.location && (
                        <span className="chip"><IconPin /> {m.location}</span>
                      )}
                    </div>
                    {m.bio && <p className="match-bio">{m.bio}</p>}
                    <div className="match-actions">
                      <Link to={`/matches/${m.user_id}`} className="btn btn-ghost">
                        View
                      </Link>
                      <Link to={`/conversations/start/${m.user_id}`} className="btn btn-primary">
                        <IconChat /> Message
                      </Link>
                    </div>
                  </div>
                </article>
              </TiltCard>
            );
          })}
        </div>
      )}
    </div>
  );
}
