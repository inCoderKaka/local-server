# transport module

## Purpose

How consumers reach the host: system Tailscale, Funnel, embedded tsnet.

## Owns

- Packages: `internal/transport/` (the port: `Transport`, `Status`, `Node`, and the listener layout `Plan` and `Bind`; PLT-F-038, built) and `internal/transport/tailscale/` (the adapter for the host's own Tailscale app, through the official `tailscale.com/client/local` package; PLT-F-038, built). Later adapters: `tsnet` (PLT-F-040), `funnel` (PLT-F-039), designed and not built.
- Tables: none.
- Port: `transport` (`internal/transport.Transport`): `Status` (detection: installed and running, logged in, tailnet address and `*.ts.net` name, HTTPS certificates), `WhoIs` (the node behind a connection: stable node ID and machine name), `Resolve` (a node by tailnet address, node ID or name) and `TLSConfig` (the `*.ts.net` certificate).

## Listener layout (req_001 D-148)

Three separate HTTPS listeners, each bound only to the host's tailnet address (never a wildcard, never loopback) and using the `*.ts.net` certificate: the consumer API (default port 8484, access keys only, never a cookie, no CORS), the consumer web page (8485, same-origin `/v1` account routes; the seam is `consumerPageHandler` in `cmd/local-server`, filled by PLT-F-027 and PLT-F-036) and the optional remote admin (8486, only while `serve.tailnet_admin_enabled` is true, behind the admin login, its `ls_admin_session` cookie `Secure`, req_001 D-86). The ports and the flag are host-owned settings (`serve.tailnet_*`). The admin listener on localhost stays the default. When Tailscale is missing, stopped, logged out or has no HTTPS certificates, `serve` still starts, serves on localhost only, and warns in the log, in `doctor` and in `GET /admin/v1/host` (`tailnet`), naming the fix. The state is read once at start: a Tailscale that comes up later needs a restart of local-server.

A tailnet request is logged with the calling node (ID and machine name, from WhoIs), never a body. An access key bound to a node (`idn_access_key.bound_node` is the stable node ID, `bound_node_name` the machine name; req_001 D-154, D-159) works only from that node: `auth.Service` asks the `auth.WhoIs` port, which `cmd/local-server` implements over this port.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-12, D-23, D-24, D-86, D-147, D-148, D-154, D-159. The module map is in `docs/design_docs/lld.md` section 1.
