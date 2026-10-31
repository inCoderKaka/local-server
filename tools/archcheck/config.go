package main

// Module is one internal part of local-server (docs/glossary.md "Module").
// It owns its packages, its tables (by prefix) and its port, if it has one.
type Module struct {
	Name string
	// Packages are the package directories the module owns, relative to the
	// repository root. A sub-directory belongs to its nearest listed ancestor,
	// unless a longer path is listed for another module.
	Packages []string
	// Tables are the D-44 table prefixes (with the trailing underscore) the
	// module owns. A module that owns tables must have a migrations folder
	// next to its first package.
	Tables []string
	// Adapters are package directories (they need not exist yet) that hold
	// adapters of the module's port. Only cmd/ may import an adapter, plus packages inside that adapter's own directory; the owning module's core packages may not (D-21: core modules import ports, never adapters).
	Adapters []string
}

// Config is the whole module map. It mirrors docs/design_docs/lld.md section 1.
type Config struct {
	// ModulePath is read from go.mod at run time, not configured here.
	Modules []Module
	// TablePrefixes lists the twelve D-44 prefixes; each must be owned by at
	// least one module.
	TablePrefixes []string
	// Restricted maps an import-path prefix to the modules, or package
	// directories (an entry containing "/", which also covers its
	// sub-directories), allowed to import it. Domain code never imports
	// drivers directly (D-09, D-21).
	Restricted map[string][]string
	// NoteDir holds one <module>.md architecture note per module (D-31).
	NoteDir string
	// NoteHeadings are the headings every note must contain, taken from the
	// template.
	NoteHeadings []string
}

// DefaultConfig reconciles D-21 (six top-level modules) with D-44 (twelve
// table prefixes) and the packages later tickets add (LLD section 1).
func DefaultConfig() Config {
	return Config{
		Modules: []Module{
			{Name: "storage", Packages: []string{"internal/storage"}, Tables: []string{"ops_"}},
			{Name: "settings", Packages: []string{"internal/settings"}, Tables: []string{"hst_"}},
			{Name: "telemetry", Packages: []string{"internal/telemetry"}, Adapters: []string{"internal/telemetry/slogprom"}},
			{Name: "host", Packages: []string{"internal/host", "internal/hostkeys", "internal/hostmetrics"}, Tables: []string{"hst_"}},
			{Name: "engine", Packages: []string{"internal/engine"}, Adapters: []string{"internal/engine/ollama"}},
			{Name: "identity", Packages: []string{"internal/identity"}, Tables: []string{"idn_"}},
			{Name: "grants", Packages: []string{"internal/grants"}, Tables: []string{"grt_"}},
			{Name: "audit", Packages: []string{"internal/audit"}, Tables: []string{"aud_"}},
			{Name: "events", Packages: []string{"internal/notifier/events"}, Tables: []string{"ntf_"}},
			{Name: "auth", Packages: []string{"internal/auth"}},
			{Name: "policy", Packages: []string{"internal/policy"}},
			{Name: "scheduler", Packages: []string{"internal/scheduler", "internal/jobs"}, Tables: []string{"sch_"}},
			{Name: "usage", Packages: []string{"internal/usage"}, Tables: []string{"use_"}},
			{Name: "ledger", Packages: []string{"internal/ledger"}, Tables: []string{"ldg_"}},
			{Name: "limits", Packages: []string{"internal/limits"}},
			{Name: "gateway", Packages: []string{"internal/gateway"}},
			{Name: "requests", Packages: []string{"internal/requests"}, Tables: []string{"req_"}},
			{Name: "notifier", Packages: []string{"internal/notifier"}, Tables: []string{"ntf_"},
				Adapters: []string{"internal/notifier/desktop", "internal/notifier/webhook"}},
			{Name: "transport", Packages: []string{"internal/transport"},
				Adapters: []string{"internal/transport/tailscale", "internal/transport/tsnet", "internal/transport/funnel"}},
			{Name: "offhost", Packages: []string{"internal/offhost"}, Tables: []string{"syn_"},
				Adapters: []string{"internal/offhost/platform", "internal/offhost/snapshot"}},
			{Name: "admin", Packages: []string{"internal/admin"}},
			{Name: "consumerauth", Packages: []string{"internal/consumerauth"}},
			{Name: "bundle", Packages: []string{"internal/bundle"}},
			{Name: "sharing", Packages: []string{"internal/sharing"}, Tables: []string{"plt_"}},
			{Name: "power", Packages: []string{"internal/power"}},
			{Name: "service", Packages: []string{"internal/service"}},
			{Name: "directory", Packages: []string{"internal/directory"}},
			{Name: "api", Packages: []string{"internal/api"}},
		},
		TablePrefixes: []string{
			"hst_", "idn_", "grt_", "req_", "sch_", "use_",
			"ldg_", "ntf_", "aud_", "syn_", "plt_", "ops_",
		},
		Restricted: map[string][]string{
			"database/sql":                        {"storage"},
			"modernc.org/sqlite":                  {"storage"},
			"github.com/shirou/gopsutil":          {"host"},
			"tailscale.com":                       {"transport"},
			"gopkg.in/natefinch/lumberjack.v2":    {"internal/telemetry/slogprom"},
			"github.com/prometheus/client_golang": {"internal/telemetry/slogprom"},
			"github.com/zalando/go-keyring":       {"internal/hostkeys"},
		},
		NoteDir:      "docs/modules",
		NoteHeadings: []string{"## Purpose", "## Owns", "## Imports", "## Decisions"},
	}
}
