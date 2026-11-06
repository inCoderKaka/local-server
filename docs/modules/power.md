# power module

## Purpose

The sleep lock while sharing is on.

## Owns

- Packages: `internal/power/` (built by PLT-F-042).
- Tables: none (the module stores nothing; no migration).
- Port: `Power`. `Acquire` takes the sleep lock and returns a `Lock` (`Release`, safe to call more than once; `Alive`, false once released or once the OS lock vanished). One adapter per OS sits in the same package, chosen by build tag, and `power.New(log)` returns it; `cmd/local-server` calls it and passes the result into `wire`, and tests pass a fake, so no test starts a real OS helper.
  - macOS (`power_darwin.go`): `caffeinate -i -w <pid>`, so the helper ends with local-server even after a crash.
  - Linux (`power_linux.go`): a logind inhibitor, `systemd-inhibit --what=sleep:idle --mode=block tail --pid=<pid> -f /dev/null`, best effort.
  - Windows (`power_windows.go`): `SetThreadExecutionState(ES_CONTINUOUS | ES_SYSTEM_REQUIRED)` through the standard library (`syscall.NewLazyDLL("kernel32.dll")`; no new dependency). The flag belongs to the thread that set it, so each lock owns a goroutine pinned to its OS thread until released (`thread.go`); the release clears the flag on that thread.
  - Other OS (`power_other.go`): holds nothing and logs it once.
  - The macOS and Linux helpers run behind the `Runner` port (`ExecRunner` is the real one). A missing tool (`ErrToolMissing`) is logged once and is not an error: `Acquire` returns a lock that holds nothing, so the host keeps sharing. A helper that exits by itself makes `Alive` false and the Keeper takes the lock again. Release and shutdown kill the helper and wait for it.
- Worker: `Keeper` (`keeper.go`), registered under the supervisor in `cmd/local-server` as the worker `power`. Every `power.check_interval` it reads `host.SharingState.Effective` (through the small `StateSource` interface) and holds the lock exactly while the state is `sharing`: it releases it while paused (D-06) and in quiet hours, and on shutdown. A state it cannot read leaves the lock as it is; an unusable quiet-hours schedule is logged and the state that comes with the error is honoured. It does not change the sharing state. Gauge `power_sleep_lock_held` (1 or 0).
- Settings (host-owned, D-38, D-54): `power.check_interval` (15 s; zero or less turns the sleep lock off, and the Keeper looks again every 15 s for a change).
- Lid-close and a manual sleep cannot be blocked by any of these (D-30).

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters. `internal/power` imports `internal/host` (the `State` type), `internal/settings` and `internal/telemetry`.

## Decisions

D-30, D-06, D-38, D-54. The module map is in `docs/design_docs/lld.md` section 1.
