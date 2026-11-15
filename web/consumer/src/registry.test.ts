import { describe, expect, it } from 'vitest';
import { collectPages, pages } from './registry';
import { fixturePages } from './test/render';

const C = () => null;

describe('page registry', () => {
  // Later pages add files only; this test does not list them.
  it('ships the start page at / first, and every page once', () => {
    expect(pages[0]).toMatchObject({ id: 'home', path: '/' });
    expect(new Set(pages.map((p) => p.id)).size).toBe(pages.length);
    expect(new Set(pages.map((p) => p.path)).size).toBe(pages.length);
  });

  it('finds page files by glob and sorts them by order', () => {
    expect(fixturePages.map((p) => p.id)).toEqual(['fake-b', 'fake-a']);
  });

  it('refuses a page without a valid page, a clash and a shell route', () => {
    expect(() => collectPages({ 'x.page.tsx': {} })).toThrow(/x.page.tsx must export const page/);
    expect(() =>
      collectPages({
        'a.page.tsx': { page: { id: 'a', path: '/a', label: 'A', order: 0, element: C } },
        'b.page.tsx': { page: { id: 'a', path: '/b', label: 'B', order: 0, element: C } },
      }),
    ).toThrow(/clashes/);
    for (const path of ['/login', '/signup']) {
      expect(() => collectPages({ 'x.page.tsx': { page: { id: 'x', path, label: 'X', order: 0, element: C } } })).toThrow(/route of the shell/);
    }
  });
});
