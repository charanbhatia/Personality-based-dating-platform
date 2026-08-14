import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import { useQuery } from '@tanstack/react-query';
import { useAuth } from '../../context/AuthContext';
import { photoOf } from '../../lib/compat';
import { profile as profileApi, qk } from '../../api';
import PhotoUploader from '../../components/PhotoUploader';
import Avatar from '../../components/Avatar';
import Loading from '../../components/Loading';

function urlsOf(data) {
  if (!data) return [];
  if (data.photo_urls?.length) return data.photo_urls;
  return photoOf(data) ? [photoOf(data)] : [];
}

export default function Photos() {
  const navigate = useNavigate();
  const { user, refreshUser } = useAuth();
  const { data, isPending } = useQuery({
    queryKey: qk.profile,
    queryFn: () => profileApi.get().then((r) => r.data),
  });
  const [local, setLocal] = useState(null);
  const photos = local ?? urlsOf(data);

  async function finish() {
    await refreshUser();
    navigate('/app');
  }

  if (isPending) return <Loading text="Loading photos…" />;

  return (
    <div className="page">
      <h1>Add photos</h1>
      <p>Add at least one photo so people can recognise you. Up to 9, first is primary.</p>
      <Avatar
        name={user?.name}
        src={photos[0] || undefined}
        seed={user?.id}
        size={120}
        ring
      />
      <PhotoUploader
        photos={photos}
        onChange={async (urls) => {
          setLocal(urls);
          await refreshUser();
        }}
      />
      <div className="match-actions" style={{ marginTop: '1.5rem' }}>
        <button type="button" className="btn btn-primary" disabled={photos.length === 0} onClick={finish}>
          Enter Kindred
        </button>
      </div>
    </div>
  );
}
