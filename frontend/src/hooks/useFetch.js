import { useState, useEffect, useCallback } from 'react';

/**
 * Shared read path for pages. Unlike the old per-page `.catch(() => setEmpty())`,
 * a rejected request lands in `error` instead of masquerading as an empty result,
 * so callers can tell "nothing here yet" apart from "the request failed".
 *
 * `fn` is called on mount and whenever `deps` change; `retry()` re-runs it.
 */
export default function useFetch(fn, deps = []) {
  const [state, setState] = useState({ data: null, error: null, loading: true });
  const [attempt, setAttempt] = useState(0);

  useEffect(() => {
    let alive = true;
    setState({ data: null, error: null, loading: true });
    fn()
      .then((res) => alive && setState({ data: res.data, error: null, loading: false }))
      .catch((error) => alive && setState({ data: null, error, loading: false }));
    return () => {
      alive = false;
    };
    // `fn` is intentionally excluded — callers pass an inline arrow, so including
    // it would re-fire every render. Anything it closes over belongs in `deps`.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...deps, attempt]);

  return { ...state, retry: useCallback(() => setAttempt((n) => n + 1), []) };
}
