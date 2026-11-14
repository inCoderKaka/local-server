package webui

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// CSP is the Content-Security-Policy on every answer: scripts, styles, fonts and API calls from
// the page's own origin only, images also as data: URLs, no plugins, no <base>, forms only to
// the same origin and no framing. The built UI has no inline script or style, so nothing needs
// 'unsafe-inline' or a nonce.
const CSP = "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; font-src 'self'; " +
	"connect-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'"

// indexName is the page a client-side route is answered with.
const indexName = "index.html"

//go:embed stub.html
var stubHTML []byte

// Config is what Handler needs.
type Config struct {
	// Files holds the built UI, with index.html at its root. Nil, or a file system without
	// index.html, serves the stub page for every page path.
	Files fs.FS
}

// file is one answerable file, read once: the files of an embedded UI never change while the
// binary runs.
type file struct {
	body  []byte
	etag  string
	ctype string
}

type handler struct {
	files map[string]file
	index file
}

// Handler returns the handler that serves cfg.Files as a single-page app (see the package
// documentation). A file that cannot be read is left out, so it is answered 404.
func Handler(cfg Config) http.Handler {
	h := &handler{files: map[string]file{}}
	if cfg.Files != nil {
		_ = fs.WalkDir(cfg.Files, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil || !d.Type().IsRegular() || hidden(p) {
				return nil
			}
			b, err := fs.ReadFile(cfg.Files, p)
			if err != nil {
				return nil
			}
			h.files[p] = newFile(p, b)
			return nil
		})
	}
	var ok bool
	if h.index, ok = h.files[indexName]; !ok {
		h.index = newFile(indexName, stubHTML)
	}
	return h
}

func newFile(name string, b []byte) file {
	sum := sha256.Sum256(b)
	return file{body: b, etag: `"` + hex.EncodeToString(sum[:]) + `"`, ctype: contentType(name)}
}

func (h *handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	hd := w.Header()
	hd.Set("Content-Security-Policy", CSP)
	hd.Set("X-Content-Type-Options", "nosniff")
	hd.Set("Referrer-Policy", "no-referrer")
	hd.Set("Cross-Origin-Resource-Policy", "same-origin")
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		hd.Set("Allow", "GET, HEAD")
		plain(w, http.StatusMethodNotAllowed, "405 method not allowed")
		return
	}
	f, ok := h.lookup(r.URL.Path)
	if !ok {
		plain(w, http.StatusNotFound, "404 page not found")
		return
	}
	hd.Set("Content-Type", f.ctype)
	hd.Set("Cache-Control", "no-cache")
	hd.Set("ETag", f.etag)
	// ServeContent answers HEAD without a body and If-None-Match with 304. The zero time
	// leaves Last-Modified out: the ETag is the validator.
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(f.body))
}

// lookup finds the file for a decoded request path: the file itself, index.html for a page
// path, or nothing.
func (h *handler) lookup(p string) (file, bool) {
	if strings.ContainsAny(p, "\\\x00") {
		return file{}, false
	}
	for _, seg := range strings.Split(p, "/") {
		if seg == ".." {
			return file{}, false
		}
	}
	name := strings.TrimPrefix(path.Clean("/"+p), "/")
	if name == "" || name == indexName {
		return h.index, true
	}
	if hidden(name) {
		return file{}, false
	}
	if f, ok := h.files[name]; ok {
		return f, true
	}
	if strings.Contains(path.Base(name), ".") {
		// A missing asset (script, style, image) is a 404, never the page: a stale hashed
		// name must fail loudly instead of loading HTML as a script.
		return file{}, false
	}
	return h.index, true
}

// hidden reports whether a path has a segment that starts with a dot (dist/.gitkeep, editor
// files); those are never served.
func hidden(p string) bool {
	for _, seg := range strings.Split(p, "/") {
		if strings.HasPrefix(seg, ".") && seg != "." {
			return true
		}
	}
	return false
}

func plain(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(msg + "\n"))
}

// contentTypes maps the extensions a Vite build writes to their types. It is fixed here, not
// read from the OS's MIME tables, so every machine answers the same type.
var contentTypes = map[string]string{
	".html":        "text/html; charset=utf-8",
	".js":          "text/javascript; charset=utf-8",
	".mjs":         "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".json":        "application/json",
	".map":         "application/json",
	".webmanifest": "application/manifest+json",
	".svg":         "image/svg+xml",
	".png":         "image/png",
	".jpg":         "image/jpeg",
	".jpeg":        "image/jpeg",
	".gif":         "image/gif",
	".webp":        "image/webp",
	".ico":         "image/x-icon",
	".woff":        "font/woff",
	".woff2":       "font/woff2",
	".txt":         "text/plain; charset=utf-8",
	".wasm":        "application/wasm",
}

func contentType(name string) string {
	if t, ok := contentTypes[strings.ToLower(path.Ext(name))]; ok {
		return t
	}
	return "application/octet-stream"
}
