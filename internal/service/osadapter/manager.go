package osadapter

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"

	"github.com/inCoderKaka/local-server/internal/service"
)

// probed is what a backend found out about the service.
type probed struct {
	installed bool
	// running is true while the service process is up.
	running bool
	// stoppable is true when Stop has something to do: running, or loaded and waiting to restart.
	stoppable bool
}

// backend is one OS family's way to install and control the service. The manager around it holds
// the logic every family shares.
type backend interface {
	install(ctx context.Context) (service.Outcome, error)
	uninstall(ctx context.Context) (service.Outcome, error)
	probe(ctx context.Context) (probed, error)
	start(ctx context.Context, p probed) error
	stop(ctx context.Context, p probed) error
}

type manager struct {
	mode service.Mode
	b    backend
}

// New returns the Manager for the Spec's OS family and mode. It checks the Spec's identity only;
// Install checks the whole Spec.
func New(spec service.Spec, env Env) (service.Manager, error) {
	if err := spec.ValidateIdentity(); err != nil {
		return nil, err
	}
	c := common{spec: spec, env: env}
	var b backend
	switch {
	case spec.Target == service.TargetDarwin:
		b = &launchd{c}
	case spec.Target == service.TargetLinux:
		b = &systemd{c}
	case spec.Mode == service.ModeUser:
		b = &logonTask{c}
	default:
		b = &windowsService{c}
	}
	return &manager{mode: spec.Mode, b: b}, nil
}

func (m *manager) Install(ctx context.Context) (service.Outcome, error) { return m.b.install(ctx) }

func (m *manager) Uninstall(ctx context.Context) (service.Outcome, error) {
	return m.b.uninstall(ctx)
}

func (m *manager) Start(ctx context.Context) (service.Outcome, error) {
	p, err := m.b.probe(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	if !p.installed {
		return service.Outcome{}, service.ErrNotInstalled
	}
	if p.running {
		return service.Outcome{Notes: []string{"already running"}}, nil
	}
	if err := m.b.start(ctx, p); err != nil {
		return service.Outcome{}, err
	}
	return service.Outcome{Changed: true}, nil
}

func (m *manager) Stop(ctx context.Context) (service.Outcome, error) {
	p, err := m.b.probe(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	// A job still loaded after its definition file was removed by hand can still be stopped.
	if !p.installed && !p.stoppable {
		return service.Outcome{Notes: []string{"not installed, nothing to stop"}}, nil
	}
	if !p.stoppable {
		return service.Outcome{Notes: []string{"not running"}}, nil
	}
	if err := m.b.stop(ctx, p); err != nil {
		return service.Outcome{}, err
	}
	return service.Outcome{Changed: true}, nil
}

func (m *manager) Status(ctx context.Context) (service.Status, error) {
	p, err := m.b.probe(ctx)
	if err != nil {
		return service.Status{}, err
	}
	return service.Status{Mode: m.mode, Installed: p.installed, Running: p.running}, nil
}

// common is what every backend holds.
type common struct {
	spec service.Spec
	env  Env
}

// run runs a tool and returns its Result; a tool that cannot run at all is an error that names it.
func (c common) run(ctx context.Context, name string, args ...string) (Result, error) {
	r, err := c.env.Runner.Run(ctx, name, args...)
	if err != nil {
		if errors.Is(err, ErrToolMissing) {
			return Result{}, fmt.Errorf("cannot find %s, which this operating system's service manager needs", name)
		}
		return Result{}, fmt.Errorf("run %s: %w", name, err)
	}
	return r, nil
}

// mustRun is run, and a non-zero exit is an error carrying what the tool said.
func (c common) mustRun(ctx context.Context, name string, args ...string) (Result, error) {
	r, err := c.run(ctx, name, args...)
	if err != nil {
		return r, err
	}
	if r.Code != 0 {
		return r, c.toolFailed(name, args, r)
	}
	return r, nil
}

func (c common) toolFailed(name string, args []string, r Result) error {
	if c.spec.Mode == service.ModeSystem && looksLikeNoRights(r.Output) {
		return service.ErrNeedsAdmin
	}
	said := strings.TrimSpace(r.Output)
	const maxSaid = 300
	if len(said) > maxSaid {
		said = said[:maxSaid] + "..."
	}
	step := name
	if len(args) > 0 {
		step += " " + args[0]
		if args[0] == "--user" && len(args) > 1 {
			step += " " + args[1]
		}
	}
	if said == "" {
		return fmt.Errorf("%s failed (exit code %d)", step, r.Code)
	}
	return fmt.Errorf("%s failed (exit code %d): %s", step, r.Code, said)
}

// writeDefinition writes a definition file and reports whether it changed and whether there was
// one before.
func (c common) writeDefinition(path, content string) (changed, existed bool, err error) {
	old, rerr := c.env.FS.ReadFile(path)
	switch {
	case rerr == nil:
		existed = true
		if string(old) == content {
			return false, true, nil
		}
	case !errors.Is(rerr, fs.ErrNotExist):
		return false, false, c.fileError("read", path, rerr)
	}
	if err := c.env.FS.WriteFile(path, []byte(content), 0o644); err != nil {
		return false, existed, c.fileError("write", path, err)
	}
	return true, existed, nil
}

func (c common) exists(path string) (bool, error) {
	_, err := c.env.FS.ReadFile(path)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, fs.ErrNotExist):
		return false, nil
	}
	return false, c.fileError("read", path, err)
}

func (c common) removeFile(path string) error {
	if err := c.env.FS.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return c.fileError("remove", path, err)
	}
	return nil
}

func (c common) fileError(verb, path string, err error) error {
	if errors.Is(err, fs.ErrPermission) && c.spec.Mode == service.ModeSystem {
		return service.ErrNeedsAdmin
	}
	return fmt.Errorf("%s %s: %w", verb, path, err)
}
