import { useState, useEffect, useRef } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { discover as discoverApi, likes as likesApi, matches as matchesApi, profile as profileApi, preferences as prefsApi, errorCode, qk } from '../api';
import { scoreOf, photoOf } from '../lib/compat';
import { topTraits, traitPercent } from '../lib/traits';
import { locateMe } from '../lib/geo';
import TiltCard from '../components/TiltCard';
import CompatibilityRing from '../components/CompatibilityRing';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import MatchCelebration from '../components/MatchCelebration';
import { IconHeart, IconChat, IconPin, IconUser, IconSpark } from '../components/Icons';

function PersonCard({ person, score, photo, actions }) {
  const traits = topTraits(person.traits);
  // A stored photo URL can outlive the object behind it; fall back to the
  // initials tile rather than rendering a broken image. The failure resets
  // during render when the photo changes, which avoids the cascading render an
  // effect would cause.
  const [photoFailed, setPhotoFailed] = useState(false);
  const [lastPhoto, setLastPhoto] = useState(photo);
  if (photo !== lastPhoto) {
    setLastPhoto(photo);
    setPhotoFailed(false);
  }
  return (
    <TiltCard max={8}>
      <article className="match-card">
        <div className="match-media">
          <span className="match-ring-badge">
            <CompatibilityRing value={score} size={50} stroke={4} label="" />
          </span>
          {photo && !photoFailed ? (
            <img
              src={photo}
              alt={person.name || ''}
              width={238}
              height={146}
              loading="lazy"
              onError={() => setPhotoFailed(true)}
            />
          ) : (
            <div className="match-media-fallback">
              <Avatar name={person.name} seed={person.user_id} size={74} fill />
            </div>
          )}
          <h3 className="match-name">{person.name}</h3>
        </div>
        <div className="match-body">
          <div className="match-meta">
            {person.age != null && <span className="chip">{person.age}</span>}
            {person.gender && (
              <span className="chip"><IconUser /> {person.gender}</span>
            )}
            {person.location && (
              <span className="chip"><IconPin /> {person.location}</span>
            )}
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
          {person.bio && <p className="match-bio">{person.bio}</p>}
          <div className="match-actions">{actions}</div>
        </div>
      </article>
    </TiltCard>
  );
}

