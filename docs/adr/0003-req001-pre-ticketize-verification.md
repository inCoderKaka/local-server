# ADR 0003: req_001 pre-ticketize verification — context overflow and Windows engine install

**Status:** Accepted
**Date:** 2026-10-30
**Extends:** [ADR 0002](0002-req001-system-design.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_001.md`
**Decisions:** D-48, D-49 and facts F-06..F-10 in [`req_001.decisions.md`](../tracks/platform/phase_1/requirements/req_001.decisions.md). The log is the source.
**executive_inputs_reference:** execetive_inputs/26-10-30_platform_req_001_pre-ticketize.md, execetive_inputs/26-10-30_platform_req_001_design-sessions-verbatim.md (your exact prompts and answers, both design sessions)

## Context

ADR 0002 left five claims labelled Unsourced, to verify before ticketize. Research with fetched sources resolved them (F-06..F-10). Three confirm the design as written:
- Funnel is on every plan, in beta, non-commercial on Personal (F-06).
- curl avoids quarantine and Go ad-hoc signs arm64 binaries (F-07).
- `zalando/go-keyring` works on all three OSes with no cgo, with a file fallback on headless Linux (F-08).

Two needed a decision:
- Ollama **silently truncates** over-long chat prompts by default (F-09).
- `OllamaSetup.exe` supports Inno Setup's silent flags and installs per user (F-10).

## Decision

- **D-48 Context overflow is a grant setting.** Each grant version has `on_context_overflow`.
  - `refuse` is the default. The adapter sends `truncate:false` and `shift:false` on `/api/chat` and `truncate:false` on `/api/embed`, and maps the error to `context_too_long`.
  - `truncate` lets Ollama drop the oldest messages. The host owns this setting.
  - Because the chat fields are undocumented, a minimum Ollama version is pinned and a contract test checks the refuse behaviour.
- **D-49 Windows engine install.** After the host answers yes, the installer runs `OllamaSetup.exe /SILENT`: no wizard, a progress window, per user with no admin, and Ollama starts afterwards. It never runs without the host's yes.

## Alternatives considered

For context overflow:
- Always refuse.
- Allow truncation but flag it.

The user chose the per-grant setting, which defaults to refuse.

For the Windows install:
- The full wizard.
- `/VERYSILENT`.

The user chose `/SILENT`.

## Consequences

- No consumer gets an answer built on a silently cut prompt unless their host chose that for their grant, which keeps D-32's "never silently ignored".
- local-server depends on undocumented Ollama request fields. The pinned version and contract test catch a break on Ollama upgrades.
- Embedding can still error near the limit (Ollama #14186), so the adapter handles that error.
- The installers must fetch with curl, never through a browser, and the release pipeline must not alter built darwin binaries (F-07).
- Funnel is beta and its bandwidth is unpublished, so public mode stays opt-in (D-24).
- Still unsourced: LaunchAgent behaviour on macOS 15/26 for the unsigned binary, and whether Ollama starts after a silent install (read from the script, not observed). Test before the first release.

## Sources

- **Ollama:** [server/prompt.go](https://raw.githubusercontent.com/ollama/ollama/main/server/prompt.go), [api/types.go](https://raw.githubusercontent.com/ollama/ollama/main/api/types.go), [embed API](https://docs.ollama.com/api/embed), [#14259](https://github.com/ollama/ollama/issues/14259), [#14186](https://github.com/ollama/ollama/issues/14186), [Nojoin PR #343](https://github.com/Valtora/Nojoin/pull/343) (secondary)
- **Windows install:** [Ollama Windows](https://docs.ollama.com/windows), [app/ollama.iss](https://raw.githubusercontent.com/ollama/ollama/main/app/ollama.iss), [Inno Setup command line](https://jrsoftware.org/ishelp/topic_setupcmdline.htm), [Inno Setup [Run]](https://jrsoftware.org/ishelp/topic_runsection.htm)
- **Tailscale:** [Funnel KB](https://tailscale.com/kb/1223/funnel), [Funnel docs](https://tailscale.com/docs/features/tailscale-funnel.md), [pricing](https://tailscale.com/pricing)
- **macOS signing and quarantine:** [Apple forums 788787](https://developer.apple.com/forums/thread/788787), [Eclectic Light quarantine](https://eclecticlight.co/2020/10/29/quarantine-and-the-quarantine-flag/), [golang/go#42684](https://github.com/golang/go/issues/42684)
- **Keychain:** [zalando/go-keyring](https://raw.githubusercontent.com/zalando/go-keyring/master/README.md), [99designs keychain.go](https://raw.githubusercontent.com/99designs/keyring/master/keychain.go)
