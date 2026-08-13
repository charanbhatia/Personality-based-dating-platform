import { useState } from 'react';
import { Link } from 'react-router-dom';
import { matches as matchesApi } from '../api';
import useFetch from '../hooks/useFetch';
import { band, isScored, ordinal, rank } from '../lib/compat';
import TiltCard from '../components/TiltCard';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconHeart, IconChat, IconPin, IconUser, IconSpark } from '../components/Icons';

const PAGE = 20;

export default function Matches() {
  const [limit, setLimit] = useState(PAGE);
  const { data, error, loading, retry } = useFetch(() => matchesApi.list({ limit }), [limit]);

  // The API scores candidates but returns them in signup order, so the ranking
  // the product promises has to happen here.
  const { list, scores, ranked } = rank(data?.matches || []);
  const total = list.length;

  if (loading) return <Loading text="Finding your people…" />;

  return (
    <div className="page">
      <span className="section-kicker">
        <IconSpark /> {ranked ? 'Ranked by compatibility' : 'Everyone on Kindred'}
      </span>
      <h1>Your matches</h1>
      <p>
        {ranked
          ? 'Closest first. The score is how similarly you answered the personality assessment — not a prediction that you will like each other.'
          : "These aren't ranked yet — nobody here has enough assessment answers in common with you to tell them apart."}
      </p>

      {error ? (
        <ErrorState text="We couldn't load your matches right now." onRetry={retry} />
      ) : total === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconHeart /></span>
          <h3>Nobody here yet</h3>
          <p>Kindred is small on purpose. When more people join, they will show up here.</p>
        </div>
      ) : (
        <>
          <div className="match-grid stagger">
            {list.map((m, i) => (
              <TiltCard key={m.user_id}>
                <article className="match-card">
                  <Link
                    to={`/app/matches/${m.user_id}`}
                    state={{ rank: i + 1, total, scores }}
                    className="match-head"
                  >
                    <Avatar name={m.name} src={m.photo_url || undefined} seed={m.user_id} size={52} />
                    <h3 className="match-name">{m.name}</h3>
                  </Link>

                  <div className="match-score">
                    <b className={isScored(m.score) ? '' : 'is-unscored'}>{band(m.score)}</b>
                    {ranked && isScored(m.score) && (
                      <span>
                        {ordinal(i + 1)} closest of {total}
                      </span>
                    )}
                  </div>

                  {m.bio && <p className="match-bio">{m.bio}</p>}

                  <div className="match-meta">
                    {m.gender && <span className="chip"><IconUser /> {m.gender}</span>}
                    {m.location && <span className="chip"><IconPin /> {m.location}</span>}
                  </div>

                  <Link
                    to={`/app/conversations/start/${m.user_id}`}
                    state={{ name: m.name, bio: m.bio }}
                    className="btn btn-primary match-cta"
                  >
                    <IconChat /> Message
                  </Link>
                </article>
              </TiltCard>
            ))}
          </div>

          <div className="match-more">
            <span>Showing {total}</span>
            {total >= limit && limit < 100 && (
              <button type="button" className="link-button" onClick={() => setLimit((l) => l + PAGE)}>
                Show more
              </button>
            )}
          </div>
        </>
      )}
    </div>
  );
}
