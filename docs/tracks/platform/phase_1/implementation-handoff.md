# platform / phase_1 — implementation handoff

Only what is open. Rewritten whole at the end of each implementation session.

## Last session

- 2026-11-06, passive, `2b1bb7a..` (see `previous-session-closeout.md` for the end sha).
- Built: PLT-F-025 (admin API for consumers, access keys and invites; resumed from the rate-limited session's partial build) and PLT-F-042 (sleep lock while sharing). PLT-F-071 was built the session before. One code gate over all three ran clean after fixes (PLT-C-013, `734ae20`).
- Migrations: not available (the repo names no migration-status command). Nothing was pushed.

## Leftovers

- The Windows sleep-lock adapter (PLT-F-042) was cross-compiled and vetted only. It has never run on Windows.
- CI has never run on GitHub, so `-race`, the 3-OS matrix and spec lint are verified locally only.
- `ConsumerRepo.SoftDelete` revokes keys with plain SQL and writes no audit row and no event. The admin delete revokes first, so today nothing is lost. Not ticketed.
- The code gate deferred six findings, not ticketed because this session built requirement tickets only:
  - a `doctor` check that says whether this host can take a sleep lock (feature; recommend a warn-level check, never fail);
  - the invite link is relative while `invites.link_base_url` is empty (recommend an admin-UI warning once PLT-F-033 exists; I-345 types the field as a URI reference);
  - `idn_consent.evidence` survives a consumer delete (same question as I-323);
  - earlier `syn_change` payloads keep a deleted consumer's name and email until retention (D-40; recommend nulling the personal fields in earlier rows on delete).
- `docs/ops/plan-window.md` is missing, so a passive `budget <N>%` cannot be enforced.

## Waiting on you

Rulings (all recommended: accept):
- I-313 quota is one nullable object with no client window (the alternative needs a migration).
- I-323 consent rows are kept on consumer delete; PLT-F-027 decides erasure of `evidence`.
- I-344 unknown JSON fields on the admin bodies and quota schemas answer 400. The gate applied D-32 by analogy, so please confirm the analogy or remove `additionalProperties: false`.
- I-240, I-225, I-203, I-184 (older design rows).
- PLT-B-027 (snapshot restore rewinds the ledger; recommend `restore` reports and warns, no new mechanism in phase 1) and PLT-F-052.

Approvals and choices:
- Approve the PLT-C-002 legal drafts. PLT-F-027 stays blocked without it.
- Decide whether to ticket the six gate-deferred findings above.
- Left out of the passive session on the no-list, each needs a live go: PLT-F-026 (needs a migration for versioned policy rules, D-87), PLT-F-041 (adds `kardianos/service`), PLT-F-044 (adds `aws-sdk-go-v2/s3`, data-key custody), PLT-F-045 (exports personal data, imports untrusted files), PLT-F-027 (auth, session and consent surface, `go-webauthn`).
- Provide `docs/ops/plan-window.md` if you want passive budgets enforced.

## Next steps

- Build PLT-F-026 (B4 S27, admin API for grants, models and policy rules) in a live session after settling PLT-B-019. Run its code gate after it.
- Then PLT-F-027 once the legal drafts are approved, and PLT-F-028 after both.
