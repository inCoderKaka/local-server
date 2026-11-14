import { act, render } from '@testing-library/react';
import type { FakeFetch } from '@local-server/shared/test';
import { App } from '../App';
import { collectPages, pages as realPages, type PageDef } from '../registry';
import { BASE } from './fixtures';

/** The two fixture pages under src/test/pages, found the same way the registry finds real pages. */
export const fixturePages = collectPages(import.meta.glob('./pages/*.page.tsx', { eager: true }));

/** The real pages plus the fixture pages. */
export const allPages: PageDef[] = collectPages({
  ...import.meta.glob('../pages/*.page.tsx', { eager: true }),
  ...import.meta.glob('./pages/*.page.tsx', { eager: true }),
});

export function renderApp(f: FakeFetch, opts: { pages?: PageDef[]; hash?: string } = {}) {
  window.location.hash = opts.hash ?? '';
  return render(<App pages={opts.pages ?? realPages} fetch={f.fetch} baseUrl={BASE} />);
}

const realSetTimeout = globalThis.setTimeout;

/** Lets pending fetches and renders finish, also while timers are faked. */
export async function settle(rounds = 15) {
  for (let i = 0; i < rounds; i++) {
    await act(async () => {
      await new Promise((r) => realSetTimeout(r, 0));
    });
  }
}
