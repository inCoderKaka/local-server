package credential

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Reason says why Guard refused a request. It is a fixed code: the Host or Origin value that
// caused it is never part of it, so a caller can put it in a log line or an answer.
type Reason string

// The refusal reasons.
const (
	// ReasonForeignHost: the Host header is not a loopback name or address or a configured name
	// (DNS rebinding), or is missing or malformed.
	ReasonForeignHost Reason = "foreign_host"
	// ReasonHostUnavailable: the configured names could not be read, so the request is refused.
	ReasonHostUnavailable Reason = "host_check_unavailable"
	// ReasonForeignOrigin: the Origin header is present and is not the request's own origin.
	ReasonForeignOrigin Reason = "foreign_origin"
	// ReasonOriginRequired: a state-changing request carries a session cookie and no Origin.
	ReasonOriginRequired Reason = "origin_required"
)

// GuardConfig configures Guard.
type GuardConfig struct {
	// Names returns the Host names accepted besides localhost and loopback addresses, for
	// example the configured tailnet name. It is called for each request, so a changed setting
	// applies at once. If it returns an error the request is refused (fail closed). Nil means
	// no extra names.
	Names func(ctx context.Context) ([]string, error)
	// SessionCookies are the names of the session cookies this surface uses. A state-changing
	// request that carries one needs an Origin header.
	SessionCookies []string
	// Reject writes the refusal. It must not echo the Host or Origin header.
	Reject func(w http.ResponseWriter, r *http.Request, why Reason)
}

// Guard returns the middleware that protects a listener against DNS rebinding and cross-site
// request forgery (req_001 D-10):
//
//   - Host: the Host header, with its port, brackets, case and a trailing dot removed, must be
//     "localhost", a loopback IP address (127.0.0.0/8, ::1) or one of cfg.Names. A web page on
//     another site can rebind its own name to 127.0.0.1 and reach a loopback listener from the
//     user's browser, but its requests still carry its own name in Host. A missing Host is
//     refused.
//   - Origin: on a method other than GET, HEAD and OPTIONS, a request that carries one of the
//     session cookies must carry an Origin header, and any Origin that is present must be the
//     request's own origin: the same scheme (http, or https when the connection is TLS), host and
//     port as the Host header. "null" and every other origin are refused.
func Guard(cfg GuardConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host, port, ok := splitHost(r.Host)
			if !ok {
				cfg.Reject(w, r, ReasonForeignHost)
				return
			}
			if !isLoopbackName(host) {
				var extra []string
				if cfg.Names != nil {
					var err error
					if extra, err = cfg.Names(r.Context()); err != nil {
						cfg.Reject(w, r, ReasonHostUnavailable)
						return
					}
				}
				if !allowedName(host, extra) {
					cfg.Reject(w, r, ReasonForeignHost)
					return
				}
			}
			if stateChanging(r.Method) {
				if why, ok := checkOrigin(r, host, port, cfg.SessionCookies); !ok {
					cfg.Reject(w, r, why)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

func stateChanging(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	return true
}

// checkOrigin applies the Origin rules to a state-changing request whose Host is host:port.
func checkOrigin(r *http.Request, host, port string, cookies []string) (Reason, bool) {
	vals := r.Header.Values("Origin")
	if len(vals) == 0 {
		for _, name := range cookies {
			if _, err := r.Cookie(name); err == nil {
				return ReasonOriginRequired, false
			}
		}
		return "", true
	}
	if len(vals) != 1 {
		return ReasonForeignOrigin, false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if !sameOrigin(vals[0], scheme, host, port) {
		return ReasonForeignOrigin, false
	}
	return "", true
}

// sameOrigin reports whether origin (an Origin header value) is exactly scheme://host[:port] for
// the request's own host and port. A default port is the same as no port.
func sameOrigin(origin, scheme, host, port string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Opaque != "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.ForceQuery ||
		(u.Path != "" && u.Path != "/") || u.Host == "" || u.Scheme != scheme {
		return false
	}
	oh, op, ok := splitHost(u.Host)
	if !ok {
		return false
	}
	return oh == host && portOrDefault(op, scheme) == portOrDefault(port, scheme)
}

func portOrDefault(port, scheme string) string {
	if port != "" {
		return port
	}
	if scheme == "https" {
		return "443"
	}
	return "80"
}

// splitHost splits a Host header value into a lower-cased host, without brackets or a trailing
// dot, and its port ("" when there is none). ok is false for an empty host or a port that is not
// a number.
func splitHost(hostport string) (host, port string, ok bool) {
	if hostport == "" {
		return "", "", false
	}
	host = hostport
	if h, p, err := net.SplitHostPort(hostport); err == nil {
		host, port = h, p
		if port != "" {
			n, err := strconv.Atoi(port)
			if err != nil || n < 0 || n > 65535 || strings.TrimLeft(port, "0123456789") != "" {
				return "", "", false
			}
		}
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "" || strings.ContainsAny(host, " \t/\\@?#") {
		return "", "", false
	}
	return host, port, true
}

func isLoopbackName(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// IsLoopbackHost reports whether a Host header value, with or without a port, names localhost or
// a loopback IP address.
func IsLoopbackHost(hostport string) bool {
	host, _, ok := splitHost(hostport)
	return ok && isLoopbackName(host)
}

func allowedName(host string, extra []string) bool {
	for _, n := range extra {
		if h, _, ok := splitHost(n); ok && h == host {
			return true
		}
	}
	return false
}
