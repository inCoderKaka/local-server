# M1 second-machine checklist (req_001 D-146, D-148)

Run on a second machine in the same tailnet as the host. Nothing is pasted back: the host reads the result from its own records.

Host name below: the host's `*.ts.net` name from `local-server doctor` (the `tailscale` line).

## Before you start

On the host:

- The standalone Tailscale app is logged in, and the tailnet's admin console has MagicDNS and HTTPS Certificates turned on (DNS page).
- local-server runs from a binary built with `make` (a plain `go build` serves stub pages instead of the admin and consumer UIs), against Ollama with `llama3.2:3b` and `nomic-embed-text` pulled.
- `serve.tailnet_admin_enabled` is on, and local-server was restarted after turning it on.
- In the admin UI: a consumer, a grant for both models, and an access key for that consumer. Keep the key secret for step 1.

On the second machine:

- Tailscale is installed and logged in to the same tailnet. `tailscale ping <host>` answers.
- **Windows:** run step 1 in **Git Bash** (Git for Windows), which has `bash` and `curl`. WSL also works only if it resolves `*.ts.net` names. Copy `ops/checks/consumer-api-check` across or clone the repo.

## Checks

1. Run `LS_KEY=<access key> ops/checks/consumer-api-check --host <host>.ts.net --port 8484`. It passes when it reports 3 of 3 (streamed chat, non-streamed chat with temperature, embeddings). The first call after a cold start of the host can be slow while the certificate is issued.
2. In a browser, open `https://<host>.ts.net:8486/admin/v1/setup`. It answers `{"admin_exists":true}`.
3. In the same browser, open `https://<host>.ts.net:8486/` and log in to the admin UI with the host's admin account. In the browser's developer tools (Application, Cookies) the `ls_admin_session` cookie is marked `Secure`.
4. Open `https://<host>.ts.net:8486/metrics`. It answers 404.

The host then confirms, on its own side: `/admin/v1/usage` shows the three calls, and the log has `tailnet request` lines with this machine's node ID and name.
