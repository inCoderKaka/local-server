//go:build !windows

package gateway_test

// No scaling off Windows; see windows_test.go.
const (
	osSlowdownNum = 1
	osSlowdownDen = 1
)
