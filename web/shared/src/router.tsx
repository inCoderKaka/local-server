import { createContext, useContext, useMemo, useSyncExternalStore, type AnchorHTMLAttributes } from 'react';

/**
 * A minimal hash router: the route is the part of the URL after `#` (`#/consumers/abc?x=1`), so
 * the server only ever serves one page and a reload keeps the route.
 */
export interface HashLocation {
  /** The route path, always starting with `/`, without the query. */
  path: string;
  /** The query after `?` in the hash. */
  query: URLSearchParams;
}

function subscribe(onChange: () => void) {
  window.addEventListener('hashchange', onChange);
  return () => window.removeEventListener('hashchange', onChange);
}

function snapshot() {
  return window.location.hash;
}

/** Parses a hash (`#/a/b?x=1`) into a location. */
export function parseHash(hash: string): HashLocation {
  let s = hash.startsWith('#') ? hash.slice(1) : hash;
  const q = s.indexOf('?');
  const query = new URLSearchParams(q >= 0 ? s.slice(q + 1) : '');
  if (q >= 0) s = s.slice(0, q);
  if (!s.startsWith('/')) s = '/' + s;
  return { path: s, query };
}

/** The current hash location; the component re-renders when it changes. */
export function useHashLocation(): HashLocation {
  const hash = useSyncExternalStore(subscribe, snapshot, () => '');
  return useMemo(() => parseHash(hash), [hash]);
}

function segments(p: string): string[] {
  return p.split('/').filter((s) => s !== '');
}

/**
 * Matches a route pattern (`/consumers/:consumerId`) against a path and returns its decoded
 * params, or null. A trailing slash does not matter.
 */
export function matchPath(pattern: string, path: string): Record<string, string> | null {
  const ps = segments(pattern);
  const xs = segments(path);
  if (ps.length !== xs.length) return null;
  const params: Record<string, string> = {};
  for (let i = 0; i < ps.length; i++) {
    const p = ps[i]!;
    const x = xs[i]!;
    if (p.startsWith(':')) {
      try {
        params[p.slice(1)] = decodeURIComponent(x);
      } catch {
        return null;
      }
    } else if (p !== x) {
      return null;
    }
  }
  return params;
}

/** The first route whose path matches, with its params. */
export function matchRoute<R extends { path: string }>(routes: readonly R[], path: string): { route: R; params: Record<string, string> } | null {
  for (const route of routes) {
    const params = matchPath(route.path, path);
    if (params) return { route, params };
  }
  return null;
}

/** The href of a route path. */
export function hrefFor(to: string): string {
  return '#' + (to.startsWith('/') ? to : '/' + to);
}

/** Goes to a route path. */
export function navigate(to: string): void {
  window.location.hash = hrefFor(to).slice(1);
}

/** A link to a route path; it is marked `aria-current="page"` when it is the current route. */
export function Link({ to, ...rest }: Omit<AnchorHTMLAttributes<HTMLAnchorElement>, 'href'> & { to: string }) {
  const { path } = useHashLocation();
  const current = matchPath(to, path) !== null;
  return <a href={hrefFor(to)} aria-current={current ? 'page' : undefined} {...rest} />;
}

const ParamsContext = createContext<Record<string, string>>({});

/** Provides the matched params to a page. */
export const ParamsProvider = ParamsContext.Provider;

/** The params of the current route (`:consumerId` → `useParams().consumerId`). */
export function useParams(): Record<string, string> {
  return useContext(ParamsContext);
}
