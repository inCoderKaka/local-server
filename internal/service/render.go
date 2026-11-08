package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Kind says which definition Render produces.
type Kind int

const (
	// KindService is the definition of local-server itself, in the Spec's mode.
	KindService Kind = iota
	// KindHelper is the per-user login entry of the user-session helper that system mode comes
	// with, because desktop notifications only work in the user's session (req_001 D-29, D-30).
	// It is the same in both modes: a login entry of the current user.
	KindHelper
)

const (
	serviceDescription = "local-server shares this machine's AI inference with invited consumers"
	helperDescription  = "local-server desktop notifications for the signed-in user"
)

// Render returns the definition text for the Spec's target, mode and kind:
//
//	macOS    a launchd plist (LaunchAgent in user mode, LaunchDaemon in system mode)
//	Linux    a systemd unit (a user unit in user mode, a system unit in system mode)
//	Windows  the XML of a logon task (user mode), or the description of the Windows service
//	         (system mode)
//
// Every definition restarts local-server after an unexpected exit and leaves it stopped after a
// clean exit or a stop (req_001 D-30). The functions are pure.
func Render(s Spec, k Kind) (string, error) {
	if err := s.Validate(); err != nil {
		return "", err
	}
	if k == KindHelper && s.Target == TargetWindows && s.UserName == "" {
		return "", errors.New("service: the helper's logon task needs the name of the current user")
	}
	d := s.definition(k)
	switch s.Target {
	case TargetDarwin:
		return d.plist(s), nil
	case TargetLinux:
		return d.systemdUnit(s), nil
	}
	if d.perUser {
		return d.taskXML(s), nil
	}
	return d.windowsService(s), nil
}

// definition is what differs between local-server and its helper.
type definition struct {
	name        string
	description string
	// args are the arguments after the binary.
	args []string
	// perUser says it is a login entry of the current user, whatever the Spec's mode.
	perUser bool
	binary  string
}

func (s Spec) definition(k Kind) definition {
	d := definition{name: s.Name, description: serviceDescription, binary: s.Binary,
		args: []string{ServeCommand}, perUser: s.Mode == ModeUser}
	if k == KindHelper {
		d.name, d.description, d.args, d.perUser = s.HelperName(), helperDescription, []string{HelperCommand}, true
	}
	if s.DataDir != "" {
		d.args = append(d.args, "--data-dir", s.DataDir)
	}
	return d
}

// ceilSeconds is d in whole seconds, rounded up.
func ceilSeconds(d time.Duration) int { return int((d + time.Second - 1) / time.Second) }

// xmlEscape escapes text for an XML element.
func xmlEscape(s string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// plist renders a launchd job. KeepAlive/SuccessfulExit=false restarts it only after an exit that
// was not a success (a crash or a failure), so a stop or a clean exit stays stopped. A
// LaunchAgent is loaded in the user's login session; a LaunchDaemon at boot.
func (d definition) plist(s Spec) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	fmt.Fprintf(&b, "\t<key>Label</key>\n\t<string>%s</string>\n", xmlEscape(d.name))
	b.WriteString("\t<key>ProgramArguments</key>\n\t<array>\n")
	for _, a := range append([]string{d.binary}, d.args...) {
		fmt.Fprintf(&b, "\t\t<string>%s</string>\n", xmlEscape(a))
	}
	b.WriteString("\t</array>\n")
	b.WriteString("\t<key>RunAtLoad</key>\n\t<true/>\n")
	b.WriteString("\t<key>KeepAlive</key>\n\t<dict>\n\t\t<key>SuccessfulExit</key>\n\t\t<false/>\n\t</dict>\n")
	fmt.Fprintf(&b, "\t<key>ThrottleInterval</key>\n\t<integer>%d</integer>\n", ceilSeconds(s.RestartDelay))
	if !d.perUser && s.RunAs != "" {
		fmt.Fprintf(&b, "\t<key>UserName</key>\n\t<string>%s</string>\n", xmlEscape(s.RunAs))
	}
	b.WriteString("</dict>\n</plist>\n")
	return b.String()
}

