package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/inCoderKaka/local-server/internal/admin"
	"github.com/inCoderKaka/local-server/internal/audit"
	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/identity"
	"github.com/inCoderKaka/local-server/internal/storage"
)

// recoveryAddr is the "remote address" the recovery-code command's login attempts are counted
// under in the login lockout: one local source.
const recoveryAddr = "recovery-code"

// wrongLogin is the one answer to any failed login: it never says which part was wrong.
const wrongLogin = "login name or password is wrong"

const recoveryShownNotice = `
local-server: this is your recovery code:

    %s

It restores this host's backups on this computer, or on another one where the host master key
survives. Backups do not contain the master key; it is kept in the OS keychain or in the file
master.key in the data folder, so copy master.key (or the keychain entry) somewhere safe and
private. Showing the code was recorded in the audit log. It is not written to any log or file;
keep it somewhere safe.

`

// readPasswordFromTerminal reads a line from the terminal without echoing it.
func readPasswordFromTerminal() (string, error) {
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	return string(b), err
}

// recoveryCode is `local-server recovery-code`: it shows the host's recovery code again after the
// admin logs in again on a terminal, and records that it did in the audit log (req_001 D-80,
// D-101). The code is an encoding of the host data key, so nothing new is stored. It is printed
// to stderr only, never through the logger, stdout or a file, and only after the audit row has
// committed: no row, no code. Exit codes: 0 shown, 1 failed, 2 usage.
func (a *app) recoveryCode(ctx context.Context, args []string) int {
	fs := flag.NewFlagSet("recovery-code", flag.ContinueOnError)
	fs.SetOutput(a.stderr)
	dataDirFlag := fs.String("data-dir", "", "data directory (default: $LS_DATA_DIR, else local-server in the user configuration directory)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(a.stderr, "local-server recovery-code: no arguments are accepted")
		return 2
	}
	// Both terminals, before the database or any key is touched: the password is typed at a
	// person's keyboard, and the code is shown only to a person (req_001 D-101).
	if !a.stdinIsTerminal() || !a.stderrIsTerminal() {
		fmt.Fprintln(a.stderr, "local-server recovery-code: this command asks for the admin password and shows a secret, so it must run in a terminal (both stdin and stderr)")
		return 1
	}
	dir, err := a.dataDir(*dataDirFlag)
	if err != nil {
		fmt.Fprintf(a.stderr, "local-server: %v\n", err)
		return 1
	}
	// ReadPassword turns echo off and restores it only when it returns; an interrupted prompt
	// returns early, so the terminal state is restored on every way out.
	if st, err := term.GetState(int(os.Stdin.Fd())); err == nil {
		defer term.Restore(int(os.Stdin.Fd()), st)
	}
	if err := a.runRecoveryCode(ctx, dir); err != nil {
		fmt.Fprintf(a.stderr, "local-server recovery-code: %v\n", err)
		return 1
	}
	return 0
}

