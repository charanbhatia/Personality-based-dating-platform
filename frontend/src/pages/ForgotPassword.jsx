import { useState } from 'react';
import { Link } from 'react-router-dom';
import { auth } from '../api';
import AuthShell from '../components/AuthShell';

export default function ForgotPassword() {
  const [email, setEmail] = useState('');
  const [sent, setSent] = useState(false);
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(false);

  async function handleSubmit(e) {
    e.preventDefault();
    setError('');
    setLoading(true);
    try {
      await auth.forgotPassword(email);
      setSent(true);
    } catch (err) {
      setError(err.response || err.code ? err.message : 'Could not send reset email');
    } finally {
      setLoading(false);
    }
  }

  return (
    <AuthShell>
      <div className="auth-page">
        <h1>Forgot password</h1>
        <p className="auth-sub">We will send a reset link if that email has an account.</p>
        {sent ? (
          <div className="alert alert-success" role="status">
            If an account exists for that address, a reset email is on its way. Check the
            server log if email is in log mode.
          </div>
        ) : (
          <form onSubmit={handleSubmit}>
            {error && <div className="alert alert-error" role="alert">{error}</div>}
            <div className="field">
              <label htmlFor="email">Email</label>
              <input
                id="email"
                className="input"
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                required
              />
            </div>
            <button type="submit" className="btn btn-primary btn-block" disabled={loading}>
              {loading ? 'Sending…' : 'Send reset link'}
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
