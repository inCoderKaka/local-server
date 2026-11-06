# platform / phase_1 — previous session closeout

Session: 2026-11-06 · passive · 2b1bb7a..6e5d49d · tree clean · 123 unpushed commits · migrations: not available (the repo names no migration-status command)

**Open items**
- The Windows sleep-lock adapter (PLT-F-042) was cross-compiled and vetted only. It has never run on Windows.
- CI has never run on GitHub: `-race`, the 3-OS matrix and spec lint are verified locally only.
- `ConsumerRepo.SoftDelete` revokes keys without an audit row or event (the admin delete revokes first, so nothing is lost today). Uncaptured.
- Six findings the code gate deferred are not ticketed, because the session built requirement tickets only: a `doctor` sleep-lock check; the relative invite link while `invites.link_base_url` is empty (found in three gate parts and again by the docs gate); consent `evidence` surviving a consumer delete (I-323); earlier `syn_change` payloads keeping a deleted consumer's name and email until retention (D-40).

**Next steps**
- Build PLT-F-026 (B4 S27, admin API for grants, models and policy rules) in a live session. It needs a migration for versioned policy rules (D-87), so it was left out of the passive session. Settle PLT-B-019 first, then run its code gate.
- Then PLT-F-027 after you approve the PLT-C-002 legal drafts, then PLT-F-028.
- Waiting on you: rulings I-313, I-323, I-344, I-240, I-225, I-203, I-184 (all recommended: accept), PLT-B-027 and PLT-F-052; approval of the legal drafts; whether to ticket the six deferred findings; `docs/ops/plan-window.md` if you want passive budgets enforced. Left out on the passive no-list, each needing a live go: PLT-F-026 (migration), PLT-F-041 and PLT-F-044 (new dependencies), PLT-F-045 (personal-data export and untrusted import), PLT-F-027 (auth and consent surface).

