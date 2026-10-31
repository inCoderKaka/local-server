package directory

import "context"

// Entry is one host a directory knows about. Phase 1 has no directory, so the
// shape stays minimal until a central server or marketplace exists (D-02).
type Entry struct {
	HostID string
	Addr   string
}

// Directory is the seam for a future central server or marketplace (D-21).
type Directory interface {
	// Discover returns the hosts the directory knows about.
	Discover(ctx context.Context) ([]Entry, error)
}

// Noop is the Phase 1 directory: it knows no hosts and contacts nobody.
type Noop struct{}

// Discover returns no entries.
func (Noop) Discover(context.Context) ([]Entry, error) { return nil, nil }

var _ Directory = Noop{}
