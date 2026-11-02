package main

import (
	"bytes"
	"errors"
	"path/filepath"
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
		{"no args", nil, 2, "", "usage:"},
		{"unknown command", []string{"frobnicate"}, 2, "", `unknown command "frobnicate"`},
		{"serve unknown flag", []string{"serve", "--nope"}, 2, "", ""},
		{"serve bad engine", []string{"serve", "--engine", "gpt"}, 2, "", "--engine must be"},
		{"serve extra argument", []string{"serve", "now"}, 2, "", "no arguments"},
		{"doctor extra argument", []string{"doctor", "now"}, 2, "", "no arguments"},
		{"serve help", []string{"serve", "-h"}, 0, "", ""},
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

func TestDataDirResolution(t *testing.T) {
	cfgDir := t.TempDir()
	cases := []struct {
		name, flag, env, want string
	}{
		{"flag wins", "/flag", "/env", "/flag"},
		{"env when no flag", "", "/env", "/env"},
		{"default in the user configuration directory", "", "", filepath.Join(cfgDir, "local-server")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a := &app{
				getenv:        func(k string) string { return map[string]string{"LS_DATA_DIR": tc.env}[k] },
				userConfigDir: func() (string, error) { return cfgDir, nil },
			}
			got, err := a.dataDir(tc.flag)
			if err != nil {
				t.Fatal(err)
			}
			if want, _ := filepath.Abs(tc.want); got != want {
				t.Errorf("dataDir = %q, want %q", got, want)
			}
		})
	}
	a := &app{getenv: func(string) string { return "" }, userConfigDir: func() (string, error) { return "", errors.New("no home") }}
	if _, err := a.dataDir(""); err == nil || !strings.Contains(err.Error(), "--data-dir") {
		t.Errorf("dataDir with no home = %v; want an error naming --data-dir", err)
	}
}
