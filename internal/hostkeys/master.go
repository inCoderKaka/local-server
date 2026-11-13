package hostkeys

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zalando/go-keyring"

	"github.com/inCoderKaka/local-server/internal/settings"
)

// The OS keychain entry that holds the host master key.
const (
	keychainService = "local-server"
	keychainUser    = "host-master-key"
)

// MasterKeySource says where the master key lives.
type MasterKeySource string

// The places a master key can live (D-40).
const (
	SourceKeychain MasterKeySource = "keychain"
	SourceFile     MasterKeySource = "file"
)

// KeyKeychainTimeout is the host-owned setting that bounds every OS keychain call, on every OS
// (req_001 D-81, D-102, D-38). Startup reads it after the database opens and passes it in
// MasterKeyConfig.Timeout; the Def is KeychainTimeoutDef.
const KeyKeychainTimeout = "hostkeys.keychain_timeout"

// KeychainTimeoutDef is the definition of KeyKeychainTimeout: 2 minutes, long enough for a
// person to answer an unlock prompt, short enough that a headless host does not hang forever.
var KeychainTimeoutDef = settings.Def{
	Key: KeyKeychainTimeout, Kind: settings.Duration, Default: "2m", Owner: settings.Host,
	Description: "Longest local-server waits for the operating system's keychain (for example an unanswered unlock prompt) when it reads or writes the host master key. On a first run a timeout makes it keep the key in a protected file instead; on an existing install it then tries the key file, and stops with an error only if neither holds the key that opens the host. A change applies on the next start.",
}

// Keychain is the OS secret store. The OS keychain is used when MasterKeyConfig.Keychain is
// nil; tests pass a fake, so they never touch the developer's real keychain.
type Keychain interface {
	// Get returns the secret, or ErrKeychainNotFound when there is none.
	Get(service, user string) (string, error)
	Set(service, user, secret string) error
}

// ErrKeychainNotFound is what Keychain.Get returns when the keychain works but holds no entry.
var ErrKeychainNotFound = keyring.ErrNotFound

// ErrMasterKeyNotFound is returned (wrapped) by LoadMasterKey with Create false when the keychain
// answered with no entry and the key file does not exist.
var ErrMasterKeyNotFound = errors.New("hostkeys: the host master key is not in the OS keychain or at the key file")

// ErrKeychainTimeout is returned (wrapped) when a keychain call did not answer in time.
var ErrKeychainTimeout = errors.New("hostkeys: the OS keychain did not answer in time")

type osKeychain struct{}

func (osKeychain) Get(service, user string) (string, error) { return keyring.Get(service, user) }
func (osKeychain) Set(service, user, secret string) error   { return keyring.Set(service, user, secret) }

// SystemKeychain returns the OS keychain (macOS Keychain, Windows Credential Manager, Linux
// Secret Service).
func SystemKeychain() Keychain { return osKeychain{} }

// timeoutError names the setting so the host knows what to change (req_001 D-81, D-102).
func timeoutError(timeout time.Duration) error {
	return fmt.Errorf("%w (waited %s): unlock the keychain or raise %s", ErrKeychainTimeout, timeout, KeyKeychainTimeout)
}

// callBounded runs fn and gives up after timeout, on every OS (req_001 D-102): go-keyring has
// no cancel and, on Linux, waits for an unlock prompt with no limit of its own. On a timeout
// the goroutine running fn is left to finish by itself; the buffered channel lets it exit
// without anyone reading, so it leaks only until the OS call returns.
func callBounded[T any](timeout time.Duration, fn func() (T, error)) (T, error) {
	type result struct {
		v   T
		err error
	}
	ch := make(chan result, 1)
	go func() {
		v, err := fn()
		ch <- result{v, err}
	}()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-t.C:
		var zero T
		return zero, timeoutError(timeout)
	}
}

// MasterKeyConfig is the input of LoadMasterKey.
type MasterKeyConfig struct {
	// FallbackPath is the user-only key file used where the keychain is unavailable. Required.
	FallbackPath string
	// Create allows creating the key on first use. Startup passes !Bootstrapped(...), judged
	// only from the host rows in the database and never from a missing key file, so an
	// existing install never gets a new master key (its secrets are sealed under the old one)
	// and a timeout cannot silently switch key stores (req_001 D-102).
	Create bool
	// Verify says whether a candidate key opens this host's sealed data (MasterKeyVerifier): nil
	// when it does, an error wrapping ErrOpen when it does not, any other error when the check
	// itself could not run (LoadMasterKey then stops with it).
	// Required when Create is false: an existing install uses the key that opens its host, from
	// the keychain first and then the key file, so a stale or different key in one store never
	// stops the other from being used (req_001 D-114). Ignored when Create is true, because
	// there is no host yet.
	Verify func(key []byte) error
	// Timeout bounds each keychain call (the setting KeyKeychainTimeout). It must be greater
	// than zero: there is no unbounded mode (req_001 D-102).
	Timeout time.Duration
	// Keychain is the secret store. Nil means the OS keychain.
	Keychain Keychain
}

