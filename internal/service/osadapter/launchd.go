package osadapter

import (
	"context"
	"fmt"
	"path"
	"strings"

	"github.com/inCoderKaka/local-server/internal/service"
)

// launchd installs a macOS LaunchAgent (user mode, in the login session's domain gui/<uid>) or a
// LaunchDaemon (system mode, domain system). Installing writes the plist and nothing else: the job
// is loaded by Start, and launchd loads it at the next login or boot by itself (RunAtLoad).
type launchd struct{ common }

func (l *launchd) plistPath() string {
	if l.spec.Mode == service.ModeSystem {
		return path.Join("/Library/LaunchDaemons", l.spec.Name+".plist")
	}
	return path.Join(l.env.Home, "Library", "LaunchAgents", l.spec.Name+".plist")
}

func (l *launchd) domain() string {
	if l.spec.Mode == service.ModeSystem {
		return "system"
	}
	return fmt.Sprintf("gui/%d", l.env.UID)
}

func (l *launchd) install(ctx context.Context) (service.Outcome, error) {
	def, err := service.Render(l.spec, service.KindService)
	if err != nil {
		return service.Outcome{}, err
	}
	changed, existed, err := l.writeDefinition(l.plistPath(), def)
	if err != nil {
		return service.Outcome{}, err
	}
	switch {
	case !changed:
		return service.Outcome{Notes: []string{"already installed, the definition is unchanged"}}, nil
	case existed:
		return service.Outcome{Changed: true, Notes: []string{"already installed, the definition was updated; if the service is running, stop and start it to apply the change"}}, nil
	}
	return service.Outcome{Changed: true}, nil
}

func (l *launchd) probe(ctx context.Context) (probed, error) {
	installed, err := l.exists(l.plistPath())
	if err != nil {
		return probed{}, err
	}
	r, err := l.run(ctx, "launchctl", "print", l.domain()+"/"+l.spec.Name)
	if err != nil {
		return probed{}, err
	}
	loaded := r.Code == 0
	running := loaded && hasLine(r.Output, "state = running")
	return probed{installed: installed, running: running, stoppable: loaded}, nil
}

// hasLine says whether some line of text, trimmed, is exactly want.
func hasLine(text, want string) bool {
	for _, ln := range strings.Split(text, "\n") {
		if strings.TrimSpace(ln) == want {
			return true
		}
	}
	return false
}

func (l *launchd) start(ctx context.Context, p probed) error {
	// Loaded but not running: launchd is waiting to restart it; kick it now. Not loaded: load it,
	// which starts it (RunAtLoad).
	if p.stoppable {
		_, err := l.mustRun(ctx, "launchctl", "kickstart", l.domain()+"/"+l.spec.Name)
		return err
	}
	_, err := l.mustRun(ctx, "launchctl", "bootstrap", l.domain(), l.plistPath())
	return err
}

func (l *launchd) stop(ctx context.Context, _ probed) error {
	_, err := l.mustRun(ctx, "launchctl", "bootout", l.domain()+"/"+l.spec.Name)
	return err
}

func (l *launchd) uninstall(ctx context.Context) (service.Outcome, error) {
	p, err := l.probe(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	if !p.installed {
		if !p.stoppable {
			return service.Outcome{Notes: []string{"not installed, nothing to remove"}}, nil
		}
		// The plist was removed by hand but the job is still loaded: stop it.
		if err := l.stop(ctx, p); err != nil {
			return service.Outcome{}, err
		}
		return service.Outcome{Changed: true, Notes: []string{"its definition file was already removed; the job that was still loaded was stopped"}}, nil
	}
	if p.stoppable {
		if err := l.stop(ctx, p); err != nil {
			return service.Outcome{}, err
		}
	}
	if err := l.removeFile(l.plistPath()); err != nil {
		return service.Outcome{}, err
	}
	return service.Outcome{Changed: true}, nil
}
