package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/inCoderKaka/local-server/internal/transport"
)

// tailnetWiring is what wire needs from the tailnet: the transport (nil when this build has none),
// how long to wait for a node lookup, how long to wait for the host's node lookup when it binds a
// key, the host's own *.ts.net names for the admin guard, and the report the admin API shows.
type tailnetWiring struct {
	Transport     transport.Transport
	WhoIsTimeout  time.Duration
	StatusTimeout time.Duration
	Hosts         []string
	Report        transport.Report
}

// detectTailnet asks the transport for its state and plans the tailnet listeners. A host whose
// Tailscale is missing, logged out or without HTTPS certificates is not an error: serve runs on
// localhost only, and this says so on stderr and in the log, naming the fix (req_001 D-148).
func (a *app) detectTailnet(ctx context.Context, tr transport.Transport, cfg startupConfig, log *slog.Logger) transport.Report {
	sctx, cancel := context.WithTimeout(ctx, cfg.TailnetStatusTimeout)
	defer cancel()
	st := tr.Status(sctx)
	rep := transport.Report{Status: st, Listeners: transport.Plan(st, cfg.Tailnet)}
	if !st.Ready() {
		fmt.Fprintf(a.stderr, "local-server: warning: %s, so it serves on this machine only (no tailnet listeners); to fix: %s\n", st.Message(), st.Fix())
		log.Warn("tailnet listeners are off; serving on localhost only", "state", string(st.State), "reason", st.Message(), "detail", st.Detail, "fix", st.Fix())
	}
	return rep
}

// warmCertificate asks Tailscale for the host's certificate once the listeners are up, so the
// first consumer does not wait for it to be issued. A failure is logged with the fix; the
// listeners stay up, and each handshake asks again.
func warmCertificate(ctx context.Context, tr transport.Transport, name string, log *slog.Logger) {
	if _, err := tr.TLSConfig().GetCertificate(&tls.ClientHelloInfo{ServerName: name}); err != nil {
		if ctx.Err() == nil {
			log.Warn("Tailscale did not issue the host's certificate; consumers cannot connect over HTTPS until it does",
				"name", name, "error", err.Error(),
				"fix", "in the tailnet's admin console turn on MagicDNS and HTTPS Certificates (DNS page), and check that this machine's Tailscale is logged in")
		}
		return
	}
	log.Info("the host's tailnet certificate is ready", "name", name)
}

// accessKeysOnly is the consumer API's front door: it takes no cookie. Only an access key
// authenticates a call to /v1 there, never a browser session, so a page on another site cannot
// ride a login. There is no CORS either: a preflight is refused, and no response names an
// allowed origin, so a browser keeps a page on another site from reading it (req_001 D-148).
func accessKeysOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
			w.Header().Set("Allow", "GET, HEAD, POST, DELETE")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		r.Header.Del("Cookie")
		next.ServeHTTP(w, r)
	})
}

// consumerPageHandler is the consumer page listener's handler. The page and the /v1 account routes
// it uses (sign-up, login, me, limits, usage, access keys, resource requests: PLT-F-027, F-036)
// are mounted on this mux when they are built; until then it answers 404 and nothing else
// (req_001 D-148).
func consumerPageHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", http.NotFound)
	return mux
}

// peerLog names the tailnet node behind each request in the log, from Tailscale's WhoIs: the node
// id and machine name, the method and the path, never a body or a header. It is the host-side
// record of who called (req_001 D-146). A node that cannot be named is logged as unknown; the
// request goes on, and a key bound to a node is checked separately.
func peerLog(tr transport.Transport, timeout time.Duration, role transport.Role, log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			n, err := tr.WhoIs(ctx, r.RemoteAddr)
			cancel()
			if err != nil {
				log.WarnContext(r.Context(), "tailnet request from a node Tailscale could not name", "role", string(role), "method", r.Method, "path", r.URL.Path, "reason", reasonOf(err))
			} else {
				log.InfoContext(r.Context(), "tailnet request", "role", string(role), "node_id", n.ID, "node_name", n.Name, "method", r.Method, "path", r.URL.Path)
			}
			next.ServeHTTP(w, r)
		})
	}
}

func reasonOf(err error) string {
	switch {
	case errors.Is(err, transport.ErrNodeNotFound):
		return "not a tailnet peer"
	case errors.Is(err, context.DeadlineExceeded):
		return "timed out"
	}
	return "Tailscale did not answer"
}

// nodeOf adapts the transport to auth.WhoIs: the stable node id behind a connection.
type nodeOf struct {
	tr      transport.Transport
	timeout time.Duration
}

func (n nodeOf) NodeOf(ctx context.Context, remoteAddr string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, n.timeout)
	defer cancel()
	node, err := n.tr.WhoIs(ctx, remoteAddr)
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// boundedResolver limits the host's node lookup when it binds a key, so a Tailscale that does not
// answer cannot hold the admin request open. A deadline reaches the adapter as ErrUnavailable. A
// zero timeout leaves the lookup unlimited.
type boundedResolver struct {
	tr      transport.Transport
	timeout time.Duration
}

func (b boundedResolver) Resolve(ctx context.Context, ref string) (transport.Node, error) {
	if b.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, b.timeout)
		defer cancel()
	}
	return b.tr.Resolve(ctx, ref)
}

// tailnetURL is the address a consumer types for a tailnet listener: https, the host's *.ts.net
// name and the listener's port.
func tailnetURL(dns, addr string) string {
	i := strings.LastIndex(addr, ":")
	return "https://" + dns + addr[i:]
}
