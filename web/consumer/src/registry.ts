import type { ComponentType } from 'react';

/**
 * One page of the consumer web page after login. A page is a file `src/pages/<name>.page.tsx`
 * that exports `const page: PageDef`; the registry finds it, so adding a page (limits, usage,
 * keys, requests, settings) edits no shared file. Sign-up and login are not pages of the
 * registry: the shell shows them while nobody is logged in.
 */
export interface PageDef {
  /** A unique id, also used as the React key. */
  id: string;
  /** The route, with params (`/requests/:requestId`); see useParams. */
  path: string;
  /** The name in the navigation and the document title. */
  label: string;
  /** Position in the navigation, lowest first. */
  order: number;
  /** The page component; use React.lazy(() => import(...)) to load it on first visit. */
  element: ComponentType;
  /** False keeps the page out of the navigation (a detail page reached by a link). Default true. */
  navVisible?: boolean;
}

function isPageDef(v: unknown): v is PageDef {
  if (!v || typeof v !== 'object') return false;
  const p = v as Record<string, unknown>;
  return (
    typeof p.id === 'string' &&
    typeof p.path === 'string' &&
    p.path.startsWith('/') &&
    typeof p.label === 'string' &&
    typeof p.order === 'number' &&
    (typeof p.element === 'function' || (typeof p.element === 'object' && p.element !== null))
  );
}

/** The routes the shell keeps for itself: the sign-up and login forms. No page may take them. */
export const shellPaths = ['/login', '/signup'] as const;

/**
 * Collects the `page` export of each module of an import.meta.glob, sorted by order, then label.
 * A module without a valid `page`, two pages with the same id or path, or a page on a shell
 * route is a build mistake and throws, so it fails the tests instead of hiding a page.
 */
export function collectPages(modules: Record<string, unknown>): PageDef[] {
  const pages: PageDef[] = [];
  for (const [file, mod] of Object.entries(modules)) {
    const page = (mod as { page?: unknown } | undefined)?.page;
    if (!isPageDef(page)) throw new Error(`${file} must export const page: PageDef`);
    if ((shellPaths as readonly string[]).includes(page.path)) throw new Error(`${file}: ${page.path} is a route of the shell`);
    const clash = pages.find((p) => p.id === page.id || p.path === page.path);
    if (clash) throw new Error(`${file}: page id or path clashes with page ${clash.id}`);
    pages.push(page);
  }
  return pages.sort((a, b) => a.order - b.order || a.label.localeCompare(b.label));
}

/** Every page of the consumer web page: each src/pages/*.page.tsx file. */
export const pages: PageDef[] = collectPages(import.meta.glob('./pages/*.page.tsx', { eager: true }));
