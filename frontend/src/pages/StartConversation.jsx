import { useEffect, useState } from 'react';
import { useParams, useNavigate, useLocation } from 'react-router-dom';
import { conversations as convApi, matches as matchesApi } from '../api';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';

async function matchIdForUser(userId, hinted) {
  if (hinted) return hinted;
  let cursor;
  for (;;) {
    const res = await matchesApi.list({
      limit: 50,
      ...(cursor ? { cursor } : {}),
    });
    const hit = (res.data.items || []).find((m) => m.user?.user_id === userId);
    if (hit?.match_id) return hit.match_id;
    cursor = res.data.next_cursor;
    if (!cursor) return null;
  }
}

export default function StartConversation() {
  const { userId } = useParams();
  const navigate = useNavigate();
  const { state } = useLocation();
  const [error, setError] = useState('');
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    if (!userId) return;
    let alive = true;
    (async () => {
      try {
        const matchId = await matchIdForUser(userId, state?.matchId);
        if (!matchId) {
          if (!alive) return;
          navigate('/app/matches', {
            replace: true,
            state: { notice: 'You can message after you both like each other.' },
          });
          return;
        }
        const res = await convApi.start(matchId);
        if (!alive) return;
        navigate(`/app/conversations/${res.data.id}`, { replace: true, state });
      } catch (err) {
        if (!alive) return;
        setError(err.message || 'Could not start this conversation.');
      }
    })();
    return () => {
      alive = false;
    };
  }, [userId, navigate, state, attempt]);

  if (error) {
    return (
      <div className="page">
        <ErrorState
          text={error}
          onRetry={() => {
            setError('');
            setAttempt((n) => n + 1);
          }}
        />
      </div>
    );
  }

  return <Loading text="Starting your conversation…" />;
}