// systemdQuote quotes one word of ExecStart: systemd splits on spaces and expands % and $.
func systemdQuote(a string) string {
	a = strings.NewReplacer("%", "%%", "$", "$$").Replace(a)
	if a == "" || strings.ContainsAny(a, " \t\"'\\;") {
		a = `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a) + `"`
	}
	return a
}

// systemdUnit renders a systemd unit. Restart=on-failure restarts it after a crash or a non-zero
// exit and not after a clean exit or `systemctl stop`. A user unit is wanted by default.target
// (kardianos/service hardcodes multi-user.target, which a user manager does not have), a system unit by
// multi-user.target after the network is up.
func (d definition) systemdUnit(s Spec) string {
	var b strings.Builder
	b.WriteString("[Unit]\n")
	fmt.Fprintf(&b, "Description=%s\n", d.description)
	if !d.perUser {
		b.WriteString("After=network-online.target\nWants=network-online.target\n")
	}
	b.WriteString("\n[Service]\nType=simple\n")
	words := make([]string, 0, len(d.args)+1)
	for _, a := range append([]string{d.binary}, d.args...) {
		words = append(words, systemdQuote(a))
	}
	fmt.Fprintf(&b, "ExecStart=%s\n", strings.Join(words, " "))
	if !d.perUser && s.RunAs != "" {
		fmt.Fprintf(&b, "User=%s\n", s.RunAs)
	}
	fmt.Fprintf(&b, "Restart=on-failure\nRestartSec=%ds\n", ceilSeconds(s.RestartDelay))
	b.WriteString("\n[Install]\n")
	if d.perUser {
		b.WriteString("WantedBy=default.target\n")
	} else {
		b.WriteString("WantedBy=multi-user.target\n")
	}
	return b.String()
}

