import { useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { auth } from '../api';
import AuthShell from '../components/AuthShell';

export default function VerifyEmail() {
  const [params] = useSearchParams();
  const [token, setToken] = useState(params.get('token') || '');
  const [done, setDone] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await auth.verifyEmail(token.trim());
      setDone(true);
    } catch (err) {
      setError(err.response || err.code ? err.message : 'Could not verify email');
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthShell>
      <div className="auth-page">
        <h1>Verify your email</h1>
        <p className="auth-sub">You can still log in without this. Verification just confirms the address.</p>
        {done ? (
          <>
            <div className="alert alert-success" role="status">Email verified.</div>
            <Link to="/app" className="btn btn-primary btn-block">Continue</Link>
          </>
        ) : (
          <form onSubmit={handleSubmit}>
            {error && <div className="alert alert-error" role="alert">{error}</div>}
            <div className="field">
              <label htmlFor="token">Verification token</label>
              <input
                id="token"
                className="input"
                value={token}
                onChange={(e) => setToken(e.target.value)}
                required
              />
            </div>
            <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
              {loading ? 'Verifying…' : 'Verify'}
            </button>
          </form>
        )}
        <p className="auth-footer">
          <Link to="/login">Log in</Link>
        </p>
      </div>
    </AuthShell>
  );
}
