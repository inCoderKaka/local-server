# Admin UI (developer note)

React + Vite + TypeScript, served by the admin listener from `web/admin/dist` (embedded by `embed.go`). Build and check it from the repository root with `make web-admin-test`; `make build` builds it into the binary.

## Add a page

1. Create `src/pages/<name>.page.tsx`. Nothing else is edited: the registry (`src/registry.ts`) finds every `*.page.tsx` file with `import.meta.glob`.
2. Export the page: `export const page: PageDef = { id: 'consumers', path: '/consumers', label: 'Consumers', order: 10, element: lazy(() => import('../consumers/ConsumersPage')) };`
3. `order` sets the position in the navigation (dashboard is 0); `navVisible: false` hides a detail page such as `/consumers/:consumerId`.
4. Read route params with `useParams()` and link with `<Link to="/consumers/abc">` (hash routes: `#/consumers/abc`).
5. Put the page's components in their own folder (`src/consumers/`) and its tests next to them (`*.test.tsx`).

The layout (navigation, sharing state, pending requests count, resource strip) wraps every page; a page renders only its own content.

## What a page uses

- From `@local-server/shared`: `useClient()` (the typed client), `call(client.GET('/admin/v1/...'))` (data or a thrown `ApiError` with `status`, `code`, `param`, `requestId`), `useApi(fetcher, deps)` (`data`, `error`, `loading`, `refetch`, `setData`), `usePolledApi`, `usePolling`, the router (`Link`, `navigate`, `useParams`, `useHashLocation`), the components (`Button`, `Card`, `Badge`, `Field`, `CheckboxField`, `Table`, `ErrorBanner`, `Spinner`, `UserText`) and the formatters (`formatBytes`, `formatPercent`, `formatRelativeTime`, `formatDateTime`; `null` is `n/a`).
- From this workspace: `useShell()` (the polled host state and host metrics), `useSettings()` (the host settings, `replace(list)` after a `PATCH`), `usePollInterval()` (from `admin.ui_poll_interval`), `useSession()`.
- Text a consumer wrote (names, comments, request messages) is rendered with `<UserText text={...} />` or as a plain React text node, never as HTML. A test fails on HTML sinks anywhere in `web/*/src`.
- No time, interval or limit is written into the UI: read it from a setting (req_001 D-38).
- A 401 anywhere returns to the login page by itself; a page shows other errors with `<ErrorBanner error={...} />`.

## Tests

Vitest + Testing Library in jsdom. `createFakeFetch()` from `@local-server/shared/test` fakes the API by schema path; `src/test/fixtures.ts` has a logged-in host and `src/test/render.tsx` renders the app (`renderApp(fake, { pages, hash })`). Query by role and label, as the end-to-end tests will.