export default function Matches() {
  const notice = useLocation().state?.notice;
  const qc = useQueryClient();
  const mutual = useQuery({
    queryKey: qk.matches,
    queryFn: () => matchesApi.list({ limit: 50 }).then((r) => r.data),
  });
  const feed = useInfiniteQuery({
    queryKey: qk.discover,
    queryFn: ({ pageParam }) =>
      discoverApi
        .list({ limit: 20, ...(pageParam ? { cursor: pageParam } : {}) })
        .then((r) => r.data),
    initialPageParam: '',
    getNextPageParam: (last) => last.next_cursor || undefined,
    staleTime: 30_000,
    retry: (count, err) => errorCode(err) !== 'assessment_required' && count < 1,
  });
  const [gone, setGone] = useState({});
  const [matchIds, setMatchIds] = useState({});
  const [celebration, setCelebration] = useState(null);
  const [locating, setLocating] = useState(false);
  const [locateError, setLocateError] = useState('');
  const banner = notice || '';
  const me = useQuery({
    queryKey: qk.profile,
    queryFn: () => profileApi.get().then((r) => r.data),
    staleTime: 60_000,
  });
  const prefs = useQuery({
    queryKey: qk.preferences,
    queryFn: () => prefsApi.get().then((r) => r.data),
    staleTime: 60_000,
  });
  // Missing coordinates cut both ways: the viewer's own distance filter cannot
  // apply, and discovery drops candidates without coordinates entirely, so this
  // profile is invisible to anyone filtering by distance. Prompt on the missing
  // location itself rather than only when this user set a distance preference.
  const needsLocation = me.data?.lat == null;
  const distanceFilterOn = Boolean(prefs.data?.max_distance_km);

  const swipe = useMutation({
    mutationFn: ({ user_id, action }) => likesApi.swipe(user_id, action).then((r) => r.data),
    onMutate: ({ user_id }) => {
      setGone((g) => ({ ...g, [user_id]: true }));
    },
    onError: (_err, vars) => {
      setGone((g) => {
        const next = { ...g };
        delete next[vars.user_id];
        return next;
      });
    },
    onSuccess: (data, vars) => {
      if (data?.matched && data.match_id) {
        setMatchIds((m) => ({ ...m, [vars.user_id]: data.match_id }));
        qc.invalidateQueries({ queryKey: qk.matches });
      }
    },
  });
  const sentinelRef = useRef(null);
  const { hasNextPage, isFetchingNextPage, fetchNextPage } = feed;

  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return undefined;
    const io = new IntersectionObserver((entries) => {
      if (entries.some((e) => e.isIntersecting) && hasNextPage && !isFetchingNextPage) {
        fetchNextPage();
      }
    }, { rootMargin: '240px' });
    io.observe(el);
    return () => io.disconnect();
  }, [hasNextPage, isFetchingNextPage, fetchNextPage]);

  const matched = mutual.data?.items || [];
  const discover = (feed.data?.pages || [])
    .flatMap((p) => p.items || [])
    .filter((m) => !gone[m.user_id]);

  function onSwipe(person, action) {
    if (swipe.isPending) return;
    swipe.mutate(
      { user_id: person.user_id, action },
      {
        onSuccess: (data) => {
          if (data?.matched && data.match_id) {
            setCelebration({
              name: person.name,
              photo: photoOf(person),
              userId: person.user_id,
              matchId: data.match_id,
            });
          }
        },
      }
    );
  }

  if (mutual.isPending && feed.isPending) {
    return (
      <div className="page">
        <div className="match-grid">
          {[0, 1, 2].map((n) => <div key={n} className="skeleton-card" />)}
        </div>
      </div>
    );
  }

  const quizNeeded = errorCode(feed.error) === 'assessment_required';
  const busyId = swipe.isPending ? swipe.variables?.user_id : null;

  return (
    <div className="page">
      <span className="section-kicker">
        <IconSpark /> Curated for you
      </span>
      <h1>Your matches</h1>
      <p>People you have already matched, then new people scored by the server.</p>

      {banner && (
        <div className="alert alert-success" role="status">
          {banner}
        </div>
      )}
      {needsLocation && (
        <div className="alert" role="status">
          {distanceFilterOn
            ? 'Distance is on, but we do not have your location yet, so your feed is unfiltered — and people who filter by distance cannot see you.'
            : 'We do not have your location yet, so people who filter by distance cannot see you.'}
          <button
            type="button"
            className="btn btn-ghost"
            disabled={locating}
            onClick={async () => {
              setLocateError('');
              setLocating(true);
              try {
                const pos = await locateMe();
                await profileApi.update({ lat: pos.lat, lng: pos.lng });
                qc.invalidateQueries({ queryKey: qk.profile });
                qc.invalidateQueries({ queryKey: qk.discover });
              } catch (err) {
                setLocateError(err.message);
              } finally {
                setLocating(false);
              }
            }}
          >
            {locating ? 'Finding you…' : 'Use my location'}
          </button>
          {locateError && <p className="field-hint">{locateError}</p>}
        </div>
      )}
      {swipe.error && (
        <div className="alert alert-error" role="alert">
          {swipe.error.message}
        </div>
      )}

      {mutual.isError ? (
        <ErrorState text="We couldn't load your matches right now." onRetry={() => mutual.refetch()} />
      ) : matched.length > 0 ? (
        <div className="match-grid stagger">
          {matched.map((row) => {
            const person = row.user || {};
            const photo = photoOf(person);
            const score = Math.round(scoreOf(row) * 100);
            return (
              <PersonCard
                key={row.match_id}
                person={person}
                score={score}
                photo={photo}
                actions={
                  <>
                    <Link
                      to={`/app/matches/${person.user_id}`}
                      state={{ score: scoreOf(row), matchId: row.match_id, traits: person.traits }}
                      className="btn btn-ghost"
                    >
                      View
                    </Link>
                    <Link
                      to={`/app/conversations/start/${person.user_id}`}
                      state={{
                        name: person.name,
                        bio: person.bio,
                        photo_url: photo,
                        matchId: row.match_id,
                      }}
                      className="btn btn-primary"
                    >
                      <IconChat /> Message
                    </Link>
                  </>
                }
              />
            );
          })}
        </div>
      ) : null}

      <h2 className="section-kicker" style={{ marginTop: '2rem' }}>
        <IconHeart /> Discover
      </h2>

      {quizNeeded ? (
        <div className="empty-state">
          <h3>Personality quiz needed</h3>
          <p>Take the quiz so we can score people against you.</p>
          <Link to="/onboarding/quiz" className="btn btn-primary">Start quiz</Link>
        </div>
      ) : feed.isError ? (
        <ErrorState text="We couldn't load people to discover right now." onRetry={() => feed.refetch()} />
      ) : feed.isPending ? (
        <Loading text="Loading people…" />
      ) : discover.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconHeart /></span>
          <h3>No one new right now</h3>
          <p>Complete your profile, or check back after more people join.</p>
          <Link to="/app/profile" className="btn btn-primary">Complete profile</Link>
        </div>
      ) : (
        <>
          <div className="match-grid stagger">
            {discover.map((m) => {
              const score = Math.round(scoreOf(m) * 100);
              const photo = photoOf(m);
              const matchedNow = matchIds[m.user_id];
              return (
                <PersonCard
                  key={m.user_id}
                  person={m}
                  score={score}
                  photo={photo}
                  actions={
                    <>
                      <Link
                        to={`/app/matches/${m.user_id}`}
                        state={{ score: scoreOf(m), matchId: matchedNow, traits: m.traits }}
                        className="btn btn-ghost"
                      >
                        View
                      </Link>
                      {matchedNow ? (
                        <Link
                          to={`/app/conversations/start/${m.user_id}`}
                          state={{
                            name: m.name,
                            bio: m.bio,
                            photo_url: photo,
                            matchId: matchedNow,
                          }}
                          className="btn btn-primary"
                        >
                          <IconChat /> Message
                        </Link>
                      ) : (
                        <>
                          <button
                            type="button"
                            className="btn btn-ghost"
                            disabled={busyId === m.user_id}
                            onClick={() => onSwipe(m, 'pass')}
                          >
                            Pass
                          </button>
                          <button
                            type="button"
                            className="btn btn-primary"
                            disabled={busyId === m.user_id}
                            onClick={() => onSwipe(m, 'like')}
                          >
                            <IconHeart /> Like
                          </button>
                        </>
                      )}
                    </>
                  }
                />
              );
            })}
          </div>
          <div ref={sentinelRef} className="load-more" aria-hidden="true" />
          {isFetchingNextPage && <p className="field-hint" style={{ textAlign: 'center' }}>Loading more…</p>}
          {celebration && (
            <MatchCelebration
              name={celebration.name}
              photo={celebration.photo}
              to={`/app/conversations/start/${celebration.userId}`}
              state={{
                name: celebration.name,
                photo_url: celebration.photo,
                matchId: celebration.matchId,
              }}
              onClose={() => setCelebration(null)}
            />
          )}
        </>
      )}
    </div>
  );
}