// promptOrCancel runs read on its own goroutine and returns its result, or ctx.Err() as soon as
// ctx is cancelled (Ctrl-C or SIGTERM): a terminal read does not watch the context, so without
// this Ctrl-C at a prompt does nothing until Enter.
func promptOrCancel(ctx context.Context, read func() (string, error)) (string, error) {
	type result struct {
		s   string
		err error
	}
	ch := make(chan result, 1) // buffered: the abandoned goroutine never blocks on send
	go func() {
		s, err := read()
		ch <- result{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

var errRunServeFirst = errors.New("this host has no admin yet: with `local-server serve` running, create the first admin (POST /admin/v1/setup on the admin listener), then run this command again")

func (a *app) runRecoveryCode(ctx context.Context, dataDir string) error {
	dbPath := filepath.Join(dataDir, dbFileName)
	if _, err := os.Stat(dbPath); err != nil {
		return fmt.Errorf("no host database in %s; if this host keeps its data elsewhere (a service may set LS_DATA_DIR), pass --data-dir; otherwise run `local-server serve` first", dataDir)
	}
	d, err := openDatabase(ctx, dbPath)
	if err != nil {
		return fmt.Errorf("open the database: %w", err)
	}
	defer d.db.Close()

	if done, err := hostkeys.Bootstrapped(ctx, d.db); err != nil {
		return fmt.Errorf("read host state: %w", err)
	} else if !done {
		return errors.New("this host has not been set up yet: run `local-server serve` first")
	}
	var admins int
	if err := d.db.Reader().QueryRow(ctx, `SELECT count(*) FROM idn_admin_user`).Scan(&admins); err != nil {
		return fmt.Errorf("read admins: %w", err)
	}
	if admins == 0 {
		return errRunServeFirst
	}

	ir := identity.NewRepos(nil)
	au, err := admin.NewAuthenticator(admin.AuthConfig{DB: d.db, Settings: d.st, Identity: ir})
	if err != nil {
		return err
	}
	// The same Authenticate as the HTTP login, with the same lockout settings. The lockout lives
	// in this process, so after the threshold of failures it stops.
	in := bufio.NewReader(a.stdin)
	var who admin.Authenticated
	for {
		fmt.Fprint(a.stderr, "Admin login name: ")
		name, err := promptOrCancel(ctx, func() (string, error) { return in.ReadString('\n') })
		if ctx.Err() != nil {
			fmt.Fprintln(a.stderr)
			return errors.New("interrupted; nothing was shown")
		}
		if err != nil && (err != io.EOF || name == "") {
			fmt.Fprintln(a.stderr)
			return errors.New("no login name was entered")
		}
		fmt.Fprint(a.stderr, "Password: ")
		pw, err := promptOrCancel(ctx, a.readPassword)
		fmt.Fprintln(a.stderr)
		if ctx.Err() != nil {
			return errors.New("interrupted; nothing was shown")
		}
		if err != nil {
			return errors.New("no password was entered")
		}
		who, err = au.Authenticate(ctx, strings.TrimRight(name, "\r\n"), pw, recoveryAddr)
		if err == nil {
			break
		}
		var locked *admin.LockedOutError
		switch {
		case errors.As(err, &locked):
			return fmt.Errorf("too many failed attempts: try again in %s", locked.RetryAfter.Round(time.Second))
		case errors.Is(err, admin.ErrInvalidLogin):
			fmt.Fprintln(a.stderr, "local-server recovery-code:", wrongLogin)
		default:
			return fmt.Errorf("check the login: %w", err)
		}
	}

	// Only now is the master key read and the data key opened.
	timeout, err := d.st.Duration(ctx, hostkeys.KeyKeychainTimeout, "")
	if err != nil {
		return fmt.Errorf("read setting %s: %w", hostkeys.KeyKeychainTimeout, err)
	}
	master, _, err := hostkeys.LoadMasterKey(hostkeys.MasterKeyConfig{
		FallbackPath: filepath.Join(dataDir, masterKeyFileName),
		Create:       false,
		Timeout:      timeout,
		Keychain:     a.keychain,
	})
	if err != nil {
		return err
	}
	keys, fresh, err := hostkeys.Open(ctx, d.db, d.st, master)
	if err != nil {
		return fmt.Errorf("open host keys: %w", err)
	}
	if fresh != "" {
		// The host exists, so Open never makes a new data key. Never show a code from here.
		return errors.New("unexpected: the host keys were created again; nothing is shown")
	}
	code := keys.RecoveryCode()

	// No audit row, no code (req_001 D-80): the row commits first.
	err = d.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := audit.Record(tx, audit.Event{Action: admin.ActionRecoveryCodeShown, ActorID: who.PrincipalID,
			Detail: map[string]string{"command": "recovery-code"}})
		return err
	})
	if err != nil {
		return fmt.Errorf("record the audit event, so the code is not shown: %w", err)
	}
	if _, err := fmt.Fprintf(a.stderr, recoveryShownNotice, code); err != nil {
		return fmt.Errorf("show the recovery code: %w", err)
	}
	return nil
}
