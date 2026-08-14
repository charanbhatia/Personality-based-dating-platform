import { useEffect, useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { preferences as prefsApi, profile as profileApi, qk } from '../../api';
import { GENDER_LABEL, GENDER_VALUES, TRAIT_KEYS, TRAIT_LABEL } from '../../lib/genders';
import { useAuth } from '../../context/AuthContext';
import { locateMe } from '../../lib/geo';
import Loading from '../../components/Loading';
import ErrorState from '../../components/ErrorState';

export default function Preferences() {
  const navigate = useNavigate();
  const { refreshUser } = useAuth();
  const { data, error, isPending, refetch } = useQuery({
    queryKey: qk.preferences,
    queryFn: () => prefsApi.get().then((r) => r.data),
  });
  const profile = useQuery({
    queryKey: qk.profile,
    queryFn: () => profileApi.get().then((r) => r.data),
    staleTime: 60_000,
  });
  const [form, setForm] = useState({
    age_min: 21,
    age_max: 45,
    genders: [],
    max_distance_km: '',
    trait_weights: {},
  });
  const [coords, setCoords] = useState({ lat: null, lng: null });
  const [saving, setSaving] = useState(false);
  const [err, setErr] = useState('');

  useEffect(() => {
    if (!data) return;
    setForm({
      age_min: data.age_min ?? 21,
      age_max: data.age_max ?? 45,
      genders: data.genders || [],
      max_distance_km: data.max_distance_km ?? '',
      trait_weights: data.trait_weights || {},
    });
  }, [data]);

  useEffect(() => {
    if (!profile.data) return;
    setCoords({ lat: profile.data.lat ?? null, lng: profile.data.lng ?? null });
  }, [profile.data]);

  function toggleGender(g) {
    setForm((f) => ({
      ...f,
      genders: f.genders.includes(g) ? f.genders.filter((x) => x !== g) : [...f.genders, g],
    }));
  }

  async function captureLocation() {
    const pos = await locateMe();
    setCoords({ lat: pos.lat, lng: pos.lng });
    await profileApi.update({ lat: pos.lat, lng: pos.lng });
    return pos;
  }

  async function handleSubmit(e) {
    e.preventDefault();
    setSaving(true);
    setErr('');
    try {
      const wantsDistance = form.max_distance_km !== '';
      if (wantsDistance && (coords.lat == null || coords.lng == null)) {
        try {
          await captureLocation();
        } catch (ex) {
          setErr(ex.message || 'Distance needs your location. Allow location access, or clear the distance field.');
          setSaving(false);
          return;
        }
      }
      const body = {
        age_min: Number(form.age_min),
        age_max: Number(form.age_max),
        genders: form.genders,
      };
      if (wantsDistance) body.max_distance_km = Number(form.max_distance_km);
      const weights = {};
      TRAIT_KEYS.forEach((k) => {
        if (form.trait_weights?.[k] != null && form.trait_weights[k] !== '') {
          weights[k] = Number(form.trait_weights[k]);
        }
      });
      if (Object.keys(weights).length) body.trait_weights = weights;
      await prefsApi.update(body);
      await refreshUser();
      navigate('/onboarding/profile');
    } catch (ex) {
      setErr(ex.message);
    } finally {
      setSaving(false);
    }
  }

  if (isPending) return <Loading text="Loading preferences…" />;
  if (error) return <ErrorState text="We couldn't load preferences." onRetry={() => refetch()} />;

  const hasCoords = coords.lat != null && coords.lng != null;

  return (
    <div className="page">
      <h1>Who are you looking for?</h1>
      <p>Age and at least one gender complete this step. A distance limit needs your location so the feed can filter.</p>
      <form onSubmit={handleSubmit} className="profile-form">
        {err && <div className="alert alert-error" role="alert">{err}</div>}
        <div className="field-row">
          <div className="field">
            <label htmlFor="age_min">Min age</label>
            <input
              id="age_min"
              className="input"
              type="number"
              min={18}
              max={120}
              value={form.age_min}
              onChange={(e) => setForm((f) => ({ ...f, age_min: e.target.value }))}
              required
            />
          </div>
          <div className="field">
            <label htmlFor="age_max">Max age</label>
            <input
              id="age_max"
              className="input"
              type="number"
              min={18}
              max={120}
              value={form.age_max}
              onChange={(e) => setForm((f) => ({ ...f, age_max: e.target.value }))}
              required
            />
          </div>
        </div>
        <fieldset className="field">
          <legend className="form-label">Genders to meet</legend>
          <div className="chip-pick">
            {GENDER_VALUES.map((g) => (
              <label key={g} className={`chip-check${form.genders.includes(g) ? ' is-on' : ''}`}>
                <input type="checkbox" checked={form.genders.includes(g)} onChange={() => toggleGender(g)} />
                {GENDER_LABEL[g]}
              </label>
            ))}
          </div>
        </fieldset>
        <div className="field">
          <label htmlFor="dist">Max distance (km, optional)</label>
          <input
            id="dist"
            className="input"
            type="number"
            min={1}
            max={20000}
            value={form.max_distance_km}
            onChange={(e) => setForm((f) => ({ ...f, max_distance_km: e.target.value }))}
          />
          <button
            type="button"
            className="btn btn-ghost"
            onClick={async () => {
              setErr('');
              try {
                await captureLocation();
              } catch (ex) {
                setErr(ex.message);
              }
            }}
          >
            {hasCoords ? 'Location saved for distance filter' : 'Use my location for distance'}
          </button>
        </div>
        <fieldset className="field">
          <legend className="form-label">Trait weights (optional, 0–5)</legend>
          {TRAIT_KEYS.map((k) => (
            <label key={k} className="weight-row">
              {TRAIT_LABEL[k]}
              <input
                type="range"
                min={0}
                max={5}
                step={0.5}
                value={form.trait_weights[k] ?? 1}
                onChange={(e) =>
                  setForm((f) => ({
                    ...f,
                    trait_weights: { ...f.trait_weights, [k]: e.target.value },
                  }))
                }
              />
            </label>
          ))}
        </fieldset>
        <button type="submit" className="btn btn-primary" disabled={saving || form.genders.length === 0}>
          {saving ? 'Saving…' : 'Continue'}
        </button>
      </form>
    </div>
  );
}
