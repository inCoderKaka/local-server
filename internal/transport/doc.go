// Package transport is part of the transport module; see docs/modules/transport.md. It is the
// port to the encrypted channel consumers reach the host over (req_001 D-12, D-23): what the
// host's tailnet looks like (Status), who a peer is (WhoIs, Resolve), the certificate the
// listeners present (TLSConfig), and the layout of the listeners that bind only to the tailnet
// address (Plan, Bind; req_001 D-148). Its adapters live in subpackages (tailscale: PLT-F-038;
// tsnet and funnel are later) and only cmd/ imports them.
package transport