// ErrMasterKeyMismatch is returned (wrapped) by LoadMasterKey with Create false when a store
// holds a master key but it does not open this host (req_001 D-114).
var ErrMasterKeyMismatch = errors.New("hostkeys: a master key was found but it does not open this host")

// masterKeyError is the refusal of an existing install: it names both stores and says why
// neither worked. It carries the sentinels that describe the causes, so callers can match them.
type masterKeyError struct {
	msg    string
	causes []error
}

func (e *masterKeyError) Error() string   { return e.msg }
func (e *masterKeyError) Unwrap() []error { return e.causes }

// LoadMasterKey returns the host master key. With Create it creates the key on first use; with
// Create false it returns the key that opens the existing host (Verify), from the OS keychain
// first and then from the key file, and reports which store it came from; if neither opens it,
// the error names both stores and why each was refused (req_001 D-114). The OS keychain is macOS
// Keychain, Windows Credential Manager or Linux Secret Service. Where it is unavailable, for
// example on a headless Linux host, a first run keeps the key in a user-only (0600) file at
// FallbackPath instead. A key already in the file stays there even if the keychain later becomes
// available. The key is never logged and never put in an error.
//
// Every keychain call is bounded by Timeout (req_001 D-81, D-102). A timeout on a first run uses
// the key file, as an unavailable keychain does. On an existing install a timeout moves on to
// the key file, and the key file is used only if its key opens the host, so a stale leftover
// cannot be used by mistake and no key is ever created (req_001 D-102, D-114). If a timed-out
// write to the keychain finishes later, it stores the same key the file holds.
func LoadMasterKey(cfg MasterKeyConfig) ([]byte, MasterKeySource, error) {
	if cfg.FallbackPath == "" {
		return nil, "", errors.New("hostkeys: a fallback path for the master key file is required")
	}
	if cfg.Timeout <= 0 {
		return nil, "", fmt.Errorf("hostkeys: %s must be greater than zero", KeyKeychainTimeout)
	}
	if !cfg.Create && cfg.Verify == nil {
		return nil, "", errors.New("hostkeys: an existing host needs a Verify function to pick its master key")
	}
	kc := cfg.Keychain
	if kc == nil {
		kc = SystemKeychain()
	}
	if !cfg.Create {
		return loadExistingMasterKey(cfg, kc)
	}
	// A first run: no host rows exist, so any key is as good as any other.
	fallbackPath := cfg.FallbackPath
	v, err := callBounded(cfg.Timeout, func() (string, error) { return kc.Get(keychainService, keychainUser) })
	if err == nil {
		k, derr := decodeKey(v)
		if derr != nil {
			return nil, "", fmt.Errorf("hostkeys: keychain entry is damaged: %w", derr)
		}
		return k, SourceKeychain, nil
	}
	// The keychain holds no key, is unavailable, or timed out: use a key file that is already
	// there, else make a key.
	if k, ferr := readKeyFile(fallbackPath); ferr == nil {
		return k, SourceFile, nil
	} else if !errors.Is(ferr, fs.ErrNotExist) {
		return nil, "", ferr
	}
	k, kerr := randomKey()
	if kerr != nil {
		return nil, "", kerr
	}
	if errors.Is(err, ErrKeychainNotFound) {
		_, serr := callBounded(cfg.Timeout, func() (struct{}, error) {
			return struct{}{}, kc.Set(keychainService, keychainUser, encodeKey(k))
		})
		if serr == nil {
			return k, SourceKeychain, nil
		}
	}
	return createKeyFile(fallbackPath, k)
}

