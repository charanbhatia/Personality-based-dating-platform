import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { profile as profileApi, qk } from '../../api';
import { GENDER_LABEL, GENDER_VALUES } from '../../lib/genders';
import { useAuth } from '../../context/AuthContext';
import Loading from '../../components/Loading';
import ErrorState from '../../components/ErrorState';
import InterestChips from '../../components/InterestChips';
import { locateMe } from '../../lib/geo';

export default function ProfileSetup() {
  const navigate = useNavigate();
  const { user, refreshUser } = useAuth();
  const { data, error, isPending, refetch } = useQuery({
    queryKey: qk.profile,
    queryFn: () => profileApi.get().then((r) => r.data),
  });
  const [form, setForm] = useState({
    bio: '',
    gender: '',
    location: '',
    interests: [],
    date_of_birth: user?.date_of_birth || '',
    lat: null,
    lng: null,
  });
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!data) return;
    setForm({
      bio: data.bio || '',
      gender: data.gender || '',
      location: data.location || '',
      interests: data.interests || [],
      date_of_birth: data.date_of_birth || user?.date_of_birth || '',
      lat: data.lat ?? null,
      lng: data.lng ?? null,
    });
  }, [data, user]);

  async function handleSubmit(e) {
    e.preventDefault();
    setSaving(true);
    setErr('');
    try {
      await profileApi.update({
        bio: form.bio,
        gender: form.gender,
        location: form.location,
        interests: form.interests,
        date_of_birth: form.date_of_birth || undefined,
        ...(form.lat != null && form.lng != null ? { lat: form.lat, lng: form.lng } : {}),
      });
      await refreshUser();
      navigate('/onboarding/photos');
    } catch (ex) {
      setErr(ex.message);
    } finally {
      setSaving(false);
    }
  }

  if (isPending) return <Loading text="Loading your profile…" />;
  if (error) return <ErrorState text="We couldn't load your profile." onRetry={() => refetch()} />;

  return (
    <div className="page">
      <h1>Your profile</h1>
      <p>Bio, gender and date of birth are required so you can appear in the feed.</p>
      <form onSubmit={handleSubmit} className="profile-form">
        {err && <div className="alert alert-error" role="alert">{err}</div>}
        <div className="field">
          <label htmlFor="bio">About you</label>
          <textarea
            id="bio"
            className="input"
            rows={4}
            maxLength={500}
            value={form.bio}
            onChange={(e) => setForm((f) => ({ ...f, bio: e.target.value }))}
            required
          />
        </div>
        <div className="field">
          <label htmlFor="gender">Gender</label>
          <select
            id="gender"
            className="form-select"
            value={form.gender}
            onChange={(e) => setForm((f) => ({ ...f, gender: e.target.value }))}
            required
          >
            <option value="">Select</option>
            {GENDER_VALUES.map((g) => (
              <option key={g} value={g}>{GENDER_LABEL[g]}</option>
            ))}
          </select>
        </div>
        <div className="field">
          <label htmlFor="dob">Date of birth</label>
          <input
            id="dob"
            className="input"
            type="date"
            value={form.date_of_birth || ''}
            onChange={(e) => setForm((f) => ({ ...f, date_of_birth: e.target.value }))}
            required
          />
        </div>
        <div className="field">
          <label htmlFor="location">Location</label>
          <input
            id="location"
            className="input"
            maxLength={120}
            value={form.location}
            onChange={(e) => setForm((f) => ({ ...f, location: e.target.value }))}
          />
          <button
            type="button"
            className="btn btn-ghost"
            onClick={async () => {
              try {
                const pos = await locateMe();
                setForm((f) => ({ ...f, lat: pos.lat, lng: pos.lng }));
              } catch (ex) {
                setErr(ex.message);
              }
            }}
          >
            {form.lat != null ? 'Location saved for distance filter' : 'Use my location for distance'}
          </button>
        </div>
        <div className="field">
          <label htmlFor="interests">Interests</label>
          <InterestChips
            value={form.interests}
            onChange={(interests) => setForm((f) => ({ ...f, interests }))}
          />
        </div>
        <button type="submit" className="btn btn-primary" disabled={saving}>
          {saving ? 'Saving…' : 'Continue'}
        </button>
      </form>
    </div>
  );
}
