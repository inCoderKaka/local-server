# requests module

## Purpose

Resource requests from consumers to the host and their comments.

## Owns

- Packages: `internal/requests/` (built in PLT-F-028).
- Tables: `req_resource_request`, `req_comment`; migrations in `internal/requests/migrations/`. The request keeps the consumer's own text (`message`, `ask`, comment `body`), classified `personal`; it never holds a prompt or a response.
- Port: none defined here. The module defines one function type, `ApplyGrant`, that the admin module passes to `Service.Approve`: it makes the new grant version inside the unit of work that approves the request. The requests module therefore imports no admin code and repeats none of its grant checks.
- Settings, all host-owned and read when used: `requests.ttl` (default 336h, 14 days), `requests.max_open_per_consumer` (5), `requests.max_comment_chars` (4,000; also the cap of a request's message), `requests.sweep_interval` (1m) and `requests.page_size` (50).

## Behaviour

- A request is `open` until it is `approved`, `rejected`, `withdrawn` or `expired`; the other four states are final. A comment, a withdrawal or a decision on a final request is a conflict (HTTP 409).
- Create, every change of state, every comment and the sweeper's expiry are each one unit of work through the events Notifier. A decision also writes its audit row (`resource_request.approved` or `resource_request.rejected`), the `resource_request.decided` event and a `syn_change` row (dataset `resource_requests`: ids, state and times, never text). An approval makes the grant version, makes it current and emits `grant.changed` in the same unit, through the admin module's grant operations; a failure of any step leaves nothing behind.
- A consumer sees and changes only its own requests: another consumer's id is not found (404), the same answer as an id that does not exist. A consumer that has `requests.max_open_per_consumer` open requests cannot open another (409, no error code).
- `RunSweeper` expires the open requests whose end time (`created_at` plus `requests.ttl` as it was when the request opened) has passed. Until it runs, such a request is still open and may be commented on or decided.
- Routes: the consumer's `/v1/resource-requests...` through `internal/consumerauth` (session cookie, consumer page listener) and `internal/gateway` (access key, consumer API listener), and the host's `/admin/v1/resource-requests...` through `internal/admin`. Unknown fields of a create's `ask` and of an approval's `limits` are ignored and named in the answer's `warnings` by `internal/api/ignored`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-38, D-39, D-44, D-105, D-148, D-157. The module map is in `docs/design_docs/lld.md` section 1.