**Where we stand**: phase_1 · batch B4 · slice S27 next: S26 done. Built this session: PLT-F-025 (resumed from the rate-limited session's partial build), PLT-F-042. PLT-F-071 was built the session before. The re-plan found nothing else eligible.

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B4** big tickets | 0 | 0 | 2 | 0 | 2 |
| small tickets | 1 | 0 | 5 | 0 | 4 |
| bugs | 0 | 0 | 0 | 0 | 0 |
| **Phase 1** big tickets | 0 | 0 | 19 | 0 | 19 |
| small tickets | 2 | 0 | 51 | 0 | 49 |
| bugs | 0 | 0 | 29 | 0 | 29 |

Dropped: 0 in both scopes. Ungated: none. PLT-F-042 sits in B8, so it counts in the phase rows only. PLT-F-071 was done at the session base and is not counted. Open now = Open at start + New − Done − Dropped, and it matches the backlog files after the docs fix.

**Bugs seen**: PLT-F-025 left two uncaptured: `ConsumerRepo.SoftDelete` writes no audit row or event (uncaptured, user said requirement tickets only); the generated request decoder dropped unknown JSON fields (fixed by the gate in `734ae20`, closed). Two privacy findings from the gate (consent `evidence` survives delete; earlier `syn_change` payloads keep personal fields) are uncaptured. The comment `usage_record` in `internal/gateway/doc.go` named a retired table (fixed in `0179c82`).

**Verification**: track-work (worklog rows for PLT-F-025, PLT-F-042, PLT-C-013) · sync-docs (`ops/bin/closeout-check` not available; docs reconciled by hand, committed) · ticket-drift not available · ai-mirror --check not available · naming-check not available (manual `rg` found one retired name, fixed) · drift audit ran (7 fixable items: 6 fixed, 1 left as a decided row) · budget (not enforced: `docs/ops/plan-window.md` is missing) · gate (built 2: gated 2, ungated 0, owed: none; code gate PLT-C-013 `734ae20` FIXED, docs gate over `2b1bb7a..0179c82` FIXED in `6e5d49d`) · docs drift (fixed: backlog Done rows and layout, worklog gate row, next-steps strikes, admin module doc, glossary sleep lock row, public roadmap wording; still open: `req_001.decisions.md` D-41..D-50 depends-on path cells name moved files (a decided row, only you revisit it), PLT-F-071 has no `P1/Bx/Sy` plan position)

**Decisions**

Needs ruling (recommended answer):
- I-344 (new) · design · unknown JSON fields on admin bodies and quota schemas answer 400 (D-32 applied by analogy) · accept and add a D-row saying D-32 covers admin bodies
- I-323 (new) · design · consent rows kept untouched on consumer delete · accept; PLT-F-027 decides erasure of `evidence`
- I-313 · design · quota is one nullable object, no client window · accept (the alternative needs a migration)
- I-240 · design · two test-only dependencies promoted to direct in `go.mod` · accept
- I-225 · design · `ntf_event` pruned by `occurred_at` alone · accept (D-83 controls; PLT-F-030 must keep outbox rows from outliving their event)
- I-203 · design · a client disconnect bills the chunks received · accept
- I-184 · design · the request counter is in memory · accept for phase 1

Added this session (new):
- I-345 · design · `InviteIssued.link` typed a URI reference · unreviewed
- I-343 · design · a key PATCH narrowing replaces the stored narrowing whole · unreviewed
- I-342 · copy · Linux inhibitor reason "Sharing is on" · unreviewed
- I-341 · design · `app.power` seam, `wire` takes a `Power` · unreviewed
- I-340 · design · gauge `power_sleep_lock_held` · unreviewed
- I-339 · design · helpers end with local-server (`caffeinate -i -w`, `systemd-inhibit` + `tail --pid`) · unreviewed
- I-338 · design · missing helper tool gives an empty lock, logged once; other errors retried · unreviewed
- I-337 · design · unreadable sharing state leaves the lock as it is · unreviewed
- I-336 · design · `power.check_interval` of 0 or less turns the lock off · unreviewed
- I-335 · design · unparsable stored narrowing shows as none and is logged · unreviewed
- I-334 · design · `/invite#` path is a constant · unreviewed
- I-333 · copy · fixed-text error messages · unreviewed
- I-332 · design · invite status precedence and link format · unreviewed
- I-331 · design · list cursor is base64url of `<created_at ms>:<id>` · unreviewed
- I-330 · design · `admin.list_page_size` (50) is default and cap · unreviewed
- I-329 · scope · a new consumer gets no grant · unreviewed
- I-328 · design · blank names 400, names trimmed, email one address · unreviewed
- I-327 · design · past `expires_at` is 400 · unreviewed
- I-326 · design · narrowing numbers below 1 are 400; empty object stored as NULL · unreviewed
- I-325 · design · narrowing wider than the grant is refused · unreviewed
- I-324 · design · narrowing models given by name, stored by id · unreviewed
- I-322 · design · delete erases name, email, labels, credentials, invite notes; revokes sessions · unreviewed
- I-321 · design · key issue to a suspended consumer is allowed · unreviewed
- I-320 · design · update, suspend and key issue on a deleted consumer answer 409 · unreviewed
- I-319 · design · idempotent no-ops write no audit row · unreviewed
- I-318 · design · audit detail holds only ids, kinds, prefixes, field names · unreviewed
- I-317 · design · consumer audit rows set `target_id` to the principal id · unreviewed
- I-316 · design · audit action names · unreviewed

Older unreviewed, design and scope (newest first, 25 of 296): I-314 `GrantLimits.quota` required and nullable; I-312 job pruning under `jobs.retention`; I-311 grant expiry rechecked at job admission; I-310 jobs queue-depth figure; I-309 admin guard read failure answers 500; I-308 interrupted jobs failed after both listeners bind; I-307 credential package inside auth; I-306 added `x/crypto`, promoted `x/term`; I-305 Argon2 cost settings; I-304 `auth.password.min_length` 12; I-303 login lockout settings; I-302 login success clears only the name counter; I-301 new admin host settings; I-300 admin session row and cookie; I-299 guard refusals use the error envelope; I-298 wrong password and unknown login give identical 401s; I-297 admin audit action names; I-296 a no-op settings PATCH writes nothing; I-295 settings PATCH error codes; I-294 audit redaction of secret-like values; I-293 `settings.Apply` and `Definitions`; I-292 host metrics semantics; I-291 metrics history range rules; I-290 engine state under a probe timeout; I-289 `AdminUser.id`, HEAD on `/metrics`. And 271 more in `implicit-decisions.md`.

Older unreviewed `copy` rows: 10, in `implicit-decisions.md`.
