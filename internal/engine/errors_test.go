package engine_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/inCoderKaka/local-server/internal/engine"
)

func TestErrorMatchesByCode(t *testing.T) {
	err := fmt.Errorf("wrapped: %w", engine.Errorf(engine.CodeContextTooLong, nil, "too long"))
	if !errors.Is(err, engine.ErrContextTooLong) || errors.Is(err, engine.ErrModelNotFound) {
		t.Fatalf("errors.Is mismatch for %v", err)
	}
	if engine.CodeOf(err) != engine.CodeContextTooLong || engine.CodeOf(errors.New("x")) != "" {
		t.Fatal("CodeOf mismatch")
	}
}
