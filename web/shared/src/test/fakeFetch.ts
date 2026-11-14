import type { paths } from '../api/schema.gen';

/** What a fake route answers: a status (200 by default), a JSON body and extra headers. */
export interface FakeReply {
  status?: number;
  body?: unknown;
  headers?: Record<string, string>;
}

export interface FakeCall {
  method: string;
  path: string;
  query: URLSearchParams;
  body: unknown;
}

type Method = 'GET' | 'POST' | 'PUT' | 'PATCH' | 'DELETE';
type Responder = FakeReply | ((call: FakeCall) => FakeReply | Promise<FakeReply>);

interface Route {
  method: Method;
  pattern: RegExp;
  reply: Responder;
}

/** The error envelope the API answers with (api/openapi.yaml, Error). */
export function errorReply(status: number, code: string | null, message: string, extra: { param?: string | null; type?: string } = {}): FakeReply {
  return {
    status,
    body: { error: { message, type: extra.type ?? 'invalid_request_error', param: extra.param ?? null, code } },
  };
}

/**
 * A tiny fake of the local-server API for component tests: routes are keyed by method and a path
 * of the generated schema (`/admin/v1/consumers/{consumer_id}`), the last registration of a route
 * wins, and every call is recorded. A call with no route answers a 404 envelope and is listed in
 * `unmatched`. Each answer carries an X-Request-Id.
 */
export function createFakeFetch() {
  const routes: Route[] = [];
  const calls: FakeCall[] = [];
  const unmatched: string[] = [];
  let n = 0;

  function on<P extends keyof paths & string>(method: Method, path: P, reply: Responder) {
    const re = '^' + path.replace(/[.*+?^$()|[\]\\]/g, '\\$&').replace(/\{[^}]+\}/g, '[^/]+') + '$';
    routes.unshift({ method, pattern: new RegExp(re), reply });
  }

  async function fetch(req: Request): Promise<Response> {
    const url = new URL(req.url);
    const text = req.method === 'GET' || req.method === 'HEAD' ? '' : await req.text();
    let body: unknown = undefined;
    if (text) {
      try {
        body = JSON.parse(text);
      } catch {
        body = text;
      }
    }
    const call: FakeCall = { method: req.method, path: url.pathname, query: url.searchParams, body };
    calls.push(call);
    const route = routes.find((r) => r.method === req.method && r.pattern.test(url.pathname));
    let reply: FakeReply;
    if (!route) {
      unmatched.push(`${req.method} ${url.pathname}`);
      reply = errorReply(404, 'not_found', 'No fake route.');
    } else {
      reply = typeof route.reply === 'function' ? await route.reply(call) : route.reply;
    }
    const status = reply.status ?? 200;
    const headers = new Headers({ 'X-Request-Id': `req-${++n}`, ...reply.headers });
    if (status === 204 || reply.body === undefined) return new Response(null, { status, headers });
    headers.set('Content-Type', 'application/json');
    return new Response(JSON.stringify(reply.body), { status, headers });
  }

  /** The calls of one method and path (exact, without the query). */
  function callsTo(method: Method, path: string): FakeCall[] {
    return calls.filter((c) => c.method === method && c.path === path);
  }

  return { on, fetch, calls, unmatched, callsTo };
}

export type FakeFetch = ReturnType<typeof createFakeFetch>;
