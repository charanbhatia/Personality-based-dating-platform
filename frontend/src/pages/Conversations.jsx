import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi, matches as matchesApi } from '../api';
import useFetch from '../hooks/useFetch';
import { band, isScored } from '../lib/compat';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconChat, IconArrowLeft } from '../components/Icons';

const day = (iso) =>
  new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });

export default function Conversations() {
  const { user } = useAuth();

  // The conversations payload carries only ids, so names come from the match
  // list. A failure there costs us the names, not the page.
  const { data, error, loading, retry } = useFetch(() =>
    Promise.all([
      convApi.list(),
      matchesApi.list({ limit: 100 }).catch(() => ({ data: { matches: [] } })),
    ]).then(([conv, people]) => ({
      data: {
        conversations: conv.data.conversations || [],
        people: Object.fromEntries((people.data.matches || []).map((m) => [m.user_id, m])),
      },
    }))
  );

  if (loading) return <Loading text="Loading messages…" />;

  const list = [...(data?.conversations || [])].sort(
    (a, b) => new Date(b.created_at) - new Date(a.created_at)
  );
  const people = data?.people || {};

  return (
    <div className="page">
      <span className="section-kicker">
        <IconChat /> Messages
      </span>
      <h1>Your conversations</h1>
      <p>The people you have started talking to.</p>

      {error ? (
        <ErrorState text="We couldn't load your conversations right now." onRetry={retry} />
      ) : list.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconChat /></span>
          <h3>No conversations yet</h3>
          <p>When you message someone, your chats will show up here.</p>
          <Link to="/app/matches" className="btn btn-primary">Find someone to talk to</Link>
        </div>
      ) : (
        <ul className="conv-list stagger">
          {list.map((c) => {
            const otherId = c.user1_id === user?.id ? c.user2_id : c.user1_id;
            const peer = people[otherId];
            return (
              <li key={c.id}>
                <Link
                  to={`/app/conversations/${c.id}`}
                  state={{ name: peer?.name, bio: peer?.bio }}
                  className="conv-item"
                >
                  <Avatar
                    name={peer?.name}
                    src={peer?.photo_url || undefined}
                    seed={otherId}
                    size={44}
                  />
                  <span className="conv-text">
                    <span className="conv-title">{peer?.name || 'Conversation'}</span>
                    <span className="conv-sub">
                      Started {day(c.created_at)}
                      {peer && isScored(peer.score) ? ` · ${band(peer.score)}` : ''}
                    </span>
                  </span>
                  <span className="conv-arrow">
                    <IconArrowLeft style={{ transform: 'rotate(180deg)' }} />
                  </span>
                </Link>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
}
