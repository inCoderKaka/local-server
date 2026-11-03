// Command local-server shares the host machine's AI inference with invited consumers.
//
// Subcommands:
//
//	local-server serve  [--data-dir DIR] [--engine ollama|fake]
//	local-server doctor [--data-dir DIR] [--json]
//	local-server recovery-code [--data-dir DIR]
//	local-server --version
//
// recovery-code shows the host's recovery code again after an admin login on a terminal; it is
// audited (req_001 D-80, D-101).
//
// The data directory is the only thing configured outside the database: --data-dir, else the
// environment variable LS_DATA_DIR, else "local-server" inside the user's configuration
// directory (os.UserConfigDir: ~/Library/Application Support on macOS, $XDG_CONFIG_HOME or
// ~/.config on Linux, %AppData% on Windows). Everything else is a host-owned setting stored in
// the database, read after it opens and applied on the next start (req_001 D-38, D-39).
//
// This package is the only place that wires adapters to ports (docs/glossary.md "Adapter"). The
// module registry is registry.go; the startup order of req_001 D-39 F9 is serve.go.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	// IANA zone data for host.quiet_hours.time_zone and policy windows on machines without a system zoneinfo database (Windows, minimal Linux).
	_ "time/tzdata"
)

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

// File names inside the data directory, and the log file name.
const (
	dbFileName        = "local-server.db"
	masterKeyFileName = "master.key"
)

// The log file lives in the OS user log folder (req_001 D-40). A host with no such folder (no
// home directory) logs to fallbackLogDir inside the data directory instead.
const (
	logFileName    = "local-server.log"
	fallbackLogDir = "logs"
)

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

// run executes one command line and returns the process exit code: 0 success, 1 the command
// failed, 2 the command line is wrong.
func run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return newApp(stdout, stderr).run(ctx, args)
}

// app holds what a command touches outside the database, so tests replace it without a real
// terminal, keychain or user directory.
type app struct {
	stdout, stderr io.Writer
	getenv         func(string) string
	// userConfigDir is os.UserConfigDir.
	userConfigDir func() (string, error)
	// userLogDir is the OS user log folder, osUserLogDir.
	userLogDir func() (string, error)
	// stderrIsTerminal says whether a person can see stderr. The recovery code is shown only
	// then (req_001 D-101).
	stderrIsTerminal func() bool
	// stdinIsTerminal says whether a person can type at stdin. recovery-code needs both
	// terminals: it reads a password (req_001 D-80, D-101).
	stdinIsTerminal func() bool
	// stdin is where recovery-code reads the login name, one line.
	stdin io.Reader
	// readPassword reads the admin password without echo; the real one reads the terminal.
	readPassword func() (string, error)
	// keychain is the OS keychain; nil means the real one.
	keychain hostkeys.Keychain
	// onListening, when set, is called by serve once its listeners are bound.
	onListening func(listening)
}

// listening are the addresses serve is bound to.
type listening struct{ Consumer, Admin string }

func newApp(stdout, stderr io.Writer) *app {
	return &app{stdout: stdout, stderr: stderr, getenv: os.Getenv,
		userConfigDir: os.UserConfigDir, userLogDir: osUserLogDir, stderrIsTerminal: stderrIsTerminal,
		stdinIsTerminal: stdinIsTerminal, stdin: os.Stdin, readPassword: readPasswordFromTerminal}
}

// osUserLogDir is the folder the OS keeps a user's application logs in: ~/Library/Logs/local-server
// on macOS, %LocalAppData%\local-server\logs on Windows, and $XDG_STATE_HOME/local-server/logs
// (else ~/.local/state/local-server/logs) elsewhere.
func osUserLogDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Logs", "local-server"), nil
	case "windows":
		base, err := os.UserCacheDir() // %LocalAppData%
		if err != nil {
			return "", err
		}
		return filepath.Join(base, "local-server", "logs"), nil
	}
	if state := os.Getenv("XDG_STATE_HOME"); state != "" {
		return filepath.Join(state, "local-server", "logs"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "local-server", "logs"), nil
}

const usageText = `usage:
  local-server serve  [--data-dir DIR] [--engine ollama|fake]
  local-server doctor [--data-dir DIR] [--json]
  local-server recovery-code [--data-dir DIR]
  local-server --version

The data directory is --data-dir, else $LS_DATA_DIR, else "local-server" in the user's
configuration directory. Every other option is a setting stored in the database.
`

func (a *app) run(ctx context.Context, args []string) int {
	if len(args) > 0 {
		switch args[0] {
		case "serve":
			return a.serve(ctx, args[1:])
		case "doctor":
			return a.doctor(ctx, args[1:])
		case "recovery-code":
			return a.recoveryCode(ctx, args[1:])
		}
	}
	fs := flag.NewFlagSet("local-server", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, usageText) }
	showVersion := fs.Bool("version", false, "print the version and exit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if *showVersion {
		fmt.Fprintln(a.stdout, "local-server", version)
		return 0
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(a.stderr, "local-server: unknown command %q\n", fs.Arg(0))
	}
	fmt.Fprint(a.stderr, usageText)
	return 2
}

// dataDir resolves the data directory: the flag, else LS_DATA_DIR, else the default.
func (a *app) dataDir(flagValue string) (string, error) {
	dir := flagValue
	if dir == "" {
		dir = a.getenv("LS_DATA_DIR")
	}
	if dir == "" {
		base, err := a.userConfigDir()
		if err != nil {
			return "", fmt.Errorf("cannot find the user configuration directory (%w); pass --data-dir or set LS_DATA_DIR", err)
		}
		dir = filepath.Join(base, "local-server")
	}
	return filepath.Abs(dir)
}

// stderrIsTerminal reports whether stderr is an interactive terminal: a character device that
// is not the null device.
func stderrIsTerminal() bool { return isTerminal(os.Stderr) }

// stdinIsTerminal reports whether stdin is an interactive terminal.
func stdinIsTerminal() bool { return isTerminal(os.Stdin) }

func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil || fi.Mode()&os.ModeCharDevice == 0 {
		return false
	}
	null, err := os.Stat(os.DevNull)
	return err != nil || !os.SameFile(fi, null)
}
