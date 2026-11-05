package admin

import (
	"context"
	"net/http"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
)

// ReferenceHash exposes referenceHash to the external tests.
func (a *Authenticator) ReferenceHash(ctx context.Context) (string, error) {
	return a.referenceHash(ctx, a.cfg.DB.Reader())
}

// Reject exposes reject to the external tests.
func (a *Admin) Reject(w http.ResponseWriter, r *http.Request, why credential.Reason) {
	a.reject(w, r, why)
}

// DecodeError exposes decodeError to the external tests: the status, param and message it answers.
func DecodeError(err error) (status int, param, message string) {
	e := decodeError(err)
	return e.status, e.param, e.message
}
