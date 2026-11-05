package main

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth/credential"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

const (
	rcUser = "recovery-admin"
	rcPass = "a long enough password"
)

// firstRun serves once on a terminal (printing the recovery code), creates the admin over the
// admin API, stops, and returns the code the first run showed.
func firstRun(t *testing.T, ta *testApp, createAdmin bool) string {
	t.Helper()
	seed(t, ta.dir, loopbackAny)
	r := startServe(t, ta, "--engine", "fake")
	m := codeRe.FindStringSubmatch(ta.err.String())
	if m == nil {
		t.Fatalf("no recovery code on stderr:\n%s", ta.err.String())
	}
	if createAdmin {
		resp, err := testClient.Post("http://"+r.addrs.Admin+"/admin/v1/setup", "application/json",
			strings.NewReader(`{"username":"`+rcUser+`","password":"`+rcPass+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != http.StatusCreated {
			t.Fatalf("setup = %d", resp.StatusCode)
		}
	}
	if code := r.stop(); code != 0 {
		t.Fatalf("serve exit = %d", code)
	}
	return m[1]
}

// recoveryApp is an app for the recovery-code command over the install of ta, typing the given
// login names and passwords, one per attempt.
func recoveryApp(t *testing.T, ta *testApp, stdinTerminal, stderrTerminal bool, attempts ...[2]string) *testApp {
	t.Helper()
	ra := newTestApp(t, stderrTerminal)
	ra.dir, ra.kc, ra.keychain, ra.logDir = ta.dir, ta.kc, ta.kc, ta.logDir
	var names, passwords []string
	for _, a := range attempts {
		names = append(names, a[0])
		passwords = append(passwords, a[1])
	}
	ra.stdin = strings.NewReader(strings.Join(names, "\n") + "\n")
	ra.stdinIsTerminal = func() bool { return stdinTerminal }
	i := 0
	ra.readPassword = func() (string, error) {
		if i >= len(passwords) {
			return "", io.EOF
		}
		i++
		return passwords[i-1], nil
	}
	return ra
}

func (ta *testApp) recovery() int {
	return ta.run(context.Background(), []string{"recovery-code", "--data-dir", ta.dir})
}

func auditCount(t *testing.T, dir, action string) int {
	t.Helper()
	return count(t, dir, `SELECT count(*) FROM aud_event WHERE action = '`+action+`'`)
}

// Without a terminal on both stdin and stderr the command refuses before it opens the database
// or touches a key (req_001 D-101).
func TestRecoveryCodeNeedsTwoTerminals(t *testing.T) {
	for name, c := range map[string]struct{ in, errTerm bool }{"stdin is not a terminal": {false, true}, "stderr is not a terminal": {true, false}, "neither": {false, false}} {
		ta := newTestApp(t, true)
		ra := recoveryApp(t, ta, c.in, c.errTerm, [2]string{rcUser, rcPass})
		if code := ra.recovery(); code != 1 {
			t.Errorf("%s: exit = %d, want 1", name, code)
		}
		contains(t, name+": stderr", ra.err.String(), "terminal")
		if exists(filepath.Join(ra.dir, dbFileName)) {
			t.Errorf("%s: the database was opened (and so created)", name)
		}
		if gets, sets := ra.kc.calls(); gets != 0 || sets != 0 {
			t.Errorf("%s: the keychain was used", name)
		}
		if ra.out.String() != "" {
			t.Errorf("%s: stdout = %q", name, ra.out.String())
		}
	}
}

func TestRecoveryCodeNeedsAHostAndAnAdmin(t *testing.T) {
	t.Run("no database", func(t *testing.T) {
		ta := newTestApp(t, true)
		ra := recoveryApp(t, ta, true, true, [2]string{rcUser, rcPass})
		if code := ra.recovery(); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		contains(t, "stderr", ra.err.String(), "local-server serve")
		if exists(filepath.Join(ra.dir, dbFileName)) {
			t.Error("the command created a database")
		}
	})
	t.Run("database without a host", func(t *testing.T) {
		ta := newTestApp(t, true)
		seed(t, ta.dir, nil)
		ra := recoveryApp(t, ta, true, true, [2]string{rcUser, rcPass})
		if code := ra.recovery(); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		contains(t, "stderr", ra.err.String(), "local-server serve")
		if n := count(t, ta.dir, `SELECT count(*) FROM hst_host`); n != 0 {
			t.Errorf("the command created %d hosts", n)
		}
		if gets, sets := ta.kc.calls(); gets != 0 || sets != 0 {
			t.Error("the keychain was used")
		}
	})
	t.Run("host without an admin", func(t *testing.T) {
		ta := newTestApp(t, true)
		firstRun(t, ta, false)
		ra := recoveryApp(t, ta, true, true, [2]string{rcUser, rcPass})
		if code := ra.recovery(); code != 1 {
			t.Fatalf("exit = %d", code)
		}
		contains(t, "stderr", ra.err.String(), "no admin", "local-server serve")
		if n := auditCount(t, ta.dir, "host.recovery_code_shown"); n != 0 {
			t.Errorf("audit rows = %d", n)
		}
	})
}

// A wrong password or an unknown name shows no code and writes no audit row, with the one fixed
// message; the lockout stops the command after its threshold.
func TestRecoveryCodeWrongLoginShowsNothing(t *testing.T) {
	ta := newTestApp(t, true)
	code := firstRun(t, ta, true)

	ra := recoveryApp(t, ta, true, true, [2]string{rcUser, "wrong password!!"}, [2]string{"nobody", rcPass}, [2]string{rcUser, rcPass})
	// The third attempt is right; the first two fail and the command asks again.
	if exit := ra.recovery(); exit != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", exit, ra.err.String())
	}
	out := ra.err.String()
	if n := strings.Count(out, "local-server recovery-code: login name or password is wrong"); n != 2 {
		t.Errorf("fixed message appears %d times, want 2:\n%s", n, out)
	}
	if strings.Count(out, code) != 1 {
		t.Errorf("the code appears %d times:\n%s", strings.Count(out, code), out)
	}

	t.Run("only wrong logins", func(t *testing.T) {
		ta := newTestApp(t, true)
		code := firstRun(t, ta, true)
		ra := recoveryApp(t, ta, true, true, [2]string{rcUser, "wrong password!!"}, [2]string{"nobody", rcPass})
		if exit := ra.recovery(); exit != 1 {
			t.Fatalf("exit = %d, want 1 once the input ends", exit)
		}
		if strings.Contains(ra.err.String(), code) {
			t.Error("the code was shown after wrong logins")
		}
		contains(t, "stderr", ra.err.String(), "login name or password is wrong")
		if n := auditCount(t, ta.dir, "host.recovery_code_shown"); n != 0 {
			t.Errorf("audit rows = %d", n)
		}
		if _, sets := ra.kc.calls(); sets != 1 {
			t.Errorf("keychain sets = %d", sets) // the master key of the first run only
		}
	})
}

func TestRecoveryCodeLockoutStopsTheCommand(t *testing.T) {
	ta := newTestApp(t, true)
	code := firstRun(t, ta, true)
	// Two failures lock out.
	d, err := openDatabase(context.Background(), filepath.Join(ta.dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	if err := credential.RegisterLockout(d.st); err != nil {
		t.Fatal(err)
	}
	if err := d.st.Set(context.Background(), settings.Host, credential.KeyLockoutFailures, "", "2"); err != nil {
		t.Fatal(err)
	}
	d.db.Close()

	bad := [2]string{rcUser, "wrong password!!"}
	ra := recoveryApp(t, ta, true, true, bad, bad, [2]string{rcUser, rcPass})
	if exit := ra.recovery(); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	contains(t, "stderr", ra.err.String(), "too many failed attempts")
	if strings.Contains(ra.err.String(), code) {
		t.Error("the code was shown by a locked-out command")
	}
	if n := auditCount(t, ta.dir, "host.recovery_code_shown"); n != 0 {
		t.Errorf("audit rows = %d", n)
	}
}

// The right login prints the code once, on stderr, after the audit row is committed; the code is
// the one the first run showed and is nowhere else (req_001 D-80, D-101).
func TestRecoveryCodeShownAfterLoginAndAudited(t *testing.T) {
	ta := newTestApp(t, true)
	code := firstRun(t, ta, true)
	ra := recoveryApp(t, ta, true, true, [2]string{" " + rcUser + " ", rcPass})
	if exit := ra.recovery(); exit != 0 {
		t.Fatalf("exit = %d; stderr:\n%s", exit, ra.err.String())
	}
	out := ra.err.String()
	if n := strings.Count(out, code); n != 1 {
		t.Fatalf("the code appears %d times on stderr, want once:\n%s", n, out)
	}
	if ra.out.String() != "" {
		t.Errorf("stdout = %q; the code must go to stderr only", ra.out.String())
	}
	contains(t, "stderr", out, "Admin login name:", "Password:", "audit")
	if strings.Contains(out, rcPass) {
		t.Error("the password was echoed")
	}
	// The code is the data key's: it recovers.
	if _, err := hostkeys.RecoverDataKey(code); err != nil {
		t.Errorf("RecoverDataKey: %v", err)
	}

	// One audit row, with the admin as the actor and no code.
	if n := auditCount(t, ta.dir, "host.recovery_code_shown"); n != 1 {
		t.Fatalf("audit rows = %d, want 1", n)
	}
	db, err := storage.OpenReadOnly(context.Background(), filepath.Join(ta.dir, dbFileName), registryModules()...)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var actor, admin, detail string
	if err := db.Reader().QueryRow(context.Background(), `SELECT actor_id, detail FROM aud_event WHERE action = 'host.recovery_code_shown'`).Scan(&actor, &detail); err != nil {
		t.Fatal(err)
	}
	if err := db.Reader().QueryRow(context.Background(), `SELECT principal_id FROM idn_admin_user`).Scan(&admin); err != nil {
		t.Fatal(err)
	}
	if actor != admin {
		t.Errorf("actor = %q, want the admin %q", actor, admin)
	}
	if strings.Contains(detail, code) || strings.Contains(strings.ReplaceAll(detail, "-", ""), strings.ReplaceAll(code, "-", "")) {
		t.Errorf("the audit detail holds the code: %s", detail)
	}
	// Not in the log file either.
	if logs, err := os.ReadFile(filepath.Join(ta.logDir, logFileName)); err == nil && strings.Contains(string(logs), code) {
		t.Error("the recovery code reached the log file")
	}
	// And a second run shows it again, and audits again.
	ra2 := recoveryApp(t, ta, true, true, [2]string{rcUser, rcPass})
	if exit := ra2.recovery(); exit != 0 || !strings.Contains(ra2.err.String(), code) {
		t.Fatalf("second run: exit %d", exit)
	}
	if n := auditCount(t, ta.dir, "host.recovery_code_shown"); n != 2 {
		t.Errorf("audit rows = %d, want 2", n)
	}
}

// If the audit row cannot be written the code is not shown (req_001 D-80: audited).
func TestRecoveryCodeNotShownWhenTheAuditWriteFails(t *testing.T) {
	ta := newTestApp(t, true)
	code := firstRun(t, ta, true)
	d, err := openDatabase(context.Background(), filepath.Join(ta.dir, dbFileName))
	if err != nil {
		t.Fatal(err)
	}
	err = d.db.Write(context.Background(), func(tx *storage.Tx) error {
		_, err := tx.Exec(`CREATE TRIGGER test_no_recovery_audit BEFORE INSERT ON aud_event WHEN NEW.action = 'host.recovery_code_shown' BEGIN SELECT RAISE(ABORT, 'audit down'); END`)
		return err
	})
	d.db.Close()
	if err != nil {
		t.Fatal(err)
	}
	ra := recoveryApp(t, ta, true, true, [2]string{rcUser, rcPass})
	if exit := ra.recovery(); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if strings.Contains(ra.err.String(), code) || strings.Contains(ra.out.String(), code) {
		t.Error("the code was shown although the audit write failed")
	}
	contains(t, "stderr", ra.err.String(), "audit")
}

// Ctrl-C (a cancelled context) at the password prompt ends the command at once: a terminal read
// does not watch the context, so the prompt is abandoned. Nothing is shown and nothing is audited.
func TestRecoveryCodeInterruptedAtThePasswordPrompt(t *testing.T) {
	ta := newTestApp(t, true)
	code := firstRun(t, ta, true)
	ra := recoveryApp(t, ta, true, true, [2]string{rcUser, rcPass})
	block := make(chan struct{})
	t.Cleanup(func() { close(block) })
	ra.readPassword = func() (string, error) {
		<-block
		return "", io.EOF
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	exit := make(chan int, 1)
	go func() { exit <- ra.run(ctx, []string{"recovery-code", "--data-dir", ra.dir}) }()
	waitFor(t, "the password prompt", func() bool { return strings.Contains(ra.err.String(), "Password:") })
	cancel()
	select {
	case got := <-exit:
		if got != 1 {
			t.Errorf("exit = %d, want 1; stderr:\n%s", got, ra.err.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the command did not stop after the context was cancelled")
	}
	contains(t, "stderr", ra.err.String(), "interrupted")
	if strings.Contains(ra.err.String(), code) || strings.Contains(ra.out.String(), code) {
		t.Error("the code was shown after an interrupt")
	}
	if n := auditCount(t, ta.dir, "host.recovery_code_shown"); n != 0 {
		t.Errorf("audit rows = %d, want 0", n)
	}
}

func TestRecoveryCodeUsage(t *testing.T) {
	ta := newTestApp(t, true)
	if code := ta.run(context.Background(), []string{"recovery-code", "extra"}); code != 2 {
		t.Errorf("exit = %d, want 2", code)
	}
	if code := ta.run(context.Background(), []string{"recovery-code", "--nope"}); code != 2 {
		t.Errorf("unknown flag exit = %d, want 2", code)
	}
}
