import { describe, expect, it } from 'vitest';
import { readAll } from './pages';

describe('readAll', () => {
  it('follows next_cursor until it is null, sending no limit', async () => {
    const asked: (string | undefined)[] = [];
    const pages: Record<string, { data: number[]; next_cursor: string | null }> = {
      first: { data: [1, 2], next_cursor: 'b' },
      b: { data: [3], next_cursor: null },
    };
    const out = await readAll<number>(async (cursor) => {
      asked.push(cursor);
      return pages[cursor ?? 'first']!;
    });
    expect(out).toEqual([1, 2, 3]);
    expect(asked).toEqual([undefined, 'b']);
  });

  it('stops at a cursor it has seen before', async () => {
    let n = 0;
    const out = await readAll<number>(async () => ({ data: [++n], next_cursor: 'same' }));
    expect(out).toEqual([1, 2]);
  });
});