// windowsQuote quotes one command-line argument the way CommandLineToArgvW reads it.
func windowsQuote(a string) string {
	if a != "" && !strings.ContainsAny(a, " \t\"") {
		return a
	}
	var b strings.Builder
	b.WriteByte('"')
	slashes := 0
	for _, r := range a {
		switch r {
		case '\\':
			slashes++
			b.WriteRune(r)
			continue
		case '"':
			b.WriteString(strings.Repeat(`\`, slashes+1))
		}
		slashes = 0
		b.WriteRune(r)
	}
	b.WriteString(strings.Repeat(`\`, slashes))
	b.WriteByte('"')
	return b.String()
}

// taskXML renders a Task Scheduler task that starts at the logon of one user and is restarted by
// RestartOnFailure when a run fails. Task Scheduler restarts at intervals of whole minutes, at
// least one, so the interval is the restart delay rounded up to a minute.
func (d definition) taskXML(s Spec) string {
	minutes := (ceilSeconds(s.RestartDelay) + 59) / 60
	user := xmlEscape(s.UserName)
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-16\"?>\n")
	b.WriteString("<Task version=\"1.2\" xmlns=\"http://schemas.microsoft.com/windows/2004/02/mit/task\">\n")
	fmt.Fprintf(&b, "  <RegistrationInfo>\n    <Description>%s</Description>\n  </RegistrationInfo>\n", xmlEscape(d.description))
	fmt.Fprintf(&b, "  <Triggers>\n    <LogonTrigger>\n      <Enabled>true</Enabled>\n      <UserId>%s</UserId>\n    </LogonTrigger>\n  </Triggers>\n", user)
	fmt.Fprintf(&b, "  <Principals>\n    <Principal id=\"Author\">\n      <UserId>%s</UserId>\n      <LogonType>InteractiveToken</LogonType>\n      <RunLevel>LeastPrivilege</RunLevel>\n    </Principal>\n  </Principals>\n", user)
	b.WriteString("  <Settings>\n")
	b.WriteString("    <MultipleInstancesPolicy>IgnoreNew</MultipleInstancesPolicy>\n")
	b.WriteString("    <DisallowStartIfOnBatteries>false</DisallowStartIfOnBatteries>\n")
	b.WriteString("    <StopIfGoingOnBatteries>false</StopIfGoingOnBatteries>\n")
	b.WriteString("    <AllowHardTerminate>true</AllowHardTerminate>\n")
	b.WriteString("    <StartWhenAvailable>true</StartWhenAvailable>\n")
	b.WriteString("    <AllowStartOnDemand>true</AllowStartOnDemand>\n")
	b.WriteString("    <Enabled>true</Enabled>\n")
	b.WriteString("    <ExecutionTimeLimit>PT0S</ExecutionTimeLimit>\n")
	fmt.Fprintf(&b, "    <RestartOnFailure>\n      <Interval>PT%dM</Interval>\n      <Count>%d</Count>\n    </RestartOnFailure>\n", minutes, s.RestartCount)
	b.WriteString("  </Settings>\n")
	fmt.Fprintf(&b, "  <Actions Context=\"Author\">\n    <Exec>\n      <Command>%s</Command>\n      <Arguments>%s</Arguments>\n    </Exec>\n  </Actions>\n",
		xmlEscape(d.binary), xmlEscape(strings.Join(quoteAll(d.args), " ")))
	b.WriteString("</Task>\n")
	return b.String()
}

func quoteAll(args []string) []string {
	q := make([]string, len(args))
	for i, a := range args {
		q[i] = windowsQuote(a)
	}
	return q
}

// WindowsServiceName is the account name Windows shows for the default service account.
const WindowsServiceName = "LocalSystem"

// WindowsServiceDef is what the Windows service is registered with: the values the adapter hands
// to the Service Control Manager.
type WindowsServiceDef struct {
	Name, DisplayName, Description string
	Executable                     string
	Arguments                      []string
	// RunAs is the account; empty means LocalSystem. It must be an account that needs no password,
	// such as NT AUTHORITY\LocalService, NT AUTHORITY\NetworkService or a managed service account:
	// the adapter never passes a password to the Service Control Manager.
	RunAs string
	// RestartDelay is the recovery action "restart the service" after an unexpected exit, for
	// every failure; ResetPeriod is when the failure count starts again.
	RestartDelay time.Duration
	ResetPeriod  time.Duration
}

// WindowsService is the definition of the system-mode Windows service. Mode must be system.
func (s Spec) WindowsService() WindowsServiceDef {
	d := s.definition(KindService)
	return WindowsServiceDef{Name: s.Name, DisplayName: s.Name, Description: d.description,
		Executable: s.Binary, Arguments: d.args, RunAs: s.RunAs,
		RestartDelay: time.Duration(ceilSeconds(s.RestartDelay)) * time.Second, ResetPeriod: time.Duration(ceilSeconds(s.RestartResetPeriod)) * time.Second}
}

// windowsService renders WindowsServiceDef as text (golden-tested): a Windows service has no file.
func (d definition) windowsService(s Spec) string {
	w := s.WindowsService()
	runAs := w.RunAs
	if runAs == "" {
		runAs = WindowsServiceName
	}
	var b strings.Builder
	fmt.Fprintf(&b, "service: %s\n", w.Name)
	fmt.Fprintf(&b, "display name: %s\n", w.DisplayName)
	fmt.Fprintf(&b, "description: %s\n", w.Description)
	b.WriteString("start type: automatic\n")
	fmt.Fprintf(&b, "run as: %s\n", runAs)
	fmt.Fprintf(&b, "command line: %s %s\n", windowsQuote(w.Executable), strings.Join(quoteAll(w.Arguments), " "))
	fmt.Fprintf(&b, "recovery on unexpected exit: restart the service after %ds\n", int(w.RestartDelay/time.Second))
	fmt.Fprintf(&b, "recovery failure count resets after: %ds\n", int(w.ResetPeriod/time.Second))
	return b.String()
}
