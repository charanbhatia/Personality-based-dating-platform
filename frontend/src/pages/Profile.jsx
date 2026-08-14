import { useState, useEffect } from 'react';
import { useQuery } from '@tanstack/react-query';
import { profile as profileApi, qk } from '../api';
import { useAuth } from '../context/AuthContext';
import { photoOf } from '../lib/compat';
import Avatar from '../components/Avatar';
import Loading from '../components/Loading';
import ErrorState from '../components/ErrorState';
import InterestChips from '../components/InterestChips';
import PhotoUploader from '../components/PhotoUploader';
import { IconSpark } from '../components/Icons';
import { GENDER_LABEL } from '../lib/genders';
import { locateMe } from '../lib/geo';

export default function Profile() {
  const { user } = useAuth();
  const { data: loaded, error: loadError, isPending, refetch } = useQuery({
    queryKey: qk.profile,
    queryFn: () =>
      Promise.all([profileApi.get(), profileApi.options().catch(() => ({ data: {} }))]).then(
        ([prof, opts]) => ({ profile: prof.data, genders: opts.data?.genders })
      ),
    staleTime: 60_000,
  });
  const [data, setData] = useState({
    bio: '',
    gender: '',
    location: '',
    interests: [],
    photo_urls: [],
    lat: null,
    lng: null,
  });
  const [saving, setSaving] = useState(false);
  const [status, setStatus] = useState(null);

  useEffect(() => {
    if (!loaded?.profile) return;
    const p = loaded.profile;
    const urls = p.photo_urls?.length ? p.photo_urls : (photoOf(p) ? [photoOf(p)] : []);
    setData({
      bio: p.bio || '',
      gender: p.gender || '',
      location: p.location || '',
      interests: p.interests || [],
      photo_urls: urls,
      lat: p.lat ?? null,
      lng: p.lng ?? null,
    });
  }, [loaded]);

  async function handleSubmit(e) {
    e.preventDefault();
    setSaving(true);
    setStatus(null);
    try {
      await profileApi.update({
        bio: data.bio,
        gender: data.gender,
        location: data.location,
        interests: data.interests,
        ...(data.lat != null && data.lng != null ? { lat: data.lat, lng: data.lng } : {}),
      });
      setStatus({ text: 'Profile updated', ok: true });
    } catch (err) {
      setStatus({ text: err.message || 'Update failed. Your changes were not saved.', ok: false });
    } finally {
      setSaving(false);
    }
  }

  if (isPending) return <Loading text="Loading your profile…" />;

  if (loadError || !loaded?.profile)
    return (
      <div className="page">
        <ErrorState text="We couldn't load your profile, so it isn't safe to edit yet." onRetry={() => refetch()} />
      </div>
    );

  const genders = loaded.genders?.length ? loaded.genders : Object.keys(GENDER_LABEL);

  return (
    <div className="page">
      <span className="section-kicker">
        <IconSpark /> Your profile
      </span>
      <h1>How the world sees you</h1>
      <p>Keep this fresh — it's what powers your matches.</p>
      {status && (
        <div className={`alert ${status.ok ? 'alert-success' : 'alert-error'}`} role="status">
          {status.text}
        </div>
      )}

      <div className="profile-grid">
        <div className="profile-preview">
          <Avatar
            name={user?.name}
            src={data.photo_urls[0] || undefined}
            seed={user?.id || user?.email}
            size={128}
            ring
          />
          <div>
            <div className="pp-name">{user?.name || 'Your name'}</div>
            <div className="pp-meta">
              {data.location || 'Add your location'}
              {data.gender ? ` · ${GENDER_LABEL[data.gender] || data.gender}` : ''}
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
              maxLength={500}
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
              {genders.map((g) => (
                <option key={g} value={g}>
                  {GENDER_LABEL[g] || g}
                </option>
              ))}
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
              maxLength={120}
            />
            <button
              type="button"
              className="btn btn-ghost"
              onClick={async () => {
                try {
                  const pos = await locateMe();
                  setData((d) => ({ ...d, lat: pos.lat, lng: pos.lng }));
                } catch (err) {
                  setStatus({ text: err.message, ok: false });
                }
              }}
            >
              {data.lat != null ? 'Location saved for distance filter' : 'Use my location for distance'}
            </button>
          </div>

          <div className="field">
            <label className="form-label" htmlFor="interests">Interests</label>
            <InterestChips
              value={data.interests}
              onChange={(interests) => setData((d) => ({ ...d, interests }))}
            />
          </div>

          <PhotoUploader
            photos={data.photo_urls}
            onChange={(photo_urls) => setData((d) => ({ ...d, photo_urls }))}
          />

          <button type="submit" className="btn btn-primary" disabled={saving}>
            {saving ? 'Saving…' : 'Save changes'}
          </button>
        </form>
      </div>
    </div>
  );
}
