package main

import (
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func TestStartupDefaults(t *testing.T) {
	st, err := newStore(nil)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := loadStartup(context.Background(), defaultValues{st})
	if err != nil {
		t.Fatal(err)
	}
	want := startupConfig{
		OllamaURL: "http://127.0.0.1:11434", ConsumerListen: "127.0.0.1:8484", MetricsListen: "127.0.0.1:8485",
		ShutdownTimeout: 10 * time.Second, ReadHeaderTimeout: 10 * time.Second, LogLevel: slog.LevelInfo,
		LogMaxAgeDays: 14, LogMaxSizeMB: 50, RestartBackoff: 5 * time.Second, KeychainTimeout: 2 * time.Minute,
	}
	if cfg != want {
		t.Errorf("defaults = %+v\nwant      %+v", cfg, want)
	}
}

// Every startup setting is host-owned and has a description (req_001 D-38).
func TestStartupDefsAreHostOwnedAndDescribed(t *testing.T) {
	defs := append(startupDefs(), hostkeys.KeychainTimeoutDef)
	for _, d := range defs {
		if d.Owner != settings.Host || len(d.Description) < 20 {
			t.Errorf("%s: owner %q, description %q", d.Key, d.Owner, d.Description)
		}
	}
}

func TestStartupSettingsRefuseBadValues(t *testing.T) {
	ctx := context.Background()
	d, err := openDatabase(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.db.Close()
	bad := map[string][]string{
		keyOllamaBaseURL:            {"", "not a url", "ftp://host", "127.0.0.1:11434"},
		keyConsumerListen:           {"8484", "host", "127.0.0.1:99999", "127.0.0.1:x"},
		keyMetricsListen:            {"0.0.0.0:8485", "192.168.1.5:8485", "example.com:80", "8485"},
		keyShutdownTimeout:          {"0s", "-1s", "soon"},
		keyReadHeaderTimeout:        {"0s"},
		keyWorkerRestartBackoff:     {"0s"},
		hostkeys.KeyKeychainTimeout: {"0s", "-2m"},
		keyLogLevel:                 {"loud", ""},
		keyLogMaxAgeDays:            {"0", "-1"},
		keyLogMaxSizeMB:             {"0"},
	}
	for key, vals := range bad {
		for _, v := range vals {
			if err := d.st.Set(ctx, settings.Host, key, "", v); err == nil {
				t.Errorf("Set(%s, %q) was accepted", key, v)
			}
		}
	}
	good := map[string]string{
		keyMetricsListen: "localhost:9000", keyConsumerListen: "0.0.0.0:0", keyLogLevel: "debug",
		keyOllamaBaseURL: "http://gpu-box:11434", hostkeys.KeyKeychainTimeout: "30s",
	}
	for key, v := range good {
		if err := d.st.Set(ctx, settings.Host, key, "", v); err != nil {
			t.Errorf("Set(%s, %q): %v", key, v, err)
		}
	}
	cfg, err := loadStartup(ctx, d.st)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.LogLevel != slog.LevelDebug || cfg.KeychainTimeout != 30*time.Second || cfg.OllamaURL != "http://gpu-box:11434" {
		t.Errorf("stored values not read: %+v", cfg)
	}
}

// A bad value stored some other way stops startup with an error naming the setting.
func TestLoadStartupNamesTheBadSetting(t *testing.T) {
	ctx := context.Background()
	d, err := openDatabase(ctx, t.TempDir()+"/t.db")
	if err != nil {
		t.Fatal(err)
	}
	defer d.db.Close()
	err = d.db.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`INSERT INTO hst_setting (key, scope, value, updated_by, updated_at) VALUES (?, '', '0.0.0.0:8485', 'host', 0)`, keyMetricsListen)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = loadStartup(ctx, d.st)
	if err == nil || !strings.Contains(err.Error(), keyMetricsListen) {
		t.Fatalf("loadStartup = %v; want an error naming %s", err, keyMetricsListen)
	}
}

// storage.synchronous is read from the opened database, and the database is reopened with that
// mode when it differs from the default it was first opened with (req_001 D-76).
func TestOpenDatabaseAppliesSynchronousSetting(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct{ name, set, want string }{
		{"default stays FULL", "", storage.SyncFull},
		{"NORMAL reopens", storage.SyncNormal, storage.SyncNormal},
		{"explicit FULL", storage.SyncFull, storage.SyncFull},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.set != "" {
				seed(t, dir, map[string]string{storage.KeySynchronous: tc.set})
			}
			d, err := openDatabase(ctx, dir+"/"+dbFileName)
			if err != nil {
				t.Fatal(err)
			}
			defer d.db.Close()
			if d.syncMode != tc.want {
				t.Errorf("opened with %s, want %s", d.syncMode, tc.want)
			}
			// The reopened database works and still holds the stored setting.
			if got, err := d.st.String(ctx, storage.KeySynchronous, ""); err != nil || (tc.set != "" && got != tc.set) {
				t.Errorf("setting after reopen = %q, %v", got, err)
			}
			if err := d.st.Set(ctx, settings.Host, keyLogLevel, "", "warn"); err != nil {
				t.Errorf("write after reopen: %v", err)
			}
		})
	}
}
