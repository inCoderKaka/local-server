import { useCallback, useEffect, useRef, useState, type DependencyList } from 'react';
import { asApiError, type ApiError } from '../api/client';

interface Page<T> {
  data: T[];
  next_cursor?: string | null;
}

export interface PagedList<T> {
  /** The items of every page read so far, in the server's order. */
  items: T[];
  /** The cursor of the next page, or null when there is none. */
  next: string | null;
  /** True once the first page arrived. */
  loaded: boolean;
  loading: boolean;
  error: ApiError | null;
  /** Reads the first page again and drops the later ones. */
  reload: () => Promise<void>;
  /** Reads the next page and adds it. */
  loadMore: () => Promise<void>;
  /** Replaces the item with the same id, for example with the answer of a write. */
  replace: (item: T) => void;
}

/**
 * A list the server pages with `next_cursor`. The page size is the server's (its page-size
 * setting caps and defaults it, req_001 D-38), so no `limit` is sent. When `deps` change the list
 * starts over; an answer that arrives after a newer call started is dropped.
 */
export function usePagedList<T extends { id: string }>(
  fetchPage: (cursor: string | undefined) => Promise<Page<T>>,
  deps: DependencyList,
): PagedList<T> {
  const [state, setState] = useState<{ items: T[]; next: string | null; loaded: boolean }>({ items: [], next: null, loaded: false });
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<ApiError | null>(null);
  const seq = useRef(0);
  const fetchRef = useRef(fetchPage);
  useEffect(() => {
    fetchRef.current = fetchPage;
  });

  const load = useCallback(async (cursor: string | undefined) => {
    const mine = ++seq.current;
    setLoading(true);
    setError(null);
    try {
      const page = await fetchRef.current(cursor);
      if (mine !== seq.current) return;
      setState((s) => ({ items: cursor ? [...s.items, ...page.data] : page.data, next: page.next_cursor ?? null, loaded: true }));
    } catch (e) {
      if (mine === seq.current) setError(asApiError(e));
    } finally {
      if (mine === seq.current) setLoading(false);
    }
  }, []);

  useEffect(() => {
    setState({ items: [], next: null, loaded: false });
    void load(undefined);
    return () => {
      seq.current++;
    };
    // deps are the caller's: the fetcher is read through a ref, so it is not one of them.
  }, [load, ...deps]);

  const reload = useCallback(() => load(undefined), [load]);
  const next = state.next;
  const loadMore = useCallback(() => (next ? load(next) : Promise.resolve()), [load, next]);
  const replace = useCallback((item: T) => setState((s) => ({ ...s, items: s.items.map((x) => (x.id === item.id ? item : x)) })), []);

  return { ...state, loading, error, reload, loadMore, replace };
}
