package service

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite testdata/*.golden")

// testSpec is a Spec with a fake binary path for the target: the tests never depend on the host
// OS, so every path is chosen by the target (req_001 D-30).
func testSpec(t Target, m Mode) Spec {
	s := Spec{Target: t, Mode: m, Name: DefaultName(t),
		RestartDelay: DefaultRestartDelay, RestartCount: DefaultRestartCount, RestartResetPeriod: DefaultRestartResetPeriod}
	switch t {
	case TargetDarwin:
		s.Binary = "/usr/local/bin/local-server"
		s.DataDir = "/Users/ana/Library/Application Support/local-server"
		s.UserName = "ana"
	case TargetLinux:
		s.Binary = "/home/ana/.local/bin/local-server"
		s.DataDir = "/home/ana/.config/local-server"
		s.UserName = "ana"
	case TargetWindows:
		s.Binary = `C:\Users\ana\AppData\Local\Programs\local-server\local-server.exe`
		s.DataDir = `C:\Users\ana\AppData\Roaming\local-server`
		s.UserName = `DESKTOP-1\ana`
	}
	if m == ModeSystem {
		s.RunAs = map[Target]string{TargetDarwin: "ana", TargetLinux: "ana", TargetWindows: ""}[t]
		if t == TargetWindows {
			s.Binary = `C:\Program Files\local-server\local-server.exe`
			s.DataDir = `C:\ProgramData\local-server`
		}
	}
	return s
}

// TestGolden renders every definition and compares it with testdata/<name>.golden. Run
// `go test ./internal/service -run TestGolden -update` to rewrite the files after a change.
// Every definition must also say it restarts local-server after an unexpected exit only.
func TestGolden(t *testing.T) {
	cases := []struct {
		file string
		spec Spec
		kind Kind
		// policy lines that must appear: the restart-on-unexpected-exit policy of req_001 D-30.
		policy []string
	}{
		{"darwin_user_launchagent.plist", testSpec(TargetDarwin, ModeUser), KindService,
			[]string{"<key>KeepAlive</key>", "<key>SuccessfulExit</key>\n\t\t<false/>", "<integer>10</integer>"}},
		{"linux_user.service", testSpec(TargetLinux, ModeUser), KindService,
			[]string{"Restart=on-failure", "RestartSec=10s", "WantedBy=default.target"}},
		{"windows_user_logon_task.xml", testSpec(TargetWindows, ModeUser), KindService,
			[]string{"<RestartOnFailure>", "<Interval>PT1M</Interval>", "<Count>999</Count>", "<LogonTrigger>"}},
		{"darwin_system_launchdaemon.plist", testSpec(TargetDarwin, ModeSystem), KindService,
			[]string{"<key>KeepAlive</key>", "<key>SuccessfulExit</key>\n\t\t<false/>", "<key>UserName</key>"}},
		{"linux_system.service", testSpec(TargetLinux, ModeSystem), KindService,
			[]string{"Restart=on-failure", "RestartSec=10s", "WantedBy=multi-user.target", "User=ana"}},
		{"windows_system_service.txt", testSpec(TargetWindows, ModeSystem), KindService,
			[]string{"recovery on unexpected exit: restart the service after 10s", "start type: automatic"}},
		{"helper_darwin.plist", testSpec(TargetDarwin, ModeSystem), KindHelper,
			[]string{"<key>SuccessfulExit</key>\n\t\t<false/>", "<string>notify-helper</string>", "com.github.incoderkaka.local-server.helper"}},
		{"helper_linux.service", testSpec(TargetLinux, ModeSystem), KindHelper,
			[]string{"Restart=on-failure", "WantedBy=default.target", "notify-helper"}},
		{"helper_windows_logon_task.xml", testSpec(TargetWindows, ModeSystem), KindHelper,
			[]string{"<RestartOnFailure>", "<LogonTrigger>", "notify-helper"}},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			got, err := Render(c.spec, c.kind)
			if err != nil {
				t.Fatal(err)
			}
			for _, p := range c.policy {
				if !strings.Contains(got, p) {
					t.Errorf("definition lacks %q:\n%s", p, got)
				}
			}
			file := filepath.Join("testdata", c.file+".golden")
			if *update {
				if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(file)
			if err != nil {
				t.Fatalf("%v (run with -update)", err)
			}
			// A Windows checkout may turn the golden file's line ends into CRLF.
			want = []byte(strings.ReplaceAll(string(want), "\r\n", "\n"))
			if string(want) != got {
				t.Errorf("%s is out of date (run with -update):\n--- got ---\n%s\n--- want ---\n%s", file, got, want)
			}
		})
	}
}

// A clean exit must not restart: no definition may ask for restart "always".
func TestNoRestartAlways(t *testing.T) {
	for _, tg := range []Target{TargetDarwin, TargetLinux, TargetWindows} {
		for _, m := range []Mode{ModeUser, ModeSystem} {
			for _, k := range []Kind{KindService, KindHelper} {
				got, err := Render(testSpec(tg, m), k)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(got, "Restart=always") || strings.Contains(got, "<key>KeepAlive</key>\n\t<true/>") {
					t.Errorf("%s %s kind %d restarts after a clean exit:\n%s", tg, m, k, got)
				}
			}
		}
	}
}

func TestRestartDelayIsRendered(t *testing.T) {
	s := testSpec(TargetLinux, ModeUser)
	s.RestartDelay = 45 * 1e9
	got, _ := Render(s, KindService)
	if !strings.Contains(got, "RestartSec=45s") {
		t.Errorf("restart delay not rendered:\n%s", got)
	}
	w := testSpec(TargetWindows, ModeUser)
	w.RestartDelay = 61 * 1e9
	got, _ = Render(w, KindService)
	if !strings.Contains(got, "<Interval>PT2M</Interval>") {
		t.Errorf("Task Scheduler interval is not rounded up to whole minutes:\n%s", got)
	}
}

func TestQuoting(t *testing.T) {
	s := testSpec(TargetLinux, ModeUser)
	s.DataDir = "/home/a b/50%/it's"
	got, _ := Render(s, KindService)
	if !strings.Contains(got, `--data-dir "/home/a b/50%%/it's"`) {
		t.Errorf("systemd quoting wrong:\n%s", got)
	}
	if g := windowsQuote(`C:\a b\`); g != `"C:\a b\\"` {
		t.Errorf("windowsQuote trailing backslash: %s", g)
	}
	if g := windowsQuote(`say "hi"`); g != `"say \"hi\""` {
		t.Errorf("windowsQuote quote: %s", g)
	}
	p := testSpec(TargetDarwin, ModeUser)
	p.DataDir = "/Users/a&b/<x>"
	got, _ = Render(p, KindService)
	if !strings.Contains(got, "/Users/a&amp;b/&lt;x&gt;") {
		t.Errorf("plist not escaped:\n%s", got)
	}
}

func TestValidate(t *testing.T) {
	bad := map[string]func(*Spec){
		"relative binary":   func(s *Spec) { s.Binary = "local-server" },
		"relative data dir": func(s *Spec) { s.DataDir = "data" },
		"bad name":          func(s *Spec) { s.Name = "a b" },
		"empty name":        func(s *Spec) { s.Name = "" },
		"bad mode":          func(s *Spec) { s.Mode = "both" },
		"short delay":       func(s *Spec) { s.RestartDelay = 0 },
		"zero count":        func(s *Spec) { s.RestartCount = 0 },
		"huge count":        func(s *Spec) { s.RestartCount = 1000 },
		"run-as in user":    func(s *Spec) { s.RunAs = "root" },
		"bad target":        func(s *Spec) { s.Target = "plan9" },
	}
	for name, mut := range bad {
		s := testSpec(TargetLinux, ModeUser)
		mut(&s)
		if _, err := Render(s, KindService); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
	sys := testSpec(TargetLinux, ModeSystem)
	sys.DataDir = ""
	if err := sys.Validate(); err == nil {
		t.Error("system mode without a data directory must be refused")
	}
	win := testSpec(TargetWindows, ModeUser)
	win.UserName = ""
	if err := win.Validate(); err == nil {
		t.Error("a Windows logon task without a user must be refused")
	}
	// A user-mode Spec without a data directory is valid: the binary resolves its default.
	u := testSpec(TargetLinux, ModeUser)
	u.DataDir = ""
	got, err := Render(u, KindService)
	if err != nil || strings.Contains(got, "--data-dir") {
		t.Errorf("no data dir: err %v\n%s", err, got)
	}
}

func TestStatusString(t *testing.T) {
	for want, s := range map[string]Status{
		"user mode: not installed":        {Mode: ModeUser},
		"system mode: installed, running": {Mode: ModeSystem, Installed: true, Running: true},
		"user mode: installed, stopped":   {Mode: ModeUser, Installed: true},
	} {
		if s.String() != want {
			t.Errorf("got %q want %q", s.String(), want)
		}
	}
}
