package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"path/filepath"
	"runtime"
	"time"

	"github.com/inCoderKaka/local-server/internal/service"
	"github.com/inCoderKaka/local-server/internal/service/osadapter"
)

// serviceHooks holds what `local-server service` touches outside the process, so tests replace
// it without a real service manager. A nil field means the real one, which lives in
// internal/service/osadapter: this file only parses flags, builds the Spec, calls the adapter and
// prints Outcomes (req_001 D-137, D-30).
type serviceHooks struct {
	// executable is the absolute path of this binary, which the service definition runs.
	executable func() (string, error)
	// target is the OS family; empty means runtime.GOOS.
	target service.Target
	// userName is the current OS user (DOMAIN\user on Windows).
	userName func() (string, error)
	// newManager returns the Manager for a Spec; nil means the real adapter for this OS.
	newManager func(service.Spec) (service.Manager, error)
}

const serviceUsageText = `usage:
  local-server service install   [--mode user|system] [--name NAME] [--data-dir DIR] [--run-as ACCOUNT]
                                 [--restart-delay D] [--restart-count N] [--restart-reset-period D] [--linger=false]
  local-server service uninstall [--mode user|system] [--name NAME]
  local-server service start     [--mode user|system] [--name NAME]
  local-server service stop      [--mode user|system] [--name NAME]
  local-server service status    [--mode user|system] [--name NAME]

The service runs "local-server serve" in the background and restarts it after a crash. User mode
(the default) starts at login and needs no administrator rights. System mode starts at boot and
needs sudo (macOS, Linux) or an elevated terminal (Windows), and --data-dir.
`

func (a *app) hookExecutable() (string, error) {
	if a.svc.executable != nil {
		return a.svc.executable()
	}
	return osadapter.Executable()
}

func (a *app) hookTarget() service.Target {
	if a.svc.target != "" {
		return a.svc.target
	}
	return service.Target(runtime.GOOS)
}

func (a *app) hookUserName() (string, error) {
	if a.svc.userName != nil {
		return a.svc.userName()
	}
	return osadapter.CurrentUserName()
}

func (a *app) hookManager(s service.Spec) (service.Manager, error) {
	if a.svc.newManager != nil {
		return a.svc.newManager(s)
	}
	return osadapter.NewManager(s)
}

// serviceCmd runs `local-server service <action>`: exit 0 done, 1 failed, 2 wrong command line.
func (a *app) serviceCmd(ctx context.Context, args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, serviceUsageText)
		return 2
	}
	action, args := args[0], args[1:]
	switch action {
	case "install", "uninstall", "start", "stop", "status":
	default:
		fmt.Fprintf(a.stderr, "local-server service: unknown action %q\n", action)
		fmt.Fprint(a.stderr, serviceUsageText)
		return 2
	}
	fs := flag.NewFlagSet("service "+action, flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	fs.Usage = func() { fmt.Fprint(a.stderr, serviceUsageText) }
	modeFlag := fs.String("mode", "", "service mode: user (starts at login, the default) or system (starts at boot)")
	nameFlag := fs.String("name", "", "service name (default: "+service.DefaultName(service.TargetLinux)+", on macOS "+service.DefaultName(service.TargetDarwin)+")")
	var (
		dataDirFlag, runAs *string
		restartDelay       *time.Duration
		restartCount       *int
		resetPeriod        *time.Duration
		linger             *bool
	)
	if action == "install" {
		dataDirFlag = fs.String("data-dir", "", "data directory the service uses (default: as for serve; required in system mode)")
		runAs = fs.String("run-as", "", "system mode: the account the service runs as (default: root, LocalSystem)")
		restartDelay = fs.Duration("restart-delay", service.DefaultRestartDelay, "wait before restarting after an unexpected exit")
		restartCount = fs.Int("restart-count", service.DefaultRestartCount, "Windows logon task: how many times to restart after a failed run")
		resetPeriod = fs.Duration("restart-reset-period", service.DefaultRestartResetPeriod, "Windows service: run this long without failing to reset the failure count")
		linger = fs.Bool("linger", true, "Linux user mode: keep the service running after logout and start it at boot (loginctl enable-linger)")
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(a.stderr, "local-server service %s: no arguments are accepted\n", action)
		return 2
	}
	target := a.hookTarget()
	if target != service.TargetDarwin && target != service.TargetLinux && target != service.TargetWindows {
		fmt.Fprintf(a.stderr, "local-server: a background service is not supported on %s\n", target)
		return 1
	}
	name := *nameFlag
	if name == "" {
		name = service.DefaultName(target)
	}
	base := service.Spec{Target: target, Name: name}

	// status without --mode asks about both modes.
	modes := []service.Mode{service.Mode(*modeFlag)}
	switch *modeFlag {
	case "":
		modes = []service.Mode{service.ModeUser}
		if action == "status" {
			modes = []service.Mode{service.ModeUser, service.ModeSystem}
		}
	case string(service.ModeUser), string(service.ModeSystem):
	default:
		fmt.Fprintf(a.stderr, "local-server service %s: --mode must be user or system\n", action)
		return 2
	}

	if action == "install" {
		return a.serviceInstall(ctx, base, modes[0], *dataDirFlag, *runAs, *restartDelay, *restartCount, *resetPeriod, *linger)
	}
	code := 0
	var shown int
	for _, m := range modes {
		s := base
		s.Mode = m
		if err := s.ValidateIdentity(); err != nil {
			fmt.Fprintf(a.stderr, "local-server service %s: %v\n", action, err)
			return 2
		}
		mgr, err := a.hookManager(s)
		if err != nil {
			fmt.Fprintf(a.stderr, "local-server service %s: %v\n", action, err)
			return 1
		}
		if action == "status" {
			st, err := mgr.Status(ctx)
			if err != nil {
				fmt.Fprintf(a.stderr, "local-server service status (%s mode): %v\n", m, err)
				code = 1
				continue
			}
			// Asked about both modes: say only what is installed, and one line if nothing is.
			if len(modes) > 1 && !st.Installed {
				continue
			}
			fmt.Fprintf(a.stdout, "local-server background service, %s\n", st)
			shown++
			continue
		}
		var out service.Outcome
		switch action {
		case "uninstall":
			out, err = mgr.Uninstall(ctx)
		case "start":
			out, err = mgr.Start(ctx)
		case "stop":
			out, err = mgr.Stop(ctx)
		}
		if err != nil {
			return a.serviceFailed(action, m, err)
		}
		a.serviceReport(action, m, out)
	}
	if action == "status" && shown == 0 && code == 0 {
		fmt.Fprintln(a.stdout, "local-server background service: not installed (run `local-server service install`)")
	}
	return code
}

