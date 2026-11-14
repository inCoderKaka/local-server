# M1 second-machine checklist (req_001 D-146, D-148)

Run on a second machine in the same tailnet as the host. Nothing is pasted back: the host reads the result from its own records.

Host name below: the host's `*.ts.net` name from `local-server doctor` (the `tailscale` line).

1. Run `LS_KEY=<access key> ops/checks/consumer-api-check --host <host>.ts.net --port 8484`. It passes when it reports 3 of 3 (streamed chat, non-streamed chat with temperature, embeddings). The first call after a cold start of the host can be slow while the certificate is issued.
2. In a browser open `https://<host>.ts.net:8486/admin/v1/setup`. It answers `{"admin_exists":true}`. This needs the host's `serve.tailnet_admin_enabled` setting on and a restart. There is no admin page yet (M2); this checks the HTTPS listener only.
3. Log in to the remote admin over HTTPS with the host's admin account (`POST /admin/v1/session` with an `Origin` header of `https://<host>.ts.net:8486`). The answer carries a `Secure` `ls_admin_session` cookie.
4. Try `https://<host>.ts.net:8486/metrics`. It answers 404.

The host then confirms, on its own side: `/admin/v1/usage` shows the three calls, and the log has `tailnet request` lines with this machine's node ID and name.
