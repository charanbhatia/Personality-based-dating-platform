import { useParams, useLocation, Link } from 'react-router-dom';
import { matches as matchesApi } from '../api';
import useFetch from '../hooks/useFetch';
import { band, coarse, isScored, ordinal } from '../lib/compat';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconChat, IconPin, IconUser } from '../components/Icons';

export default function MatchDetail() {
  const { id } = useParams();
  // Rank is meaningless on its own — it only exists relative to the list this
  // page was opened from, so it travels with the link rather than being refetched.
  const { rank, total, scores } = useLocation().state || {};
  const { data: match, error, loading, retry } = useFetch(() => matchesApi.get(id), [id]);

  if (loading) return <Loading text="Loading profile…" />;

  // 404 means this person is gone and 400 means the id isn't even a UUID —
  // both are permanent, so offer a way out rather than a retry that can't win.
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
          <ErrorState text="We couldn't load this profile right now." onRetry={retry} />
        )}
      </div>
    );

  const scored = isScored(match.score);
  const spread = Array.isArray(scores) && scores.length > 1 ? scores : null;

  return (
    <div className="page md">
      <header className="md-head">
        <Avatar
          name={match.name}
          src={match.photo_url || undefined}
          seed={match.user_id}
          size={72}
        />
        <div>
          <h1>{match.name}</h1>
          <div className="match-meta">
            {match.gender && <span className="chip"><IconUser /> {match.gender}</span>}
            {match.location && <span className="chip"><IconPin /> {match.location}</span>}
          </div>
        </div>
      </header>

      {match.bio && <p className="md-bio">{match.bio}</p>}

      <section className="md-score">
        <b className={scored ? '' : 'is-unscored'}>{band(match.score)}</b>
        {scored && (
          <span className="md-score-sub">
            {rank ? `${ordinal(rank)} closest of ${total} · ` : ''}
            overlap {coarse(match.score)} of 100, rounded to fives
          </span>
        )}

        {scored && spread && (
          <figure className="md-spread">
            <div className="md-spread-line" aria-hidden="true">
              {spread.map((s, i) => {
                const min = Math.min(...spread);
                const max = Math.max(...spread);
                const at = max === min ? 50 : ((s - min) / (max - min)) * 100;
                return (
                  <span
                    key={i}
                    className={`md-tick ${s === match.score ? 'is-this' : ''}`}
                    style={{ left: `${at}%` }}
                  />
                );
              })}
            </div>
            <figcaption>
              Where this person sits among your matches — furthest {coarse(Math.min(...spread))},
              closest {coarse(Math.max(...spread))}.
            </figcaption>
          </figure>
        )}

        <p className="md-method">
          {scored
            ? 'This is the average agreement across the Big Five traits you have both answered — one minus the gap on each, averaged. It measures how similarly you are wired. It does not predict whether you will like each other.'
            : 'Neither of you has answered enough of the personality assessment for a score to mean anything, so we are not showing one.'}
        </p>
      </section>

      <Link
        to={`/app/conversations/start/${match.user_id}`}
        state={{ name: match.name, bio: match.bio }}
        className="btn btn-primary"
      >
        <IconChat /> Send a message
      </Link>
    </div>
  );
}
