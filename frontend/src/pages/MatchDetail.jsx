import { useState } from 'react';
import { useParams, Link, useLocation } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { matches as matchesApi, likes as likesApi, qk } from '../api';
import { scoreOf, photoOf } from '../lib/compat';
import { topTraits, traitPercent } from '../lib/traits';
import CompatibilityRing from '../components/CompatibilityRing';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import MatchCelebration from '../components/MatchCelebration';
import SafetyActions from '../components/SafetyActions';
import { IconChat, IconPin, IconUser, IconHeart } from '../components/Icons';

export default function MatchDetail() {
  const { id } = useParams();
  const qc = useQueryClient();
  const passed = useLocation().state || {};
  const { data: match, error, isPending, refetch } = useQuery({
    queryKey: qk.publicUser(id),
    queryFn: () => matchesApi.get(id).then((r) => r.data),
  });
  const [busy, setBusy] = useState(false);
  const [matchId, setMatchId] = useState(passed.matchId || null);
  const [swiped, setSwiped] = useState(null);
  const [swipeError, setSwipeError] = useState('');
  const [blocked, setBlocked] = useState(false);
  const [celebration, setCelebration] = useState(null);

  async function swipe(action) {
    if (busy || !match) return;
    setBusy(true);
    setSwipeError('');
    try {
      const res = await likesApi.swipe(match.user_id, action);
      setSwiped(action);
      qc.invalidateQueries({ queryKey: qk.discover });
      if (res.data?.matched && res.data.match_id) {
        setMatchId(res.data.match_id);
        qc.invalidateQueries({ queryKey: qk.matches });
        setCelebration({
          name: match.name,
          photo: photoOf(match),
          matchId: res.data.match_id,
        });
      }
    } catch (err) {
      setSwipeError(err.message);
    } finally {
      setBusy(false);
    }
  }

  if (isPending) return <Loading text="Loading profile…" />;

  const status = error?.response?.status;
  if (error)
    return (
      <div className="page">
        {status === 404 || status === 400 ? (
          <div className="empty-state">
            <span className="empty-icon"><IconUser /></span>
            <h3>Profile not found</h3>
            <p>This person may no longer be available.</p>
            <Link to="/app/matches" className="btn btn-ghost">Back to matches</Link>
          </div>
        ) : (
          <ErrorState text="We couldn't load this profile right now." onRetry={() => refetch()} />
        )}
      </div>
    );

  const score = Math.round((passed.score ?? scoreOf(match)) * 100);
  const photo = photoOf(match);
  const canMessage = match.is_matched || matchId;
  const traits = topTraits(match.traits || passed.traits);

  return (
    <div className="page">
      <div className="match-detail">
        <div className="match-detail-media">
          {photo ? (
            <img src={photo} alt={match.name || ''} width={640} height={480} />
          ) : (
            <div className="match-media-fallback">
              <Avatar name={match.name} seed={match.user_id} size={150} fill />
            </div>
          )}
        </div>

        <div className="match-detail-info">
          <h1>{match.name}</h1>
          <div className="match-meta">
            {match.age != null && <span className="chip">{match.age}</span>}
            {match.gender && <span className="chip"><IconUser /> {match.gender}</span>}
            {match.location && <span className="chip"><IconPin /> {match.location}</span>}
          </div>
          {traits.length > 0 && (
            <div className="trait-row">
              {traits.map((t) => (
                <span key={t.key} className="chip chip-trait">
                  {t.label} {traitPercent(t.value)}
                </span>
              ))}
            </div>
          )}

          {score > 0 && (
            <div className="md-score">
              <CompatibilityRing value={score} size={84} stroke={8} />
              <div className="md-score-text">
                <b>{score}% compatible</b>
                <span>Based on your shared personality traits and values</span>
              </div>
            </div>
          )}

          {match.bio && <p className="bio">{match.bio}</p>}

          {swipeError && (
            <div className="alert alert-error" role="alert">
              {swipeError}
            </div>
          )}

          {blocked ? (
            <p className="auth-sub">This person is blocked.</p>
          ) : canMessage ? (
            <Link
              to={`/app/conversations/start/${match.user_id}`}
              state={{ name: match.name, bio: match.bio, photo_url: photo, matchId }}
              className="btn btn-primary"
            >
              <IconChat /> Send a message
            </Link>
          ) : swiped === 'pass' ? (
            <p className="auth-sub">Passed. They will not show up in your feed again.</p>
          ) : (
            <div className="match-actions">
              <button type="button" className="btn btn-ghost" disabled={busy} onClick={() => swipe('pass')}>
                Pass
              </button>
              <button type="button" className="btn btn-primary" disabled={busy} onClick={() => swipe('like')}>
                <IconHeart /> Like
              </button>
            </div>
          )}

          {!blocked && (
            <SafetyActions
              userId={match.user_id}
              name={match.name}
              onHidden={() => setBlocked(true)}
            />
          )}
        </div>
      </div>
      {celebration && (
        <MatchCelebration
          name={celebration.name}
          photo={celebration.photo}
          to={`/app/conversations/start/${match.user_id}`}
          state={{ name: match.name, bio: match.bio, photo_url: photo, matchId: celebration.matchId }}
          onClose={() => setCelebration(null)}
        />
      )}
    </div>
  );
}
