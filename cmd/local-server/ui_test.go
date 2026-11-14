package main

import (
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"

	"github.com/inCoderKaka/local-server/internal/webui"
	adminui "github.com/inCoderKaka/local-server/web/admin"
)

// The admin listener serves the admin UI embedded in the binary: the built UI after `make build`,
// else the stub page, with the CSP; unknown API paths keep the error envelope (PLT-F-032,
// req_001 D-10, D-152).
func TestAdminListenerServesTheUI(t *testing.T) {
	ta := newTestApp(t, true)
	seed(t, ta.dir, loopbackAny)
	r := startServe(t, ta, "--engine", "fake")
	defer r.stop()
	base := "http://" + r.addrs.Admin

	resp, err := testClient.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Security-Policy") != webui.CSP {
		t.Fatalf("GET / = %d, CSP %q", resp.StatusCode, resp.Header.Get("Content-Security-Policy"))
	}
	index, err := fs.ReadFile(adminui.Files(), "index.html")
	switch {
	case err == nil:
		if string(body) != string(index) {
			t.Fatalf("GET / is not the built index.html: %s", body)
		}
		// Every script and stylesheet the page names is served with its type.
		refs := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(string(index), -1)
		if len(refs) == 0 {
			t.Fatal("the built index.html names no asset")
		}
		for _, m := range refs {
			a, err := testClient.Get(base + m[1])
			if err != nil {
				t.Fatal(err)
			}
			a.Body.Close()
			want := "text/javascript; charset=utf-8"
			if strings.HasSuffix(m[1], ".css") {
				want = "text/css; charset=utf-8"
			}
			if a.StatusCode != http.StatusOK || a.Header.Get("Content-Type") != want || a.Header.Get("Content-Security-Policy") != webui.CSP {
				t.Errorf("GET %s = %d %q", m[1], a.StatusCode, a.Header.Get("Content-Type"))
			}
		}
	default:
		if !strings.Contains(string(body), "make web-admin-build") {
			t.Fatalf("GET / without a built UI is not the stub: %s", body)
		}
	}

	resp, err = testClient.Get(base + "/admin/v1/nope")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var env struct {
		Error map[string]any `json:"error"`
	}
	if resp.StatusCode != http.StatusNotFound || json.NewDecoder(resp.Body).Decode(&env) != nil || env.Error["message"] == nil {
		t.Fatalf("GET /admin/v1/nope = %d, not the envelope", resp.StatusCode)
	}
}
