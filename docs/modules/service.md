# service module

## Purpose

Running in the background as a user or system service.

## Owns

- Packages: `internal/service/` (the OS-agnostic part: the `Manager` port, the `Spec` and its validation, and the pure functions that render the launchd plist, systemd unit, Windows logon task and Windows service definition, plus the user-session helper's definition), `internal/service/osadapter/` (the adapter, and the only place that is OS-dependent: it installs and controls the service per OS family by running `launchctl`, `systemctl`, `loginctl` and `schtasks` through an injected command runner and writing files through an injected file system, and registers the Windows system service, and only that, through `kardianos/service`) (PLT-F-041, req_001 D-137).
- Tables: none.
- Port: `service`.

## OS-agnostic and OS-dependent parts

`kardianos/service` registers the Windows system service only. The launchd, systemd and Windows logon-task definitions are rendered by this module and installed by the adapter. The adapter has no build constraints: its code compiles on every OS and is tested with fakes on any host, and the OS family is chosen by the `Spec`'s `Target` value. The CLI passes `runtime.GOOS` as that value, the one place the real OS is chosen.

| Component | OS-agnostic or OS-dependent | Where |
|---|---|---|
| `Spec`, validation (`Validate`, `ValidateIdentity`), `Target`, `Mode`, `Outcome`, `Status` | OS-agnostic | `internal/service/service.go` |
| Defaults (restart delay, count, reset period) and default and helper names (`DefaultName`, `HelperName`) | OS-agnostic (values differ per `Target`, the code does not) | `internal/service/service.go` |
| Rendering of the launchd plist, systemd unit, Windows logon task, Windows service definition and the helper definition | OS-agnostic (pure functions, golden files for every OS family) | `internal/service/render.go`, `internal/service/testdata/` |
| `Manager` port | OS-agnostic (interface) | `internal/service/service.go` |
| CLI: flags, `--mode` and `--name` handling, the sudo guard (user mode is refused when root runs the command through sudo), printing of `Outcome` and `Status` | OS-agnostic; its hooks default to the adapter | `cmd/local-server/service.go` |
| `Runner`, `FS` and `Env` seams | OS-agnostic (interfaces and a value) | `internal/service/osadapter/env.go` |
| Manager shared logic (probe, start, stop and status around a backend) | OS-agnostic logic inside the adapter | `internal/service/osadapter/manager.go` |
| launchd backend (`launchctl`, LaunchAgent and LaunchDaemon files) | OS-dependent behaviour, compiles everywhere, tested with fakes | `internal/service/osadapter/launchd.go` |
| systemd backend (`systemctl`, `loginctl enable-linger`, unit files) | OS-dependent behaviour, compiles everywhere, tested with fakes | `internal/service/osadapter/systemd.go` |
| Windows logon task backend (`schtasks`, UTF-16 task XML) | OS-dependent behaviour, compiles everywhere, tested with fakes | `internal/service/osadapter/windows.go` |
| Windows system service backend (`kardianos/service`) | OS-dependent behaviour, compiles everywhere, tested with a fake `WindowsService` | `internal/service/osadapter/windows.go` |
| Real `Runner` (`os/exec`) and real `FS` (`os`) | OS-dependent, compiles everywhere | `internal/service/osadapter/real.go` |
| User, home folder, user id and binary discovery (`CurrentUserName`, `Executable`, `NewEnv`, `RunByRootThroughSudo`) | OS-dependent, compiles everywhere | `internal/service/osadapter/real.go` |
| Missing-rights detection (`ErrNeedsAdmin`) | OS-dependent in what the OS tools say, compiles everywhere: it reads the tool output and file errors, it does not ask the OS who the user is, so there is no build-tagged privilege check | `internal/service/osadapter/env.go`, `manager.go` |
| Fakes (`Runner`, `FS`, `WindowsService`, CLI hooks) | Test-only | `internal/service/osadapter/manager_test.go`, `cmd/local-server/service_test.go` |

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-30, D-29, D-137. The module map is in `docs/design_docs/lld.md` section 1.
