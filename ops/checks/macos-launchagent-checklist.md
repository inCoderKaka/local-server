# macOS LaunchAgent checklist for the unsigned binary (PLT-C-003, req_001 D-30, D-156)

Run by hand, once on macOS 15 and once on macOS 26, on an Apple silicon Mac, signed in to a normal user account (not root, no `sudo` anywhere in this list). What macOS does with an unsigned binary that `curl` downloaded, when it runs as a LaunchAgent, is not yet sourced (req_001 D-30, "Unsourced: Gatekeeper behaviour for an unsigned curl-downloaded binary"). Nothing here is verified yet: every "pass" below is what to look for, not a claim. Record what you see, word for word, in the table at the end.

Values used below, from the service code (`cmd/local-server/service.go`, `internal/service/osadapter/launchd.go`):

- Label (service name): `com.github.incoderkaka.local-server`
- Plist: `~/Library/LaunchAgents/com.github.incoderkaka.local-server.plist`
- launchd domain: `gui/<your user id>`, from `id -u`
- The plist runs `local-server serve --data-dir <data folder>` with `RunAtLoad` and a restart after a crash.
- Data folder (this checklist): `~/ls-test/data`, a throwaway folder given with `--data-dir`, so the real data folder is not created (req_001 D-149). The default is `~/Library/Application Support/local-server`.
- Log file, if the server writes one: `~/Library/Logs/local-server/local-server.log`
- Local ports (defaults): consumer API `127.0.0.1:8484`, admin `127.0.0.1:8485`

`<tag>` is the release tag you test, for example `v0.1.0-rc.1`. `<owner>/<repo>` is the repository the release is in.

1. Make an empty test folder and download from the release with `curl` (not a browser):
   `mkdir -p ~/ls-test && cd ~/ls-test`
   `curl -fsSLO https://github.com/<owner>/<repo>/releases/download/<tag>/local-server_darwin_arm64`
   `curl -fsSLO https://github.com/<owner>/<repo>/releases/download/<tag>/SHA256SUMS`
   Pass: both files download. `xattr local-server_darwin_arm64` prints nothing (no `com.apple.quarantine`).
2. Check the checksum: `shasum -a 256 -c SHA256SUMS --ignore-missing`.
   Pass: it prints `local-server_darwin_arm64: OK`. (`--ignore-missing` skips the five binaries you did not download.)
3. Make it runnable and check the version: `chmod +x local-server_darwin_arm64 && ./local-server_darwin_arm64 --version`.
   Pass: it prints `local-server <tag>` with no Gatekeeper dialog. Record any dialog or `killed` message.
4. Install the LaunchAgent: `./local-server_darwin_arm64 service install --data-dir ~/ls-test/data`.
   Pass: it prints `local-server background service installed (user mode).` and `Start it now with: local-server service start`. The service runs the binary at the path you ran it from; if you move the file later, install again.
   Also check that sudo is refused by design: `sudo ./local-server_darwin_arm64 service install`. Pass: it refuses with "user mode belongs to the signed-in user" and exits non-zero (run it only to see the refusal; if it installs anything, record that as a failure and run `service uninstall` without sudo).
5. Check the plist: `ls -l ~/Library/LaunchAgents/com.github.incoderkaka.local-server.plist`.
   Pass: the file exists. `plutil -lint` on it prints `OK`.
6. First run, in this terminal, so that a person sees the one-time recovery code (req_001 D-101, D-149): `./local-server_darwin_arm64 serve --data-dir ~/ls-test/data`.
   Pass: it prints a one-time recovery code once. Record any macOS notification or dialog shown, then press Ctrl-C to stop it. The background service refuses to start on a data folder that has not had this first run, so the steps after it depend on it.
7. Start it: `./local-server_darwin_arm64 service start`, then `./local-server_darwin_arm64 service status`.
   Pass: start prints `local-server background service started (user mode).` and status prints `local-server background service, user mode: installed, running`. Record any macOS notification or dialog shown at start.
8. Check that launchd loaded it: `launchctl print gui/$(id -u)/com.github.incoderkaka.local-server`.
   Pass: it prints the job (not `Could not find service`) and shows a `state` of `running` and a `pid`. Record the `last exit code` and any `job state` text if it is not running.
9. Check that the server answers: `./local-server_darwin_arm64 doctor --data-dir ~/ls-test/data` and `curl -s http://127.0.0.1:8485/admin/v1/setup`.
   Pass: the `consumer_port` and `admin_port` checks (or the lines about `127.0.0.1:8484` and `127.0.0.1:8485`) say `cannot listen on ...`, which is the expected sign that the server holds them; and the `curl` answers with JSON such as `{"admin_exists":false}`. Fail: both ports free, or no answer.
10. Log out and back in (or restart the Mac). Do not run anything by hand.
   Pass: after the login, with no prompt and no dialog, `curl -s http://127.0.0.1:8485/admin/v1/setup` answers again, and `launchctl print gui/$(id -u)/com.github.incoderkaka.local-server` shows it running. Record how many seconds after login it answered, and any prompt that appeared.
11. Open System Settings > General > Login Items & Extensions (the name may differ on your version).
    Pass: look for an entry for the binary. Record the exact wording of the item name, of any developer name or "unidentified developer" text, and whether its switch is on. Do not guess: write "no entry" if there is none.
12. If the log file exists, read its last lines: `tail -n 20 ~/Library/Logs/local-server/local-server.log`.
    Pass: it shows the server started after the login in step 10. Record "no log file" if it does not exist.
13. Stop and remove: `./local-server_darwin_arm64 service stop`, then `./local-server_darwin_arm64 service uninstall`.
    Pass: stop prints `local-server background service stopped (user mode).`, uninstall prints `local-server background service removed (user mode).` After them: `ls ~/Library/LaunchAgents/com.github.incoderkaka.local-server.plist` says there is no such file; `launchctl print gui/$(id -u)/com.github.incoderkaka.local-server` says it could not find the service; `curl -s http://127.0.0.1:8485/admin/v1/setup` fails to connect; the Login Items entry is gone after you reopen System Settings. The test data folder (`~/ls-test/data`) stays; that is expected. Remove the test folder, which removes the test data folder too: `rm -rf ~/ls-test`.
14. Contrast, an observation and not a pass condition. This is not the supported way to install: download `local-server_darwin_arm64` from the release page in a browser (this sets `com.apple.quarantine`; `xattr` shows it), `chmod +x` it and run `./local-server_darwin_arm64 --version` from a terminal. Record whether Gatekeeper blocks it, the exact dialog or terminal text, and what the Privacy & Security settings page offers. Then delete the file.

## Result to record

One copy per macOS version. Fill in the macOS version and build (`sw_vers`), and the checked release tag.

| Step | Pass or fail | Exact text seen (dialogs, errors, wording) |
|---|---|---|
| 1 download with curl, no quarantine | | |
| 2 checksum | | |
| 3 `--version` | | |
| 4 service install, and the sudo refusal | | |
| 5 plist exists | | |
| 6 first run, recovery code | | |
| 7 start and status | | |
| 8 `launchctl print` | | |
| 9 doctor and local answer | | |
| 10 starts after login | | |
| 11 Login Items wording | | |
| 12 log file | | |
| 13 stop and uninstall leave nothing | | |
| 14 browser download (observation) | | |

The result goes back to the supervisor session to be written into the decisions log. Nothing is pasted anywhere automatically.
