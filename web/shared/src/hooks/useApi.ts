import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react';
import { ApiError, asApiError } from '../api/client';

export interface ApiState<T> {
  /** The last data that arrived; kept while a refetch runs or after it fails. */
  data: T | undefined;
  /** The error of the last call, or null after a success. */
  error: ApiError | null;
  /** True while a call runs. */
  loading: boolean;
  /** Runs the call again. */
  refetch: () => Promise<void>;
  /** Replaces the data, for example with the answer of a write. */
  setData: (data: T) => void;
}

/**
 * Runs an API call when the component mounts and whenever `deps` change, and keeps its data,
 * error and loading state. The answer of an older call that comes back after a newer one started
 * is dropped. With `enabled: false` nothing runs until it turns true.
 */
export function useApi<T>(fetcher: () => Promise<T>, deps: DependencyList, opts: { enabled?: boolean } = {}): ApiState<T> {
  const enabled = opts.enabled ?? true;
  const [state, setState] = useState<{ data: T | undefined; error: ApiError | null; loading: boolean }>({
    data: undefined,
    error: null,
    loading: enabled,
  });
  const fetcherRef = useRef(fetcher);
  const seq = useRef(0);
  useEffect(() => {
    fetcherRef.current = fetcher;
  });

  const refetch = useCallback(async () => {
    const mine = ++seq.current;
    setState((s) => (s.loading ? s : { ...s, loading: true }));
    try {
      const data = await fetcherRef.current();
      if (mine === seq.current) setState({ data, error: null, loading: false });
    } catch (e) {
      if (mine === seq.current) setState((s) => ({ data: s.data, error: asApiError(e), loading: false }));
    }
  }, []);

  const setData = useCallback((data: T) => {
    seq.current++;
    setState({ data, error: null, loading: false });
  }, []);

  useEffect(() => {
    if (enabled) void refetch();
    return () => {
      // Drop answers that arrive after the deps changed or the component left.
      seq.current++;
    };
    // deps are the caller's: the fetcher is read through a ref, so it is not one of them.
  }, [enabled, refetch, ...deps]);

  return { ...state, refetch, setData };
}
