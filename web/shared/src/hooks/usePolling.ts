import { useEffect, useRef, type DependencyList } from 'react';
import { useApi, type ApiState } from './useApi';

/**
 * Calls `tick` every `intervalMs` milliseconds, waiting for one call to finish before timing the
 * next, so calls never overlap. Nothing runs while `intervalMs` is null or not above 0, and
 * nothing runs while the tab is hidden: when the tab is shown again `tick` runs at once and the
 * timing restarts. The interval always comes from a setting (req_001 D-38, D-157), never from a
 * number written in the UI.
 */
export function usePolling(tick: () => unknown, intervalMs: number | null): void {
  const tickRef = useRef(tick);
  useEffect(() => {
    tickRef.current = tick;
  });

  useEffect(() => {
    if (intervalMs === null || !(intervalMs > 0)) return;
    let timer: ReturnType<typeof setTimeout> | undefined;
    let stopped = false;
    let running = false;
    const schedule = () => {
      clearTimeout(timer);
      timer = undefined;
      if (!stopped && !document.hidden) timer = setTimeout(run, intervalMs);
    };
    const run = async () => {
      if (running) return;
      running = true;
      try {
        await tickRef.current();
      } catch {
        // The caller keeps its own error state; polling goes on.
      } finally {
        running = false;
        schedule();
      }
    };
    const onVisibility = () => {
      if (document.hidden) {
        clearTimeout(timer);
        timer = undefined;
      } else {
        void run();
      }
    };
    document.addEventListener('visibilitychange', onVisibility);
    schedule();
    return () => {
      stopped = true;
      clearTimeout(timer);
      document.removeEventListener('visibilitychange', onVisibility);
    };
  }, [intervalMs]);
}

/** useApi that also refetches every `intervalMs` (see usePolling). */
export function usePolledApi<T>(
  fetcher: () => Promise<T>,
  deps: DependencyList,
  intervalMs: number | null,
  opts: { enabled?: boolean } = {},
): ApiState<T> {
  const state = useApi(fetcher, deps, opts);
  usePolling(state.refetch, opts.enabled === false ? null : intervalMs);
  return state;
}