// loadExistingMasterKey tries the keychain, then the key file, and returns the first key that
// opens the host (req_001 D-114). It never writes to either store.
func loadExistingMasterKey(cfg MasterKeyConfig, kc Keychain) ([]byte, MasterKeySource, error) {
	var causes []error
	mismatch := false

	var kcWhy string
	v, err := callBounded(cfg.Timeout, func() (string, error) { return kc.Get(keychainService, keychainUser) })
	switch {
	case err == nil:
		k, derr := decodeKey(v)
		switch {
		case derr != nil:
			kcWhy = "its entry is damaged (" + derr.Error() + ")"
		default:
			switch verr := cfg.Verify(k); {
			case verr == nil:
				return k, SourceKeychain, nil
			case !errors.Is(verr, ErrOpen):
				return nil, "", fmt.Errorf("hostkeys: check the keychain's master key against the host: %w", verr)
			}
			kcWhy = "it holds a key that does not open this host"
			mismatch = true
		}
	case errors.Is(err, ErrKeychainNotFound):
		kcWhy = "it holds no master key"
	case errors.Is(err, ErrKeychainTimeout):
		kcWhy = err.Error() // names the setting to raise
		causes = append(causes, ErrKeychainTimeout)
	default:
		kcWhy = "it is unavailable or locked: " + err.Error()
	}

	var fileWhy string
	k, ferr := readKeyFile(cfg.FallbackPath)
	switch {
	case ferr == nil:
		switch verr := cfg.Verify(k); {
		case verr == nil:
			return k, SourceFile, nil
		case !errors.Is(verr, ErrOpen):
			return nil, "", fmt.Errorf("hostkeys: check the key file's master key against the host: %w", verr)
		}
		fileWhy = "it holds a key that does not open this host"
		mismatch = true
	case errors.Is(ferr, fs.ErrNotExist):
		fileWhy = "there is no such file"
	default:
		fileWhy = ferr.Error()
	}

	switch {
	case mismatch:
		causes = append(causes, ErrMasterKeyMismatch)
	case errors.Is(err, ErrKeychainNotFound) && errors.Is(ferr, fs.ErrNotExist):
		causes = append(causes, ErrMasterKeyNotFound)
	}
	msg := fmt.Sprintf("hostkeys: no master key opens this host. OS keychain: %s. Key file %s: %s. This database was created with its master key, so restore it to the keychain or to the key file rather than starting over", kcWhy, cfg.FallbackPath, fileWhy)
	return nil, "", &masterKeyError{msg: msg, causes: causes}
}

// KeychainStatus is what CheckKeychain found.
type KeychainStatus string

// The outcomes of CheckKeychain.
const (
	// KeychainHasKey: the keychain answered and holds the host master key.
	KeychainHasKey KeychainStatus = "has_key"
	// KeychainEmpty: the keychain answered and holds no master key (a first run, or the key is in the file).
	KeychainEmpty KeychainStatus = "empty"
)

// CheckKeychain reports whether the keychain answers within timeout. It only reads: it never
// creates or writes a key. A keychain that is unavailable, locked or too slow is an error; a
// slow one wraps ErrKeychainTimeout.
func CheckKeychain(kc Keychain, timeout time.Duration) (KeychainStatus, error) {
	if timeout <= 0 {
		return "", fmt.Errorf("hostkeys: %s must be greater than zero", KeyKeychainTimeout)
	}
	if kc == nil {
		kc = SystemKeychain()
	}
	_, err := callBounded(timeout, func() (string, error) { return kc.Get(keychainService, keychainUser) })
	switch {
	case err == nil:
		return KeychainHasKey, nil
	case errors.Is(err, ErrKeychainNotFound):
		return KeychainEmpty, nil
	}
	return "", err
}

func encodeKey(k []byte) string { return base64.StdEncoding.EncodeToString(k) }

func decodeKey(s string) ([]byte, error) {
	k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || len(k) != KeySize {
		return nil, errors.New("not a valid key")
	}
	return k, nil
}

func readKeyFile(path string) ([]byte, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	// Windows has no group/other permission bits to check; its profile folder is user-only.
	if runtime.GOOS != "windows" && fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("hostkeys: master key file %s is readable by other users (mode %o); fix it with chmod 600", path, fi.Mode().Perm())
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	k, err := decodeKey(string(b))
	if err != nil {
		return nil, fmt.Errorf("hostkeys: master key file %s is damaged: %w", path, err)
	}
	return k, nil
}

func createKeyFile(path string, k []byte) ([]byte, MasterKeySource, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, "", err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return nil, "", fmt.Errorf("hostkeys: create master key file: %w", err)
	}
	if err := f.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		f.Close()
		os.Remove(path)
		return nil, "", err
	}
	if _, err := f.WriteString(encodeKey(k) + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, "", err
	}
	// The host rows that depend on this key are committed (and synced by SQLite) after this
	// returns, so the key must be on disk first, or a power loss could leave a host nobody can
	// open. A crash between the create above and this write can still leave an empty file; that
	// window is microseconds, it exists only on a first run, and readKeyFile names the file.
	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(path)
		return nil, "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, "", err
	}
	if runtime.GOOS != "windows" { // best effort: make the new directory entry durable too
		if dir, err := os.Open(filepath.Dir(path)); err == nil {
			dir.Sync()
			dir.Close()
		}
	}
	return k, SourceFile, nil
}
