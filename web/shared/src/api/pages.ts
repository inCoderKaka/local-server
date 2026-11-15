/** One page of a list the API pages with `next_cursor`. */
export interface CursorPage<T> {
  data: T[];
  next_cursor?: string | null;
}

/**
 * Reads every page of a list that pages with next_cursor; the page size is the server's (its
 * page-size setting caps and defaults it, req_001 D-38), so no `limit` is sent. A cursor seen
 * before ends the read, so a server that repeats one cannot loop it forever.
 */
export async function readAll<T>(page: (cursor: string | undefined) => Promise<CursorPage<T>>): Promise<T[]> {
  const out: T[] = [];
  const seen = new Set<string>();
  let cursor: string | undefined;
  do {
    const p = await page(cursor);
    out.push(...p.data);
    cursor = p.next_cursor ?? undefined;
    if (cursor !== undefined) {
      if (seen.has(cursor)) break;
      seen.add(cursor);
    }
  } while (cursor);
  return out;
}
