import { useState, useEffect } from 'react';
import { useParams, Link } from 'react-router-dom';
import { matches as matchesApi } from '../api';

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

  if (loading) return <div className="loading-page">Loading...</div>;
  if (!match) return <div className="page"><p>Profile not found.</p></div>;

  return (
    <div className="page">
      <div className="match-detail">
        {match.photo_url && <img src={match.photo_url} alt="" />}
        <h1>{match.name}</h1>
        <p className="score">Compatibility: {Math.round((match.score || 0) * 100)}%</p>
        <p>{match.gender} {match.location && ` · ${match.location}`}</p>
        {match.bio && <p className="bio">{match.bio}</p>}
        <Link to={`/conversations/start/${match.user_id}`} className="btn-message">Send message</Link>
      </div>
    </div>
  );
}
