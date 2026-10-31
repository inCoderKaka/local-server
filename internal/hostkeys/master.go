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

	"github.com/zalando/go-keyring"
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

// LoadMasterKey returns the host master key. With create true it creates the key on first use;
// startup passes !Bootstrapped(...) as create, so an existing install never gets a new master
// key (its secrets are sealed under the old one). With create false a key that cannot be found
// is an error. It prefers the OS keychain (macOS Keychain, Windows Credential Manager, Linux
// Secret Service). Where the keychain is unavailable, for example on a headless Linux host, it
// keeps the key in a user-only (0600) file at fallbackPath instead. A key already in the file
// stays there even if the keychain later becomes available. The key is never logged.
func LoadMasterKey(fallbackPath string, create bool) ([]byte, MasterKeySource, error) {
	if fallbackPath == "" {
		return nil, "", errors.New("hostkeys: a fallback path for the master key file is required")
	}
	v, err := keyring.Get(keychainService, keychainUser)
	switch {
	case err == nil:
		k, derr := decodeKey(v)
		if derr != nil {
			return nil, "", fmt.Errorf("hostkeys: keychain entry is damaged: %w", derr)
		}
		return k, SourceKeychain, nil
	case errors.Is(err, keyring.ErrNotFound):
		if k, ferr := readKeyFile(fallbackPath); ferr == nil {
			return k, SourceFile, nil
		} else if !errors.Is(ferr, fs.ErrNotExist) {
			return nil, "", ferr
		}
		if !create {
			return nil, "", fmt.Errorf("hostkeys: the host master key is not in the OS keychain or at %s; this database was created with it, so restore it rather than starting over", fallbackPath)
		}
		k, kerr := randomKey()
		if kerr != nil {
			return nil, "", kerr
		}
		if serr := keyring.Set(keychainService, keychainUser, encodeKey(k)); serr == nil {
			return k, SourceKeychain, nil
		}
		return createKeyFile(fallbackPath, k)
	default: // keychain unavailable
		if k, ferr := readKeyFile(fallbackPath); ferr == nil {
			return k, SourceFile, nil
		} else if !errors.Is(ferr, fs.ErrNotExist) {
			return nil, "", ferr
		}
		if !create {
			return nil, "", fmt.Errorf("hostkeys: the OS keychain is unavailable or locked: %w; unlock it and start again", err)
		}
		k, kerr := randomKey()
		if kerr != nil {
			return nil, "", kerr
		}
		return createKeyFile(fallbackPath, k)
	}
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
	if err := f.Close(); err != nil {
		os.Remove(path)
		return nil, "", err
	}
	return k, SourceFile, nil
}