func (a *app) serviceInstall(ctx context.Context, base service.Spec, mode service.Mode, dataDirFlag, runAs string,
	delay time.Duration, count int, reset time.Duration, linger bool) int {
	s := base
	s.Mode, s.RunAs, s.RestartDelay, s.RestartCount, s.RestartResetPeriod, s.Linger = mode, runAs, delay, count, reset, linger
	var err error
	if mode == service.ModeSystem {
		// A system service has no user configuration directory: only an explicit directory counts.
		dir := dataDirFlag
		if dir == "" {
			dir = a.getenv("LS_DATA_DIR")
		}
		if dir != "" {
			s.DataDir, err = filepath.Abs(dir)
		}
	} else {
		s.DataDir, err = a.dataDir(dataDirFlag)
	}
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	if s.Binary, err = a.hookExecutable(); err != nil {
		fmt.Fprintf(a.stderr, "local-server: cannot find this program's own path: %v\n", err)
		return 1
	}
	if s.UserName, err = a.hookUserName(); err != nil {
		fmt.Fprintf(a.stderr, "local-server: cannot find the current user: %v\n", err)
		return 1
	}
	if err := s.Validate(); err != nil {
		fmt.Fprintf(a.stderr, "local-server service install: %v\n", err)
		return 2
	}
	mgr, err := a.hookManager(s)
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server service install: %v\n", err)
		return 1
	}
	out, err := mgr.Install(ctx)
	if err != nil {
		return a.serviceFailed("install", mode, err)
	}
	a.serviceReport("install", mode, out)
	if out.Changed {
		fmt.Fprintf(a.stdout, "Start it now with: local-server service start%s\n", modeArg(mode))
	}
	return 0
}

func modeArg(m service.Mode) string {
	if m == service.ModeSystem {
		return " --mode system"
	}
	return ""
}

func (a *app) serviceFailed(action string, m service.Mode, err error) int {
	if errors.Is(err, service.ErrNotInstalled) {
		fmt.Fprintf(a.stderr, "local-server service %s: %s (%s mode); run `local-server service install%s` first\n",
			action, err, m, modeArg(m))
		return 1
	}
	fmt.Fprintf(a.stderr, "local-server service %s (%s mode): %v\n", action, m, err)
	return 1
}

// serviceReport says what a command did, in plain words.
func (a *app) serviceReport(action string, m service.Mode, out service.Outcome) {
	past := map[string]string{"install": "installed", "uninstall": "removed", "start": "started", "stop": "stopped"}[action]
	if out.Changed {
		fmt.Fprintf(a.stdout, "local-server background service %s (%s mode).\n", past, m)
	} else if len(out.Notes) == 0 {
		fmt.Fprintf(a.stdout, "local-server background service: nothing to do (%s mode).\n", m)
	}
	for _, n := range out.Notes {
		fmt.Fprintf(a.stdout, "local-server background service (%s mode): %s.\n", m, n)
	}
}
