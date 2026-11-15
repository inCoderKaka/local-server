import { ApiError, call, type ApiClient } from '@local-server/shared';

/** The route of the resource requests page (PLT-F-035). */
export const REQUESTS_PATH = '/requests';

/** The count of open resource requests, or `unavailable` while the host does not have them yet. */
export type PendingCount = { kind: 'count'; n: number } | { kind: 'unavailable' };

/**
 * Counts the open resource requests: GET /admin/v1/resource-requests?status=open, following
 * next_cursor until it is null (req_001 D-157). A host without resource requests answers 501,
 * which is `unavailable`, not an error (req_001 D-160). The shell badge and the requests page
 * show this one count.
 */
export async function countOpenRequests(client: ApiClient): Promise<PendingCount> {
  let n = 0;
  let cursor: string | undefined;
  const seen = new Set<string>();
  for (;;) {
    let page;
    try {
      page = await call(
        client.GET('/admin/v1/resource-requests', { params: { query: cursor ? { status: 'open', cursor } : { status: 'open' } } }),
      );
    } catch (e) {
      if (e instanceof ApiError && e.status === 501) return { kind: 'unavailable' };
      throw e;
    }
    n += page.data.length;
    const next = page.next_cursor;
    // A cursor seen before would loop forever; the count so far is all there is.
    if (!next || seen.has(next)) return { kind: 'count', n };
    seen.add(next);
    cursor = next;
  }
}
