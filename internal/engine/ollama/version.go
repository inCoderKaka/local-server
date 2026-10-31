package ollama

import (
	"context"
	"strconv"
	"strings"

	"github.com/inCoderKaka/local-server/internal/engine"
)

// Version reads /api/version.
func (a *Adapter) Version(ctx context.Context) (string, error) {
	var out struct {
		Version string `json:"version"`
	}
	if err := a.do(ctx, a.cfg.RequestTimeout, "GET", "/api/version", nil, &out); err != nil {
		return "", err
	}
	return out.Version, nil
}

// CheckVersion fails with engine.ErrUnsupportedVersion when the server is
// older than Config.MinVersion (req_001 D-48). cmd/ calls it at startup and
// after the host changes the Ollama address.
func (a *Adapter) CheckVersion(ctx context.Context) error {
	got, err := a.Version(ctx)
	if err != nil {
		return err
	}
	have, ok1 := parseVersion(got)
	need, ok2 := parseVersion(a.cfg.MinVersion)
	if !ok2 {
		return engine.Errorf(engine.CodeInvalidRequest, nil, "minimum version %q is not a version number", a.cfg.MinVersion)
	}
	if !ok1 || compareVersion(have, need) < 0 {
		return engine.Errorf(engine.CodeUnsupportedVersion, nil, "ollama %q is older than the minimum %s", got, a.cfg.MinVersion)
	}
	return nil
}

// parseVersion reads "0.12.6", "v0.12.6" or "0.12.6-rc1" into three numbers.
func parseVersion(s string) ([3]int, bool) {
	var v [3]int
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if s == "" || len(parts) > 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func compareVersion(a, b [3]int) int {
	for i := range a {
		if a[i] != b[i] {
			if a[i] < b[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}
