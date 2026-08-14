import { useEffect, useRef } from 'react';
import { NavLink } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { notifications as notifApi, qk } from '../api';
import { IconBell } from './Icons';

export default function NotificationBell() {
  const { data } = useQuery({
    queryKey: qk.unread,
    queryFn: () => notifApi.unreadCount().then((r) => r.data),
    refetchInterval: 20_000,
  });
  const count = data?.unread_count || 0;
  const prev = useRef(undefined);

  useEffect(() => {
    if (
      document.hidden &&
      typeof Notification !== 'undefined' &&
      Notification.permission === 'granted' &&
      typeof prev.current === 'number' &&
      count > prev.current
    ) {
      try {
        new Notification('Kindred', { body: 'You have new notifications' });
      } catch { /* ignore */ }
    }
    prev.current = count;
  }, [count]);

  return (
    <NavLink to="/app/notifications" className="nav-item bell-link" aria-label="Notifications">
      <IconBell />
      {count > 0 && <span className="bell-count">{count > 9 ? '9+' : count}</span>}
    </NavLink>
  );
}
