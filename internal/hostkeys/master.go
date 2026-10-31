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
	keyringService = "local-server"
	keyringUser    = "host-master-key"
)

// MasterKeySource says where the master key lives.
type MasterKeySource string

// The places a master key can live (D-40).
const (
	SourceKeyring MasterKeySource = "keyring"
	SourceFile    MasterKeySource = "file"
)

// LoadMasterKey returns the host master key, creating it on first use. It prefers the OS
// keychain (macOS Keychain, Windows Credential Manager, Linux Secret Service). Where the
// keychain is unavailable, for example on a headless Linux host, it keeps the key in a
// user-only (0600) file at fallbackPath instead. A key already in the file stays there even
// if the keychain later becomes available. The key is never logged.
func LoadMasterKey(fallbackPath string) ([]byte, MasterKeySource, error) {
	if fallbackPath == "" {
		return nil, "", errors.New("hostkeys: a fallback path for the master key file is required")
	}
	v, err := keyring.Get(keyringService, keyringUser)
	switch {
	case err == nil:
		k, derr := decodeKey(v)
		if derr != nil {
			return nil, "", fmt.Errorf("hostkeys: keychain entry is damaged: %w", derr)
		}
		return k, SourceKeyring, nil
	case errors.Is(err, keyring.ErrNotFound):
		if k, ferr := readKeyFile(fallbackPath); ferr == nil {
			return k, SourceFile, nil
		} else if !errors.Is(ferr, fs.ErrNotExist) {
			return nil, "", ferr
		}
		k, kerr := randomKey()
		if kerr != nil {
			return nil, "", kerr
		}
		if serr := keyring.Set(keyringService, keyringUser, encodeKey(k)); serr == nil {
			return k, SourceKeyring, nil
		}
		return createKeyFile(fallbackPath, k)
	default: // keychain unavailable
		if k, ferr := readKeyFile(fallbackPath); ferr == nil {
			return k, SourceFile, nil
		} else if !errors.Is(ferr, fs.ErrNotExist) {
			return nil, "", ferr
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
		return nil, "", err
	}
	if _, err := f.WriteString(encodeKey(k) + "\n"); err != nil {
		f.Close()
		os.Remove(path)
		return nil, "", err
	}
	if err := f.Close(); err != nil {
		return nil, "", err
	}
	return k, SourceFile, nil
}
