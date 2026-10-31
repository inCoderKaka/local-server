package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRun(t *testing.T) {
	cases := []struct {
		name      string
		args      []string
		want      int
		stdoutPre string
		stderrHas string
	}{
		{"version", []string{"--version"}, 0, "local-server ", ""},
		{"help", []string{"--help"}, 0, "", ""},
		{"short help", []string{"-h"}, 0, "", ""},
		{"unknown flag", []string{"--nope"}, 2, "", ""},
		{"no args", nil, 2, "", "nothing to run yet"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if got := run(tc.args, &stdout, &stderr); got != tc.want {
				t.Fatalf("run(%v) = %d, want %d", tc.args, got, tc.want)
			}
			if !strings.HasPrefix(stdout.String(), tc.stdoutPre) {
				t.Errorf("stdout = %q, want prefix %q", stdout.String(), tc.stdoutPre)
			}
			if !strings.Contains(stderr.String(), tc.stderrHas) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.stderrHas)
			}
		})
	}
}
