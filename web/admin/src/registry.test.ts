import { describe, expect, it } from 'vitest';
import { collectPages, pages } from './registry';
import { fixturePages } from './test/render';

const C = () => null;

describe('page registry', () => {
  // Later pages add files only; this test does not list them.
  it('ships the dashboard at / first, and every page once', () => {
    expect(pages[0]).toMatchObject({ id: 'dashboard', path: '/' });
    expect(new Set(pages.map((p) => p.id)).size).toBe(pages.length);
    expect(new Set(pages.map((p) => p.path)).size).toBe(pages.length);
  });

  it('finds page files by glob and sorts them by order', () => {
    expect(fixturePages.map((p) => p.id)).toEqual(['fake-c', 'fake-b', 'fake-a']);
  });

  it('sorts equal orders by label', () => {
    const got = collectPages({
      'z.page.tsx': { page: { id: 'z', path: '/z', label: 'Zeta', order: 1, element: C } },
      'a.page.tsx': { page: { id: 'a', path: '/a', label: 'Alpha', order: 1, element: C } },
      'm.page.tsx': { page: { id: 'm', path: '/m', label: 'Mu', order: 0, element: C } },
    });
    expect(got.map((p) => p.id)).toEqual(['m', 'a', 'z']);
  });

  it('refuses a page file without a valid page and two pages with the same id or path', () => {
    expect(() => collectPages({ 'x.page.tsx': {} })).toThrow(/x.page.tsx must export const page/);
    expect(() => collectPages({ 'x.page.tsx': { page: { id: 'x', path: 'no-slash', label: 'X', order: 0, element: C } } })).toThrow();
    expect(() =>
      collectPages({
        'a.page.tsx': { page: { id: 'a', path: '/a', label: 'A', order: 0, element: C } },
        'b.page.tsx': { page: { id: 'a', path: '/b', label: 'B', order: 0, element: C } },
      }),
    ).toThrow(/clashes/);
    expect(() =>
      collectPages({
        'a.page.tsx': { page: { id: 'a', path: '/a', label: 'A', order: 0, element: C } },
        'b.page.tsx': { page: { id: 'b', path: '/a', label: 'B', order: 0, element: C } },
      }),
    ).toThrow(/clashes/);
  });
});
