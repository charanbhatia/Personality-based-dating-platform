import { useState, useEffect } from 'react';
import { profile as profileApi } from '../api';

export default function Profile() {
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

  if (loading) return <div className="loading-page">Loading profile...</div>;

  return (
    <div className="page">
      <h1>My profile</h1>
      {message && <p className="message">{message}</p>}
      <form onSubmit={handleSubmit}>
        <textarea
          placeholder="Bio"
          value={data.bio}
          onChange={(e) => setData((d) => ({ ...d, bio: e.target.value }))}
          rows={4}
        />
        <label className="form-label">Gender</label>
        <select
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
        <input
          type="text"
          placeholder="Location"
          value={data.location}
          onChange={(e) => setData((d) => ({ ...d, location: e.target.value }))}
        />
        <input
          type="text"
          placeholder="Photo URL"
          value={data.photo_url}
          onChange={(e) => setData((d) => ({ ...d, photo_url: e.target.value }))}
        />
        <button type="submit" disabled={saving}>
          {saving ? 'Saving...' : 'Save'}
        </button>
      </form>
    </div>
  );
}
