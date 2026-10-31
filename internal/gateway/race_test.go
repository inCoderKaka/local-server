//go:build race

package gateway_test

// The race detector slows the code several times over; the SLO checks scale their targets by it.
const (
	raceEnabled  = true
	raceSlowdown = 20
)
