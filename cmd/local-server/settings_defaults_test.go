package main

import (
	"testing"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/consumerauth"
	"github.com/inCoderKaka/local-server/internal/settings"
)

// consumer.session_ttl is its own setting with the same default as admin.session_ttl (req_001
// D-157). The two live in modules that do not import each other, so this package checks it.
func TestConsumerSessionTTLDefaultMatchesAdmin(t *testing.T) {
	def := func(defs []settings.Def, key string) (string, bool) {
		for _, d := range defs {
			if d.Key == key {
				return d.Default, true
			}
		}
		return "", false
	}
	adminTTL, ok := def(admin.Defs(), admin.KeySessionTTL)
	if !ok {
		t.Fatalf("%s is not in admin.Defs()", admin.KeySessionTTL)
	}
	consumerTTL, ok := def(consumerauth.Defs(), consumerauth.KeySessionTTL)
	if !ok {
		t.Fatalf("%s is not in consumerauth.Defs()", consumerauth.KeySessionTTL)
	}
	if adminTTL != consumerTTL {
		t.Errorf("%s default = %q, want %q (the default of %s)",
			consumerauth.KeySessionTTL, consumerTTL, adminTTL, admin.KeySessionTTL)
	}
}
