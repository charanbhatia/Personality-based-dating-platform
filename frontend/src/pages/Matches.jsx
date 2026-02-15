import { useState, useEffect } from 'react';
import { Link } from 'react-router-dom';
import { matches as matchesApi } from '../api';

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

  if (loading) return <div className="loading-page">Loading matches...</div>;

  return (
    <div className="page">
      <h1>Matches</h1>
      <p>People who match your personality and preferences.</p>
      <div className="match-grid">
        {list.length === 0 ? (
          <p>No matches yet. Complete your profile to get better recommendations.</p>
        ) : (
          list.map((m) => (
            <div key={m.user_id} className="match-card">
              {m.photo_url && <img src={m.photo_url} alt="" />}
              <h3>{m.name}</h3>
              <p className="meta">{m.gender} {m.location && ` · ${m.location}`}</p>
              <p className="score">Compatibility: {Math.round((m.score || 0) * 100)}%</p>
              {m.bio && <p className="bio">{m.bio}</p>}
              <div className="match-card-actions">
                <Link to={`/matches/${m.user_id}`}>View profile</Link>
                <Link to={`/conversations/start/${m.user_id}`}>Message</Link>
              </div>
            </div>
          ))
        )}
      </div>
    </div>
  );
}
