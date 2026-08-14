import { useState, useEffect } from 'react';
import { profile as profileApi } from '../api';
import useFetch from '../hooks/useFetch';
import { useAuth } from '../context/AuthContext';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import { IconSpark } from '../components/Icons';

export default function Profile() {
  const { user } = useAuth();
  const { data: loaded, error: loadError, loading, retry } = useFetch(() => profileApi.get());
  const [data, setData] = useState({ bio: '', gender: '', location: '', photo_url: '' });
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState(null);

  useEffect(() => {
    if (!loaded) return;
    setData({
      bio: loaded.bio || '',
      gender: loaded.gender || '',
      location: loaded.location || '',
      photo_url: loaded.photo_url || '',
    });
  }, [loaded]);

  async function handleSubmit(e) {
    e.preventDefault();
    setSaving(true);
    setStatus(null);
    try {
      await profileApi.update(data);
      setStatus({ text: 'Profile updated', ok: true });
    } catch {
      setStatus({ text: 'Update failed. Your changes were not saved.', ok: false });
    } finally {
      setSaving(false);
    }
  }

  if (loading) return <Loading text="Loading your profile…" />;

  // PUT /api/profile replaces every field, so rendering the form over failed-to-load
  // defaults would let a Save blank out the real profile. Bail out instead.
  if (loadError || !loaded)
    return (
      <div className="page">
        <ErrorState text="We couldn't load your profile, so it isn't safe to edit yet." onRetry={retry} />
      </div>
    );

  return (
    <div className="page">
      <span className="section-kicker">
        <IconSpark /> Your profile
      </span>
      <h1>How the world sees you</h1>
      <p>Keep this fresh — it's what powers your matches.</p>
      {/* A <div>, not a <p>: `.page > p` (0-1-1) would out-rank `.alert-error`
          (0-1-0) and repaint the error text as ordinary body copy. */}
      {status && (
        <div className={`alert ${status.ok ? 'alert-success' : 'alert-error'}`} role="status">
          {status.text}
        </div>
      )}

      <div className="profile-grid">
        <div className="profile-preview">
          <Avatar
            name={user?.name}
            src={data.photo_url || undefined}
            seed={user?.id || user?.email}
            size={128}
            ring
          />
          <div>
            <div className="pp-name">{user?.name || 'Your name'}</div>
            <div className="pp-meta">
              {data.location || 'Add your location'}
              {data.gender ? ` · ${data.gender}` : ''}
            </div>
          </div>
        </div>

        <form className="profile-form" onSubmit={handleSubmit}>
          <div className="field">
            <label className="form-label" htmlFor="bio">About you</label>
            <textarea
              id="bio"
              placeholder="Share what makes you, you — interests, values, what you're looking for…"
              value={data.bio}
              onChange={(e) => setData((d) => ({ ...d, bio: e.target.value }))}
              rows={4}
            />
          </div>

          <div className="field">
            <label className="form-label" htmlFor="gender">Gender</label>
            <select
              id="gender"
              value={data.gender}
              onChange={(e) => setData((d) => ({ ...d, gender: e.target.value }))}
              className="form-select"
            >
              <option value="">Prefer not to say</option>
              <option value="Male">Male</option>
              <option value="Female">Female</option>
              <option value="Non-binary">Non-binary</option>
              <option value="Other">Other</option>
            </select>
          </div>

          <div className="field">
            <label className="form-label" htmlFor="location">Location</label>
            <input
              id="location"
              type="text"
              placeholder="City, Country"
              value={data.location}
              onChange={(e) => setData((d) => ({ ...d, location: e.target.value }))}
            />
          </div>

          <div className="field">
            <label className="form-label" htmlFor="photo">Photo URL</label>
            <input
              id="photo"
              type="text"
              placeholder="https://…"
              value={data.photo_url}
              onChange={(e) => setData((d) => ({ ...d, photo_url: e.target.value }))}
            />
          </div>

          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
        </form>
      </div>
    </div>
  );
}
