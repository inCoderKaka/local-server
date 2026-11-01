package gateway

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/scheduler"
)

func TestErrorHandlerSeparatesWriteFailuresFromDecodeRefusals(t *testing.T) {
	g := &Gateway{log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	for _, tc := range []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{"a response ogen could not write", errors.New("write: broken pipe"), http.StatusInternalServerError, ""},
		{"a request decoding error", &ogenerrors.DecodeRequestError{Err: errors.New("bad body")}, http.StatusBadRequest, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			g.errorHandler(context.Background(), rec, httptest.NewRequest(http.MethodPost, "/v1/x", nil), tc.err)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d: %s", rec.Code, tc.status, rec.Body)
			}
			if tc.status == http.StatusInternalServerError && !strings.Contains(rec.Body.String(), `"server_error"`) {
				t.Fatalf("body = %s, want a server_error", rec.Body)
			}
		})
	}
}

func TestClassifyFlagsUnknownSchedulerAndEngineCodesAsInternal(t *testing.T) {
	if _, internal := classify(&scheduler.Error{Code: "other"}, time.Time{}); !internal {
		t.Error("an unknown scheduler code is not flagged internal")
	}
	if _, internal := classify(engine.Errorf("other", nil, "x"), time.Time{}); !internal {
		t.Error("an unknown engine code is not flagged internal")
	}
	if _, internal := classify(&scheduler.Error{Code: scheduler.CodeQueueFull}, time.Time{}); internal {
		t.Error("queue_full is flagged internal")
	}
}
