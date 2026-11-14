import type { ComponentType } from 'react';

/**
 * One page of the admin UI. A page is a file `src/pages/<name>.page.tsx` that exports
 * `const page: PageDef`; the registry finds it, so adding a page edits no shared file.
 */
export interface PageDef {
  /** A unique id, also used as the React key. */
  id: string;
  /** The route, with params (`/consumers/:consumerId`); see useParams. */
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

/**
 * Collects the `page` export of each module of an import.meta.glob, sorted by order, then label.
 * A module without a valid `page`, or two pages with the same id or path, is a build mistake and
 * throws, so it fails the tests instead of hiding a page.
 */
export function collectPages(modules: Record<string, unknown>): PageDef[] {
  const pages: PageDef[] = [];
  for (const [file, mod] of Object.entries(modules)) {
    const page = (mod as { page?: unknown } | undefined)?.page;
    if (!isPageDef(page)) throw new Error(`${file} must export const page: PageDef`);
    const clash = pages.find((p) => p.id === page.id || p.path === page.path);
    if (clash) throw new Error(`${file}: page id or path clashes with page ${clash.id}`);
    pages.push(page);
  }
  return pages.sort((a, b) => a.order - b.order || a.label.localeCompare(b.label));
}

/** Every page of the admin UI: each src/pages/*.page.tsx file. */
export const pages: PageDef[] = collectPages(import.meta.glob('./pages/*.page.tsx', { eager: true }));
