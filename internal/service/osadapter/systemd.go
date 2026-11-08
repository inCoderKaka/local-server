package osadapter

import (
	"context"
	"path"

	"github.com/inCoderKaka/local-server/internal/service"
)

// systemd installs a systemd unit: a user unit for `systemctl --user` (user mode) or a system
// unit (system mode). In user mode it also asks logind to keep the user's manager running
// without a login, `loginctl enable-linger` (req_001 D-30).
type systemd struct{ common }

func (s *systemd) unit() string { return s.spec.Name + ".service" }

func (s *systemd) unitPath() string {
	if s.spec.Mode == service.ModeSystem {
		return path.Join("/etc/systemd/system", s.unit())
	}
	return path.Join(s.env.ConfigHome, "systemd", "user", s.unit())
}

// systemctl runs systemctl for the mode's manager.
func (s *systemd) systemctl(ctx context.Context, must bool, args ...string) (Result, error) {
	if s.spec.Mode == service.ModeUser {
		args = append([]string{"--user"}, args...)
	}
	if must {
		return s.mustRun(ctx, "systemctl", args...)
	}
	return s.run(ctx, "systemctl", args...)
}

func (s *systemd) install(ctx context.Context) (service.Outcome, error) {
	def, err := service.Render(s.spec, service.KindService)
	if err != nil {
		return service.Outcome{}, err
	}
	changed, existed, err := s.writeDefinition(s.unitPath(), def)
	if err != nil {
		return service.Outcome{}, err
	}
	var out service.Outcome
	out.Changed = changed
	switch {
	case !changed:
		out.Notes = append(out.Notes, "already installed, the definition is unchanged")
	case existed:
		out.Notes = append(out.Notes, "already installed, the definition was updated; if the service is running, stop and start it to apply the change")
	}
	if s.spec.Mode == service.ModeUser && s.spec.Linger {
		// Best effort: a host without logind policy for it can still run the service while logged in.
		if _, err := s.mustRun(ctx, "loginctl", "enable-linger", s.spec.UserName); err != nil {
			out.Notes = append(out.Notes, "could not keep the service running after logout ("+err.Error()+"); it will run only while you are logged in")
		}
	}
	if _, err := s.systemctl(ctx, true, "daemon-reload"); err != nil {
		return service.Outcome{}, err
	}
	if _, err := s.systemctl(ctx, true, "enable", s.unit()); err != nil {
		return service.Outcome{}, err
	}
	return out, nil
}

func (s *systemd) probe(ctx context.Context) (probed, error) {
	installed, err := s.exists(s.unitPath())
	if err != nil {
		return probed{}, err
	}
	r, err := s.systemctl(ctx, false, "is-active", s.unit())
	if err != nil {
		return probed{}, err
	}
	active := r.Code == 0
	return probed{installed: installed, running: active, stoppable: active}, nil
}

func (s *systemd) start(ctx context.Context, _ probed) error {
	_, err := s.systemctl(ctx, true, "start", s.unit())
	return err
}

func (s *systemd) stop(ctx context.Context, _ probed) error {
	_, err := s.systemctl(ctx, true, "stop", s.unit())
	return err
}

func (s *systemd) uninstall(ctx context.Context) (service.Outcome, error) {
	installed, err := s.exists(s.unitPath())
	if err != nil {
		return service.Outcome{}, err
	}
	if !installed {
		return service.Outcome{Notes: []string{"not installed, nothing to remove"}}, nil
	}
	if _, err := s.systemctl(ctx, true, "disable", "--now", s.unit()); err != nil {
		return service.Outcome{}, err
	}
	if err := s.removeFile(s.unitPath()); err != nil {
		return service.Outcome{}, err
	}
	if _, err := s.systemctl(ctx, true, "daemon-reload"); err != nil {
		return service.Outcome{}, err
	}
	out := service.Outcome{Changed: true}
	if s.spec.Mode == service.ModeUser {
		out.Notes = append(out.Notes, "login lingering was left as it is")
	}
	return out, nil
}
