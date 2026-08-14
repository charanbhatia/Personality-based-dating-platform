import { useState } from 'react';
import { Link } from 'react-router-dom';
import { useQuery, useQueryClient } from '@tanstack/react-query';
import { auth, safety, qk } from '../api';
import { useAuth } from '../context/AuthContext';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconShield } from '../components/Icons';

export default function Settings() {
  const { user, logout } = useAuth();
  const qc = useQueryClient();
  const sessions = useQuery({
    queryKey: qk.sessions,
    queryFn: () => auth.sessions().then((r) => r.data),
  });
  const blocks = useQuery({
    queryKey: qk.blocks,
    queryFn: () => safety.listBlocks().then((r) => r.data),
  });
  const [msg, setMsg] = useState('');

  async function resend() {
    try {
      await auth.resendEmail();
      setMsg('Verification email sent (or logged by the server).');
    } catch (err) {
      setMsg(err.message);
    }
  }

  async function revoke(id) {
    await auth.revokeSession(id);
    qc.invalidateQueries({ queryKey: qk.sessions });
  }

  async function unblock(id) {
    await safety.unblock(id);
    qc.invalidateQueries({ queryKey: qk.blocks });
    qc.invalidateQueries({ queryKey: qk.discover });
  }

  const items = sessions.data?.items || [];
  const blocked = blocks.data?.items || blocks.data || [];
  const blockList = Array.isArray(blocked) ? blocked : [];

  return (
    <div className="page">
      <span className="section-kicker">
        <IconShield /> Account
      </span>
      <h1>Settings</h1>

      <section className="settings-block">
        <h2>Email</h2>
        <p>
          {user?.email}{' '}
          {user?.email_verified ? '(verified)' : '(not verified — you can still use the app)'}
        </p>
        {!user?.email_verified && (
          <button type="button" className="btn btn-ghost" onClick={resend}>
            Resend verification
          </button>
        )}
        {msg && <p className="auth-sub">{msg}</p>}
      </section>

      <section className="settings-block">
        <h2>Onboarding</h2>
        <p>
          <Link to="/onboarding/quiz">Retake quiz</Link>
          {' · '}
          <Link to="/onboarding/preferences">Preferences</Link>
          {' · '}
          <Link to="/onboarding/photos">Photos</Link>
        </p>
      </section>

      <section className="settings-block">
        <h2>Active sessions</h2>
        {sessions.isPending ? (
          <Loading text="Loading sessions…" />
        ) : sessions.error ? (
          <ErrorState text="Could not load sessions." onRetry={() => sessions.refetch()} />
        ) : (
          <ul className="session-list">
            {items.map((s) => (
              <li key={s.id} className="session-row">
                <span>
                  <b>{s.current ? 'This device' : s.user_agent || 'Other device'}</b>
                  <br />
                  <span className="conv-sub">
                    {s.ip} · last used {s.last_used_at ? new Date(s.last_used_at).toLocaleString() : '—'}
                  </span>
                </span>
                {!s.current && (
                  <button type="button" className="btn btn-ghost" onClick={() => revoke(s.id)}>
                    Revoke
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="settings-block">
        <h2>Blocked people</h2>
        {blocks.isPending ? (
          <Loading />
        ) : blockList.length === 0 ? (
          <p className="auth-sub">You have not blocked anyone.</p>
        ) : (
          <ul className="session-list">
            {blockList.map((b) => (
              <li key={b.user_id} className="session-row">
                <span>{b.name || b.user_id}</span>
                <button type="button" className="btn btn-ghost" onClick={() => unblock(b.user_id)}>
                  Unblock
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <button type="button" className="btn btn-primary" onClick={logout}>
        Log out
      </button>
    </div>
  );
}
