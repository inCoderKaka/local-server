import { describe, expect, it, vi } from 'vitest';
import { ApiError, call, createApiClient, isUnauthorized } from './client';
import { createFakeFetch, errorReply } from '../test/fakeFetch';

const base = 'http://admin.test';

describe('createApiClient and call', () => {
  it('returns the data of a 2xx answer', async () => {
    const f = createFakeFetch();
    f.on('GET', '/admin/v1/setup', { body: { admin_exists: true } });
    const c = createApiClient({ baseUrl: base, fetch: f.fetch });
    await expect(call(c.GET('/admin/v1/setup'))).resolves.toEqual({ admin_exists: true });
    expect(f.calls[0]?.path).toBe('/admin/v1/setup');
  });

  it('turns the error envelope and the request id into an ApiError', async () => {
    const f = createFakeFetch();
    f.on('POST', '/admin/v1/session', errorReply(429, 'rate_limit_exceeded', 'Too many failed logins. Try again later.', { type: 'requests' }));
    const c = createApiClient({ baseUrl: base, fetch: f.fetch });
    const err = await call(c.POST('/admin/v1/session', { body: { username: 'a', password: 'b' } })).catch((e) => e);
    expect(err).toBeInstanceOf(ApiError);
    expect(err).toMatchObject({ kind: 'http', status: 429, code: 'rate_limit_exceeded', type: 'requests', param: null, requestId: 'req-1' });
    expect(err.message).toBe('Too many failed logins. Try again later.');
  });

  it('gives a 401 its own kind and tells onUnauthorized', async () => {
    const f = createFakeFetch();
    f.on('GET', '/admin/v1/host', errorReply(401, null, 'Log in first: there is no valid admin session.'));
    const seen = vi.fn();
    const c = createApiClient({ baseUrl: base, fetch: f.fetch, onUnauthorized: seen });
    const err = await call(c.GET('/admin/v1/host')).catch((e) => e);
    expect(err.kind).toBe('unauthorized');
    expect(isUnauthorized(err)).toBe(true);
    expect(seen).toHaveBeenCalledTimes(1);
  });

  it('describes an answer without an envelope by its status', async () => {
    const f = createFakeFetch();
    f.on('GET', '/admin/v1/host', { status: 502, body: 'bad gateway' });
    const c = createApiClient({ baseUrl: base, fetch: f.fetch });
    const err = await call(c.GET('/admin/v1/host')).catch((e) => e);
    expect(err).toMatchObject({ kind: 'http', status: 502, code: null });
    expect(err.message).toContain('502');
  });

  it('makes a thrown fetch a network error', async () => {
    const c = createApiClient({ baseUrl: base, fetch: () => Promise.reject(new TypeError('Failed to fetch')) });
    const err = await call(c.GET('/admin/v1/host')).catch((e) => e);
    expect(err).toMatchObject({ kind: 'network', status: 0 });
  });

  it('accepts a 204 without a body', async () => {
    const f = createFakeFetch();
    f.on('DELETE', '/admin/v1/session', { status: 204 });
    const c = createApiClient({ baseUrl: base, fetch: f.fetch });
    await expect(call(c.DELETE('/admin/v1/session'))).resolves.toBeUndefined();
  });

  it('sends same-origin credentials and calls the page origin by default', async () => {
    let seen: Request | undefined;
    const c = createApiClient({
      baseUrl: base,
      fetch: async (r) => {
        seen = r;
        return new Response('{"admin_exists":false}', { headers: { 'Content-Type': 'application/json' } });
      },
    });
    await call(c.GET('/admin/v1/setup'));
    expect(seen?.credentials).toBe('same-origin');
    expect(seen?.headers.get('Accept')).toBe('application/json');
  });
});
