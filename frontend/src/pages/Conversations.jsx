import { Link } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { conversations as convApi, qk } from '../api';
import Avatar from '../components/Avatar';
import ErrorState from '../components/ErrorState';
import VirtualList from '../components/VirtualList';
import { IconChat, IconArrowLeft } from '../components/Icons';

const stamp = (iso) => {
  if (!iso) return '';
  const d = new Date(iso);
  return d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
};

export default function Conversations() {
  const { data, error, isPending, refetch } = useQuery({
    queryKey: qk.conversations,
    queryFn: () => convApi.list({ limit: 50 }).then((r) => r.data),
  });

  if (isPending) {
    return (
      <div className="page">
        {[0, 1, 2, 3].map((n) => <div key={n} className="skeleton-row" />)}
      </div>
    );
  }

  const list = data?.items || [];

  return (
    <div className="page">
      <span className="section-kicker">
        <IconChat /> Messages
      </span>
      <h1>Your conversations</h1>
      <p>The people you have started talking to.</p>

      {error ? (
        <ErrorState text="We couldn't load your conversations right now." onRetry={() => refetch()} />
      ) : list.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconChat /></span>
          <h3>No conversations yet</h3>
          <p>Match with someone, then your chats will show up here.</p>
          <Link to="/app/matches" className="btn btn-primary">Find someone to talk to</Link>
        </div>
      ) : (
        <VirtualList count={list.length} estimateSize={76}>
          {(i) => {
            const c = list[i];
            const peer = c.peer || {};
            const when = c.last_message_at || c.created_at;
            return (
              <Link
                to={`/app/conversations/${c.id}`}
                state={{
                  name: peer.name,
                  bio: peer.bio,
                  photo_url: peer.photo_url,
                  user_id: peer.user_id,
                }}
                className="conv-item"
              >
                <Avatar
                  name={peer.name}
                  src={peer.photo_url || undefined}
                  seed={peer.user_id || c.id}
                  size={44}
                />
                <span className="conv-text">
                  <span className="conv-title">
                    {peer.name || 'Conversation'}
                    {c.unread_count > 0 ? ` · ${c.unread_count}` : ''}
                  </span>
                  <span className="conv-sub">
                    {c.last_message_preview || (when ? `Started ${stamp(when)}` : 'No messages yet')}
                  </span>
                </span>
                <span className="conv-arrow">
                  <IconArrowLeft style={{ transform: 'rotate(180deg)' }} />
                </span>
              </Link>
            );
          }}
        </VirtualList>
      )}
    </div>
  );
}
