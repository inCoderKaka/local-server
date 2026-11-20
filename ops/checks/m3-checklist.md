# M3 second-machine browser checklist (req_001 D-146, finish-line statement 4)

Run in a browser on a second machine in the same tailnet as the host, after the M1 checklist. Nothing is pasted back: the host reads the result from its own records (consent, session, resource-request and audit rows, and the calling node in the log).

Host name below: the host's `*.ts.net` name from `local-server doctor`.

## Before you start (on the host)

- Set the host setting `invites.link_base_url` to `https://<host>.ts.net:8485`, so invite links are whole addresses. Otherwise put that address in front of the relative link by hand.
- In the admin UI, open Invites and create an invite. Copy its link; it is shown only once.

## Checks (on the second machine)

1. Open the invite link. The sign-up page shows the Terms of Use and the Privacy notice. Accept both and create a login with a password.
2. The consumer page says there is no access yet (no grant, D-150) and offers a resource request.
3. Log out, then log in again with the same password.
4. Optional: add a passkey in Settings (Windows Hello works), log out and log in with the passkey.
5. File a resource request for `llama3.2:3b` with a short reason, then add a comment to it.
6. On the host, open Requests in the admin UI. The pending count shows the request. Approve it, setting the limits.
7. Back on the second machine, reload the consumer page. The request shows as approved, and My limits shows the new grant.
8. Create an access key on the consumer page. Its secret is shown once.
9. Optional: run `LS_KEY=<that key> ops/checks/consumer-api-check --host <host>.ts.net --port 8484` (Git Bash on Windows). My usage then shows the calls after the next rollup.
10. File a second request and have the host reject it. The consumer page shows it as rejected.

The host then confirms, on its own side: the consumer's consent rows, a consumer session from this machine's tailnet node, both resource requests with the comment and the approve and reject in the audit log.
