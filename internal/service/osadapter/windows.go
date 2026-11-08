package osadapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf16"

	ksvc "github.com/kardianos/service"

	"github.com/inCoderKaka/local-server/internal/service"
)

// logonTask installs the user-mode Windows service: a Task Scheduler task that starts at the
// logon of the current user, needs no administrator rights and is restarted when it fails
// (req_001 D-30). schtasks reads the definition from an XML file.
type logonTask struct{ common }

// utf16LE encodes text as UTF-16 little endian with a byte order mark, which is what schtasks
// reads for a task XML that declares encoding="UTF-16".
func utf16LE(s string) []byte {
	u := utf16.Encode([]rune(s))
	b := make([]byte, 0, 2+2*len(u))
	b = append(b, 0xFF, 0xFE)
	for _, c := range u {
		b = append(b, byte(c), byte(c>>8))
	}
	return b
}

func (w *logonTask) taskInstalled(ctx context.Context) (bool, error) {
	r, err := w.run(ctx, "schtasks", "/Query", "/TN", w.spec.Name)
	return r.Code == 0 && err == nil, err
}

func (w *logonTask) install(ctx context.Context) (service.Outcome, error) {
	def, err := service.Render(w.spec, service.KindService)
	if err != nil {
		return service.Outcome{}, err
	}
	existed, err := w.taskInstalled(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	tmp := strings.TrimRight(w.env.TempDir, `\/`) + `\` + w.spec.Name + ".xml"
	if err := w.env.FS.WriteFile(tmp, utf16LE(def), 0o600); err != nil {
		return service.Outcome{}, w.fileError("write", tmp, err)
	}
	defer w.env.FS.Remove(tmp) //nolint:errcheck // a temporary file; nothing to do if it is gone
	if _, err := w.mustRun(ctx, "schtasks", "/Create", "/TN", w.spec.Name, "/XML", tmp, "/F"); err != nil {
		return service.Outcome{}, err
	}
	out := service.Outcome{Changed: true}
	if existed {
		out.Notes = append(out.Notes, "already installed, the definition was replaced; if the service is running, stop and start it to apply the change")
	}
	return out, nil
}

func (w *logonTask) probe(ctx context.Context) (probed, error) {
	installed, err := w.taskInstalled(ctx)
	if err != nil || !installed {
		return probed{}, err
	}
	// Get-ScheduledTask prints the state in English whatever the display language is; schtasks
	// /Query prints it translated.
	r, err := w.run(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command",
		fmt.Sprintf("(Get-ScheduledTask -TaskName '%s').State", w.spec.Name))
	if err != nil {
		return probed{}, err
	}
	// The task exists, so a PowerShell failure means the state is unknown, not stopped.
	if r.Code != 0 {
		return probed{}, w.toolFailed("powershell", []string{"Get-ScheduledTask"}, r)
	}
	running := strings.TrimSpace(r.Output) == "Running"
	return probed{installed: true, running: running, stoppable: running}, nil
}

func (w *logonTask) start(ctx context.Context, _ probed) error {
	_, err := w.mustRun(ctx, "schtasks", "/Run", "/TN", w.spec.Name)
	return err
}

func (w *logonTask) stop(ctx context.Context, _ probed) error {
	_, err := w.mustRun(ctx, "schtasks", "/End", "/TN", w.spec.Name)
	return err
}

func (w *logonTask) uninstall(ctx context.Context) (service.Outcome, error) {
	installed, err := w.taskInstalled(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	if !installed {
		return service.Outcome{Notes: []string{"not installed, nothing to remove"}}, nil
	}
	// A task that is not running makes /End fail; that is fine, the delete is what counts.
	if _, err := w.run(ctx, "schtasks", "/End", "/TN", w.spec.Name); err != nil {
		return service.Outcome{}, err
	}
	if _, err := w.mustRun(ctx, "schtasks", "/Delete", "/TN", w.spec.Name, "/F"); err != nil {
		return service.Outcome{}, err
	}
	return service.Outcome{Changed: true}, nil
}

// windowsService installs the system-mode Windows service through kardianos/service, which calls
// the Service Control Manager directly and sets the recovery action "restart" (req_001 D-30).
type windowsService struct{ common }

// The option keys and values of kardianos/service's Windows service. The library declares them
// only in its Windows file, so they are repeated here to keep this package building on every OS.
const (
	optStartType      = "StartType"
	startAutomatic    = "automatic"
	optOnFailure      = "OnFailure"
	onFailureRestart  = "restart"
	optFailureDelay   = "OnFailureDelayDuration"
	optFailureResetAt = "OnFailureResetPeriod"
)

// kardianosConfig maps the service definition to kardianos/service's config.
func kardianosConfig(def service.WindowsServiceDef) *ksvc.Config {
	return &ksvc.Config{
		Name: def.Name, DisplayName: def.DisplayName, Description: def.Description,
		Executable: def.Executable, Arguments: def.Arguments, UserName: def.RunAs,
		Option: ksvc.KeyValue{
			optStartType:      startAutomatic,
			optOnFailure:      onFailureRestart,
			optFailureDelay:   def.RestartDelay.String(),
			optFailureResetAt: int(def.ResetPeriod / time.Second),
		},
	}
}

func (w *windowsService) open() (WindowsService, error) {
	if w.env.NewWindowsService == nil {
		return nil, errors.New("this build cannot manage Windows services")
	}
	return w.env.NewWindowsService(kardianosConfig(w.spec.WindowsService()))
}

// rights turns an access-denied answer into ErrNeedsAdmin.
func (w *windowsService) rights(err error) error {
	if err != nil && looksLikeNoRights(err.Error()) {
		return service.ErrNeedsAdmin
	}
	return err
}

func (w *windowsService) install(ctx context.Context) (service.Outcome, error) {
	if err := w.spec.Validate(); err != nil {
		return service.Outcome{}, err
	}
	p, err := w.probe(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	if p.installed {
		return service.Outcome{Notes: []string{"already installed; to change its definition, uninstall it first"}}, nil
	}
	svc, err := w.open()
	if err != nil {
		return service.Outcome{}, err
	}
	if err := svc.Install(); err != nil {
		return service.Outcome{}, w.rights(fmt.Errorf("install the Windows service: %w", err))
	}
	return service.Outcome{Changed: true}, nil
}

func (w *windowsService) probe(context.Context) (probed, error) {
	svc, err := w.open()
	if err != nil {
		return probed{}, err
	}
	st, err := svc.Status()
	switch {
	case errors.Is(err, ksvc.ErrNotInstalled):
		return probed{}, nil
	case err != nil:
		return probed{}, w.rights(fmt.Errorf("ask the Windows service manager: %w", err))
	}
	running := st == ksvc.StatusRunning
	return probed{installed: true, running: running, stoppable: running}, nil
}

func (w *windowsService) start(context.Context, probed) error {
	svc, err := w.open()
	if err != nil {
		return err
	}
	return w.rights(svc.Start())
}

func (w *windowsService) stop(context.Context, probed) error {
	svc, err := w.open()
	if err != nil {
		return err
	}
	return w.rights(svc.Stop())
}

func (w *windowsService) uninstall(ctx context.Context) (service.Outcome, error) {
	p, err := w.probe(ctx)
	if err != nil {
		return service.Outcome{}, err
	}
	if !p.installed {
		return service.Outcome{Notes: []string{"not installed, nothing to remove"}}, nil
	}
	svc, err := w.open()
	if err != nil {
		return service.Outcome{}, err
	}
	if p.running {
		if err := svc.Stop(); err != nil {
			return service.Outcome{}, w.rights(fmt.Errorf("stop the Windows service: %w", err))
		}
	}
	if err := svc.Uninstall(); err != nil {
		return service.Outcome{}, w.rights(fmt.Errorf("remove the Windows service: %w", err))
	}
	return service.Outcome{Changed: true}, nil
}
