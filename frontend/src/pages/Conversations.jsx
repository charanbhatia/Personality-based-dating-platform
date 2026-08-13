import { Link } from 'react-router-dom';
import { useAuth } from '../context/AuthContext';
import { conversations as convApi } from '../api';
import useFetch from '../hooks/useFetch';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconChat, IconArrowLeft } from '../components/Icons';

export default function Conversations() {
  const { user } = useAuth();
  const { data, error, loading, retry } = useFetch(() => convApi.list());
  const list = data?.conversations || [];

  if (loading) return <Loading text="Loading messages…" />;

  return (
    <div className="page">
      <span className="section-kicker">
        <IconChat /> Messages
      </span>
      <h1>Your conversations</h1>
      <p>Meaningful chats with the people you've matched with.</p>

      {error ? (
        <ErrorState text="We couldn't load your conversations right now." onRetry={retry} />
      ) : list.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconChat /></span>
          <h3>No conversations yet</h3>
          <p>When you message a match, your chats will show up here.</p>
          <Link to="/app/matches" className="btn btn-primary">Find someone to talk to</Link>
        </div>
      ) : (
        <ul className="conv-list stagger">
          {list.map((c) => {
            const otherId = c.user1_id === user?.id ? c.user2_id : c.user1_id;
            const short = otherId ? String(otherId).slice(0, 8) : '';
            return (
              <li key={c.id}>
                <Link to={`/app/conversations/${c.id}`} className="conv-item">
                  <Avatar name={short || 'Chat'} seed={otherId} size={46} />
                  <span className="conv-text">
                    <span className="conv-title">
                      {otherId ? `Conversation · ${short}` : 'Conversation'}
                    </span>
                    <span className="conv-sub">Tap to open the chat</span>
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
