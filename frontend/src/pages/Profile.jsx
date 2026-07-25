import { useState, useEffect } from 'react';
import { profile as profileApi } from '../api';
import { useAuth } from '../context/AuthContext';
import Avatar from '../components/Avatar';
import { IconUser, IconSpark } from '../components/Icons';

export default function Profile() {
  const { user } = useAuth();
  const [data, setData] = useState({ bio: '', gender: '', location: '', photo_url: '' });
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [message, setMessage] = useState('');

  useEffect(() => {
    profileApi
      .get()
      .then((res) => {
        const p = res.data;
        setData({
          bio: p.bio || '',
          gender: p.gender || '',
          location: p.location || '',
          photo_url: p.photo_url || '',
        });
      })
      .catch(() => setMessage('Failed to load profile'))
      .finally(() => setLoading(false));
  }, []);

  async function handleSubmit(e) {
    e.preventDefault();
    setSaving(true);
    setMessage('');
    try {
      await profileApi.update(data);
      setMessage('Profile updated');
    } catch {
      setMessage('Update failed');
    } finally {
      setSaving(false);
    }
  }

  if (loading)
    return (
      <div className="loading-page">
        <div className="spinner" />
        <p>Loading your profile…</p>
      </div>
    );

  return (
    <div className="page">
      <span className="section-kicker">
        <IconSpark /> Your profile
      </span>
      <h1>How the world sees you</h1>
      <p>Keep this fresh — it's what powers your matches.</p>
      {message && <p className="message">{message}</p>}

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
