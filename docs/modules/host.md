# host module

## Purpose

The host machine's own state: sharing on, paused or quiet, its keys, and measured host and share-budget metrics.

## Owns

- Packages: `internal/host/`, `internal/hostkeys/`, `internal/hostmetrics/` (built by PLT-F-005, PLT-F-012, PLT-F-013).
- Tables: `hst_*` (the settings module owns `hst_setting`; this module owns `hst_host`, `hst_key`), migrations in `internal/host/migrations/`, registered by `host.Module()`.
- Port: `HostMetrics`.
- Sharing state (`internal/host`): `sharing`, `paused` and `quiet`. Only the manual switch is stored (`hst_host.sharing_state`); `quiet` is derived from the clock and the host-owned `host.quiet_hours.*` settings (enabled, time zone, start, end; off by default; a window whose end is before its start crosses midnight), so a schedule transition writes nothing. A pause wins over quiet. Pause and resume write an audit row in the same unit of work as the state change; a pause also emits one `host.paused` event. `SharingState.EffectiveAt` reports the state for the scheduler and `/v1/health`. The clock is a port so tests are deterministic.
- Keys (`internal/hostkeys`): an Ed25519 identity key whose public key is the host id, and receipt and checkpoint subkeys that carry identity-signed certificates and rotate on host settings. Secrets are sealed with AES-256-GCM under the host data key, which is itself wrapped by the host master key. The master key is read from the OS keychain, then from a user-only file. Only a first run may create one, so an existing install whose keychain is locked or empty fails instead of starting with a new key. The recovery code is the data key itself, shown once at setup and never stored.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-06, D-21, D-25, D-27, D-31, D-38, D-40, D-41, D-44, D-54, D-65. The module map is in `docs/design_docs/lld.md` section 1.
