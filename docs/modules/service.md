# service module

## Purpose

Running in the background as a user or system service.

## Owns

- Packages: `internal/service/` (the `Manager` port, the `Spec`, and the pure functions that render the launchd plist, systemd unit, Windows logon task and Windows service definition, plus the user-session helper's definition), `internal/service/kardianos/` (the adapter: installs and controls the service per OS through an injected command runner and file system, and registers the Windows system service through `kardianos/service`) (PLT-F-041).
- Tables: none.
- Port: `service`.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-30. The module map is in `docs/design_docs/lld.md` section 1.
