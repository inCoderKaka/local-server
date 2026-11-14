// Package tailscale is the transport adapter for the host's own Tailscale app (req_001 D-23,
// D-147): it asks Tailscale's local API, through the official tailscale.com/client/local
// package, for the host's tailnet state, for the node behind a connection (WhoIs) and for the
// *.ts.net certificate. It talks to a Tailscale that is already running; it embeds none (tsnet
// is a later adapter). Only cmd/ imports it.
package tailscale
