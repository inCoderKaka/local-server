# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session. Work order: `finish-plan.md` (req_001 D-141..D-160).

## Last session (2026-11-15, passive, M4)

- Built and gated: PLT-B-001 `4c6aca5` (CI actions pinned, Dependabot), PLT-C-003 `517aed6` (`release.yml`, `make release-dry`, macOS checklist), PLT-F-043 `9211b8d` (install, upgrade, uninstall scripts, bats and Pester, two CI jobs). The M4 gate over `2002da6..HEAD` implemented 6 of 95 candidates (`1d2afb2`, `037bc61`, `311520c`) and parked 36 items (`4569d61`).
- Checked: real bats 60 of 60 on macOS (2 skip, no `setsid`); CI green on all three OSes, Pester on Windows. Never run: `release.yml` and the Windows script on a real Windows machine.
- `implementation-status.md` rebuilt (M2, M3 and M4). State: tree clean, 72 unpushed commits. Spend about 0.65 window.

## Open items

- **M2+M3 gate:** fixes `5305511..2002da6` (parts 01 to 07) are committed, but the gate's final report never arrived (rate limit). "Gated" for F-027..F-037 is inferred from commits.
- PLT-F-038 stays `building` until the M1 second-machine check.
- `docs-drift` flags `ROADMAP.md` B8 ("in progress"); it is true until the rc tag and a clean-account install. Remaining drift: 12 stale-decision warnings (D-41..D-47, D-50, D-141..D-144, D-20), `ticket-sizes.md` header totals, `ROADMAP.md:4` points at `next-steps.md`, `ai-mirror --check` not available here.
- Worklog has no gate rows for the M2+M3 and M4 gates.

## Waiting on you

1. ~~Push `main`~~ done 2026-11-15: CI is green on `c96069f` (run 43727505230, all 11 jobs incl. web-admin, install-scripts sh and ps on Windows PowerShell 5.1 and pwsh 7, race on ubuntu and macOS). The first run (7936f4b) failed on a doctor test that needed a local Ollama and on Pester tests sharing TestDrive state; both fixed in `c96069f`. `release.yml` has still never run (it needs a tag).
2. **M1 second-machine check** (`ops/checks/m1-checklist.md`); **M3 browser check** on a second machine (D-146).
3. **After green CI:** push `v0.1.0-rc.1`; run `ops/checks/macos-launchagent-checklist.md` on macOS 15 and 26; run the install command on a clean macOS user account; then push `v0.1.0` (D-156). Until `v0.1.0` exists, `releases/latest/download/install.sh` returns 404, so docs must show `releases/download/v0.1.0-rc.1/install.sh`.
4. **Rulings** (parking lines in `phase-2-parking.md`): (a) system-mode service runs the user-owned binary as root; recommended `--run-as "$(id -un)"` or ship user mode only for rc.1; (b) the user service is registered before the first run, so declining it loops at login; recommended register after the first run.
5. **I-455 (needs-ruling):** is `/usage` plus the in-process e2e harness enough for statement 5? Recommended yes, and turn off failure-trace snapshots.
6. A reaffirm row for D-141..D-144 (D-133 pattern), recommended; and ROADMAP B8 wording, recommended "built, not yet run on real machines".

## Next steps

M5: PLT-C-004 (host setup guide and README, show the rc.1 install URL), user guide, developer guide, HLD and LLD reconciled with the code (the LLD module map lacks `webui`), `/phase-retro platform 1`. Spin-offs stay in `phase-2-parking.md` (D-142).
