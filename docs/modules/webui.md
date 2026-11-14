# webui module

## Purpose

Serves a built single-page web UI from a file system with a strict Content Security Policy, so both embedded UIs (the admin UI now, the consumer web page later) are served the same way. It is its own module because it owns the CSP and the caching and path rules, and knows nothing about either API.

## Owns

- Packages: `internal/webui/` (built by PLT-F-032).
- Tables: none.
- Port: none. `webui.Handler(webui.Config{Files})` returns an `http.Handler`; the caller mounts it behind its own guard.
- Behaviour:
  - Only `GET` and `HEAD`; any other method is `405` with `Allow: GET, HEAD`.
  - A file is served by its cleaned path with a fixed Content-Type per extension (not the OS's MIME tables). A path that is not a file and whose last segment has no extension gets `index.html` (client-side routes); a path with an extension that is not a file is `404`, so a stale asset name never loads HTML.
  - Never served: a directory listing, a dot file (`dist/.gitkeep`), a path with a `..` segment (also when sent as `%2e%2e`), a backslash or a NUL.
  - Every answer, `404` and `405` included, carries `Content-Security-Policy` (the constant `webui.CSP`: `default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'`), `X-Content-Type-Options: nosniff`, `Referrer-Policy: no-referrer` and `Cross-Origin-Resource-Policy: same-origin`, and no CORS header.
  - Files carry `Cache-Control: no-cache` and an `ETag` that is the SHA-256 of the content; `If-None-Match` is answered `304`. No cache lifetime is set (req_001 D-38).
  - When the file system has no `index.html` (the UI was not built), every page path gets the committed one-page stub `internal/webui/stub.html` with status `200`. It says how to build the UI (`make build`, `make web-admin-build`) and has no script or style.
- The built admin UI is embedded by package `adminui` in `web/admin/embed.go` (`//go:embed all:dist`); git holds only `web/admin/dist/.gitkeep` (req_001 D-152). `cmd/local-server` passes `adminui.Files()` to `webui.Handler` and the handler to `admin.Config.UI`.

## Imports

Only the standard library. No other module imports it except `cmd/local-server` (wiring) and the admin module's tests.

## Decisions

D-10, D-21, D-31, D-38, D-151, D-152.
