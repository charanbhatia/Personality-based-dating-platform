import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { auth } from '../api';
import AuthShell from '../components/AuthShell';

export default function ResetPassword() {
  const [params] = useSearchParams();
  const [token, setToken] = useState(params.get('token') || '');
  const [password, setPassword] = useState('');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await auth.resetPassword(token.trim(), password);
      setDone(true);
    } catch (err) {
      setError(err.response || err.code ? err.message : 'Could not reset password');
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthShell>
      <div className="auth-page">
        <h1>Set a new password</h1>
        <p className="auth-sub">Paste the token from your email if it is not already in the link.</p>
        {done ? (
          <>
            <div className="alert alert-success" role="status">
              Password updated. Sign in with your new password.
            </div>
            <Link to="/login" className="btn btn-primary btn-block">Log in</Link>
          </>
        ) : (
          <form onSubmit={handleSubmit}>
            {error && <div className="alert alert-error" role="alert">{error}</div>}
            <div className="field">
              <label htmlFor="token">Reset token</label>
              <input
                id="token"
                className="input"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                required
              />
            </div>
            <div className="field">
              <label htmlFor="password">New password</label>
              <input
                id="password"
                className="input"
                type="password"
                minLength={8}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                required
              />
            </div>
            <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
              {loading ? 'Saving…' : 'Update password'}
            </button>
          </form>
        )}
        <p className="auth-footer">
          <Link to="/login">Back to log in</Link>
        </p>
      </div>
    </AuthShell>
  );
}
