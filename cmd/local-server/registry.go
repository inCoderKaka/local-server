package main

import (
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/grants"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/usage"
)

// registry is the module registry: one line per module that owns tables, in migration order
// (req_001 D-39 F9 step 2). storage.Open applies the storage module's own migrations first and
// then these, in this order, and refuses a table that references a table of a module listed
// after it (storage.checkForeignKeys). So a module goes after every module whose tables its
// migrations reference.
//
// To add a module (a later ticket's whole job here): add one line, after the modules it
// references. The registry test then opens every registered module on a fresh database.
var registry = []func() storage.Module{
	settings.Module, // hst_setting: first, every later module's settings live here
	host.Module,     // hst_host, hst_key, hst_metric_minute
	identity.Module, // idn_*
	grants.Module,   // grt_*: references idn_*
	audit.Module,    // aud_*: references idn_principal
	events.Module,   // ntf_*
	usage.Module,    // use_*
	ledger.Module,   // ldg_*
}

// registryModules returns the storage modules of the registry, in order.
func registryModules() []storage.Module {
	mods := make([]storage.Module, len(registry))
	for i, m := range registry {
		mods[i] = m()
	}
	return mods
}
