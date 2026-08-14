import { useState } from 'react';
import { media, profile as profileApi, errorCode } from '../api';

const MAX_PHOTOS = 9;

function putFile(url, file, headers, onProgress) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open('PUT', url);
    Object.entries(headers || {}).forEach(([k, v]) => xhr.setRequestHeader(k, v));
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable && onProgress) onProgress(Math.round((e.loaded / e.total) * 100));
    };
    xhr.onload = () =>
      xhr.status >= 200 && xhr.status < 300
        ? resolve()
        : reject(new Error(`Upload failed (${xhr.status})`));
    xhr.onerror = () => reject(new Error('Upload failed'));
    xhr.send(file);
  });
}

async function waitReady(assetId, onStatus) {
  for (let i = 0; i < 20; i += 1) {
    const res = await media.get(assetId);
    const status = res.data.status;
    if (onStatus) onStatus(status);
    if (status === 'ready') return res.data;
    if (status === 'failed') throw new Error(res.data.error || 'Processing failed');
    await new Promise((r) => setTimeout(r, 1500));
  }
  throw new Error('Photo is still processing. Try again in a moment.');
}

export default function PhotoUploader({ photos = [], onChange, max = MAX_PHOTOS }) {
  const [progress, setProgress] = useState(0);
  const [status, setStatus] = useState('');
  const [error, setError] = useState('');
  const [urlDraft, setUrlDraft] = useState('');
  const [useUrl, setUseUrl] = useState(false);
  const [busy, setBusy] = useState(false);
  const [assetIds, setAssetIds] = useState([]);
  const [lightbox, setLightbox] = useState(null);

  async function persist(nextUrls, nextIds) {
    const trimmed = nextUrls.filter(Boolean).slice(0, max);
    const ids = (nextIds || []).slice(0, trimmed.length);
    if (ids.length === trimmed.length && ids.every(Boolean)) {
      await profileApi.setPhotos({ asset_ids: ids });
    } else {
      await profileApi.setPhotos({ photo_urls: trimmed });
    }
    setAssetIds(ids);
    onChange?.(trimmed);
    return trimmed;
  }

  async function handleFile(e) {
    const file = e.target.files?.[0];
    e.target.value = '';
    if (!file || photos.length >= max) return;
    setBusy(true);
    setError('');
    setProgress(0);
    setStatus('Requesting upload…');
    try {
      const signed = await media.presign(file.type || 'image/jpeg', file.size);
      const { upload_url, asset_id, headers } = signed.data;
      setStatus('Uploading…');
      await putFile(upload_url, file, headers, setProgress);
      setStatus('Processing…');
      await media.complete(asset_id);
      const ready = await waitReady(asset_id, (s) => setStatus(`Processing (${s})…`));
      const url = ready.thumb_url || ready.original_url || '';
      await persist([...photos, url], [...assetIds, asset_id]);
      setStatus('Saved');
    } catch (err) {
      if (errorCode(err) === 'internal_error' || err.response?.status === 503) {
        setUseUrl(true);
        setError('File upload is not available on this server. Paste an HTTPS image URL instead.');
      } else {
        setError(err.message || 'Upload failed');
      }
    } finally {
      setBusy(false);
    }
  }

  async function saveUrl(e) {
    e.preventDefault();
    const trimmed = urlDraft.trim();
    if (!trimmed || photos.length >= max) return;
    setBusy(true);
    setError('');
    try {
      await persist([...photos, trimmed], [...assetIds, null]);
      setUrlDraft('');
      setStatus('Saved');
    } catch (err) {
      setError(err.message || 'Could not save photo');
    } finally {
      setBusy(false);
    }
  }

  async function removeAt(i) {
    setBusy(true);
    setError('');
    try {
      await persist(
        photos.filter((_, idx) => idx !== i),
        assetIds.filter((_, idx) => idx !== i)
      );
      setStatus('Saved');
    } catch (err) {
      setError(err.message || 'Could not update photos');
    } finally {
      setBusy(false);
    }
  }

  async function move(i, dir) {
    const j = i + dir;
    if (j < 0 || j >= photos.length) return;
    const next = [...photos];
    [next[i], next[j]] = [next[j], next[i]];
    const nextIds = [...assetIds];
    [nextIds[i], nextIds[j]] = [nextIds[j], nextIds[i]];
    setBusy(true);
    try {
      await persist(next, nextIds);
    } catch (err) {
      setError(err.message || 'Could not reorder photos');
    } finally {
      setBusy(false);
    }
  }

  const canAdd = photos.length < max && !busy;

  return (
    <div className="photo-uploader">
      {photos.length > 0 && (
        <ul className="photo-gallery">
          {photos.map((url, i) => (
            <li key={`${url}-${i}`} className="photo-gallery-item">
              <img src={url} alt={`Photo ${i + 1}`} width={160} height={160} loading="lazy" onClick={() => setLightbox(url)} />
              {i === 0 && <span className="photo-primary">Primary</span>}
              <div className="photo-gallery-tools">
                <button type="button" className="btn-icon" disabled={busy || i === 0} onClick={() => move(i, -1)} aria-label="Move left">
                  ‹
                </button>
                <button type="button" className="btn-icon" disabled={busy || i === photos.length - 1} onClick={() => move(i, 1)} aria-label="Move right">
                  ›
                </button>
                <button type="button" className="btn-icon" disabled={busy} onClick={() => removeAt(i)} aria-label="Remove photo">
                  ×
                </button>
              </div>
            </li>
          ))}
        </ul>
      )}

      {canAdd && !useUrl && (
        <div className="field">
          <label className="form-label" htmlFor="photo-file">
            {photos.length ? `Add another photo (${photos.length}/${max})` : 'Upload a photo'}
          </label>
          <input
            id="photo-file"
            type="file"
            accept="image/jpeg,image/png,image/webp"
            onChange={handleFile}
            disabled={busy}
          />
          {busy && (
            <div className="upload-progress" aria-valuenow={progress} aria-valuemin={0} aria-valuemax={100}>
              <div className="upload-progress-bar" style={{ width: `${progress}%` }} />
              <span>{status} {progress ? `${progress}%` : ''}</span>
            </div>
          )}
          <button type="button" className="btn btn-ghost" onClick={() => setUseUrl(true)}>
            Use a photo URL instead
          </button>
        </div>
      )}

      {canAdd && useUrl && (
        <form onSubmit={saveUrl}>
          <div className="field">
            <label className="form-label" htmlFor="photo-url">Photo URL</label>
            <input
              id="photo-url"
              type="url"
              className="input"
              placeholder="https://…"
              value={urlDraft}
              onChange={(e) => setUrlDraft(e.target.value)}
            />
          </div>
          <button type="submit" className="btn btn-primary" disabled={busy || !urlDraft.trim()}>
            {busy ? 'Saving…' : 'Add photo'}
          </button>
        </form>
      )}

      {error && <div className="alert alert-error" role="alert">{error}</div>}
      {!error && status === 'Saved' && (
        <div className="alert alert-success" role="status">Photos saved</div>
      )}
      {lightbox && (
        <button type="button" className="lightbox" onClick={() => setLightbox(null)} aria-label="Close photo">
          <img src={lightbox} alt="" />
        </button>
      )}
    </div>
  );
}
