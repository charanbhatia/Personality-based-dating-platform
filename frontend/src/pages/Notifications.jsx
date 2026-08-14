import { useNavigate } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { notifications as notifApi, qk } from '../api';
import { registerWebDevice } from '../lib/push';
import ErrorState from '../components/ErrorState';
import VirtualList from '../components/VirtualList';
import { IconChat, IconHeart } from '../components/Icons';

function parseData(n) {
  let data = n.data;
  if (typeof data === 'string') {
    try { data = JSON.parse(data); } catch { data = {}; }
  }
  return data || {};
}

function hrefFor(n) {
  const data = parseData(n);
  if (data.conversation_id) return `/app/conversations/${data.conversation_id}`;
  if (data.user_id) return `/app/conversations/start/${data.user_id}`;
  if (n.type === 'match.created') return '/app/matches';
  return '/app/notifications';
}

export default function Notifications() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const { data, error, isPending, refetch } = useQuery({
    queryKey: qk.notifications,
    queryFn: () => notifApi.list({ limit: 50 }).then((r) => r.data),
  });
  const list = data?.items || [];

  async function open(n) {
    if (!n.read_at) {
      try {
        await notifApi.markRead(n.id);
        qc.invalidateQueries({ queryKey: qk.notifications });
        qc.invalidateQueries({ queryKey: qk.unread });
      } catch { /* still navigate */ }
    }
    const to = hrefFor(n);
    const payload = parseData(n);
    navigate(to, payload.match_id ? { state: { matchId: payload.match_id } } : undefined);
  }

  async function markAll() {
    await notifApi.markAllRead();
    qc.invalidateQueries({ queryKey: qk.notifications });
    qc.invalidateQueries({ queryKey: qk.unread });
  }

  async function enableDesktop() {
    if (!('Notification' in window)) return;
    await Notification.requestPermission();
    await registerWebDevice().catch(() => {});
  }

  if (isPending) {
    return (
      <div className="page">
        {[0, 1, 2, 3].map((n) => <div key={n} className="skeleton-row" />)}
      </div>
    );
  }

  const canAskNotify = typeof Notification !== 'undefined' && Notification.permission === 'default';

  return (
    <div className="page">
      <span className="section-kicker">Inbox</span>
      <h1>Notifications</h1>
      <div className="match-actions">
        {list.length > 0 && (
          <button type="button" className="btn btn-ghost" onClick={markAll}>
            Mark all read
          </button>
        )}
        {canAskNotify && (
          <button type="button" className="btn btn-ghost" onClick={enableDesktop}>
            Enable desktop alerts
          </button>
        )}
      </div>
      {error ? (
        <ErrorState text="We couldn't load notifications." onRetry={() => refetch()} />
      ) : list.length === 0 ? (
        <div className="empty-state">
          <span className="empty-icon"><IconHeart /></span>
          <h3>Nothing yet</h3>
          <p>Matches and new messages will show up here.</p>
        </div>
      ) : (
        <VirtualList count={list.length} estimateSize={76}>
          {(i) => {
            const n = list[i];
            return (
              <button type="button" className={`conv-item notif-item${n.read_at ? '' : ' is-unread'}`} onClick={() => open(n)}>
                <span className="feature-icon">{n.type?.includes('message') ? <IconChat /> : <IconHeart />}</span>
                <span className="conv-text">
                  <span className="conv-title">{n.title}</span>
                  <span className="conv-sub">{n.body}</span>
                </span>
              </button>
            );
          }}
        </VirtualList>
      )}
    </div>
  );
}
