# Consumer web page (developer note)

React + Vite + TypeScript, served by the consumer page listener from `web/consumer/dist` (embedded by `embed.go`, package `consumerui`). Build and check it from the repository root with `make web-consumer-test`; `make build` builds it into the binary.

## The shell

- Nobody logged in: `#/signup` is the invite sign-up, every page route (and `#/login`) is the login form, any other route is "Page not found". After login the page route that was asked for opens.
- Logged in: the layout (navigation of the registered pages, the consumer's name, Log out) wraps the page; `#/login` and `#/signup` go to `#/`. A 401 from any call returns to the login form.
- An invite link is `<page address>/invite#<token>`. `src/lib/invite.ts` reads the token once at start, replaces the address with `/#/signup` and keeps the token in memory only.
- Passkeys: `src/lib/webauthn.ts` turns the begin routes' options (base64url) into what `navigator.credentials` takes and its answer back into JSON. Every option, the timeout included, is the host's.
- The Terms of Use and the Privacy notice shown at sign-up are `docs/legal/*.md`, built in as text; the consent sends their `**Version:**` ids, and a test checks those equal the host's `TermsVersion` and `PrivacyVersion`.

## The pages

| Page | Route | Reads and writes |
|---|---|---|
| Home | `#/` | a summary: the limits in one line, what is used now, the count of pending requests |
| My limits | `#/limits` | `GET /v1/me/limits` (a 404 is "no access yet", req_001 D-150) |
| My usage | `#/usage` | `GET /v1/usage`: use of each current quota window, then a range per model as a chart (uPlot) and a table |
| My keys | `#/keys` | `GET /v1/me/access-keys`, read only |
| Requests | `#/requests`, `#/requests/<id>` | `GET`/`POST /v1/resource-requests`, one request with its comments, comment, withdraw |
| Settings | `#/settings` | `GET`/`PATCH /v1/me/settings`, `PUT /v1/me/password`, `/v1/me/passkeys/begin` and `/finish` |

- A quota window is counted the way the host counts it: fixed windows of their length from 0001-01-01 00:00 UTC (`src/lib/usage.ts`, `quotaWindow`), summed from the usage rollups in buckets that start at the window's start.
- The few display choices the API leaves to the page (the summary period of a grant without a quota, the chart colours) are in `src/lib/constants.ts`; the usage ranges are the shared `RANGES`. The pages do not poll: the host has no consumer-readable poll interval, so each page reads on open and has a Refresh button.
- Lists page with the host's page size (`usePagedList`, `readAll` from `@local-server/shared`); no `limit` is sent.

## Add a page

1. Create `src/pages/<name>.page.tsx` exporting `const page: PageDef` (`id`, `path`, `label`, `order`, `element`, optional `navVisible: false`). The registry (`src/registry.ts`) finds it; nothing else is edited. `/login` and `/signup` belong to the shell.
2. The start page is `home` at `/` (order 0).
3. Use `useSession()` (the consumer, `logout`) and, from `@local-server/shared`, `useClient()`, `call(...)`, `useApi`, the router and the components, as in the admin UI (`web/admin/README.md`).
4. Text a person wrote is rendered with `<UserText>` or as a plain text node, never as HTML. No time, interval or limit is written into the page: it comes from the host (req_001 D-38).

## Tests

Vitest + Testing Library in jsdom. `src/test/fixtures.ts` has a logged-out and a logged-in fake host (the logged-in one answers every page's reads: a grant, no usage, no requests, no keys, two settings) and builders of the API objects, `src/test/render.tsx` renders the app (`renderApp(fake, { pages, hash, invite })`) and `src/test/passkeys.ts` installs a fake `navigator.credentials`.
