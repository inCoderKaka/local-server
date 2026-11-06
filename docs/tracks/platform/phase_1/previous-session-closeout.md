# platform / phase_1 — previous session closeout

Session: 2026-11-06 · semi-live · 638f76b..9a6134f · tree clean · 130 unpushed commits · migrations: not available (the repo names no migration-status command; 4 new ones are applied to no real data dir)

**Open items**
- The code gate is owed for the set PLT-F-096 + PLT-F-026 over `a0124e8^..79d0973` (94 files, four migrations, `api/openapi.yaml` and the regenerated `internal/api`). It was deferred for budget. `a0124e8` holds only the counter removal, so read it with `400ee0b`.
- The docs gate is owed over `638f76b..HEAD`, including the `ops/bin` scripts.
- PLT-B-053, PLT-B-054 and PLT-F-097 are specified but not built. PLT-B-055 to B-060 and ideas PLT-F-098 to F-101 are captured.
- Five fixable doc drifts remain: PLT-B-030, B-032 and B-035 status in `bugs.md`; `docs/modules/gateway.md:5`; the depends-on path of D-41 to D-45.
- CI has never run on GitHub; the Windows sleep-lock adapter has never run on Windows.

**Next steps**
- Run the owed code gate, then build PLT-B-053, PLT-B-054, PLT-F-097, PLT-F-028; PLT-F-027 after you approve the PLT-C-002 legal drafts.
- Waiting on you: the legal drafts; rulings on PLT-B-027, PLT-F-052, PLT-B-045; whether to ticket the older gate findings; reviewing I-346 to I-386; when to apply the migrations; PLT-F-041, F-044, F-045, F-070 need a live go (dependency, security, privacy, root daemon).

**Where we stand**: phase_1 · batch B4 · slice S27 done (PLT-F-026), S29 (PLT-F-027) next. Built this session, all ungated: PLT-F-096, PLT-F-026, PLT-B-019, PLT-B-020, PLT-C-014, PLT-C-015.

| | Done: gated | Done: ungated | Open at start | New this session | Open now |
|---|---|---|---|---|---|
| **Batch B4** big tickets | 0 | 1 | 2 | 1 | 2 |
| small tickets | 0 | 1 | 4 | 1 | 4 |
| bugs | 0 | 0 | 0 | 7 | 7 |
| **Phase 1** big tickets | 0 | 1 | 18 | 1 | 18 |
| small tickets | 0 | 3 | 50 | 7 | 54 |
| bugs | 0 | 2 | 29 | 8 | 35 |

Dropped: 0 in both scopes. Ungated: PLT-F-096, F-026, B-019, B-020 (gate owed); PLT-C-014, C-015 are tooling chores. The id → big|small map is now persisted in `ticket-sizes.md`; that is what fixed the 19/49 vs 18/50 mismatch (the old map was never saved; PLT-C-004 is small).

**Bugs seen**: PLT-B-055 (quota window clock vs `started_at`), PLT-B-056 (model caps required in the spec, nullable in storage), PLT-B-057 to B-059 (keep_alive not wired, preload not atomic, allowed-model check before the write), PLT-B-060 (gateway.md says `/v1/capabilities` is planned) — all captured.

**Verification**: track-work (worklog rows for PLT-F-096, F-026, B-019, B-020, C-014, C-015) · sync-docs (`closeout-check --base 638f76b` exit 0, 6 of 6) · ticket-drift exit 0 (40 old warnings) · ai-mirror --check not available · naming-check exit 0 · drift audit: 5 fixable items open (above) · budget (about $24 of the $27.58 ceiling = 95% of the calibrated window, `docs/ops/plan-window.md`: 6,201,248 tokens) · gate (built 6: gated 0, ungated 6, owed: code gate F-096 + F-026; docs gate) · docs drift (fixed: status counts and a retired name | still open: five items above)

**Decisions**

Needs ruling: none.

New this session (all `unreviewed`; the seven earlier rulings are recorded as D-105 to D-108):
- I-346 (new) · design · old `quota` and `quota_requests_per_day` stay as deprecated aliases of list entries (D-107 vs D-105)
- I-347 (new) · design · quota entry wire form is `{metric, limit, window_seconds}`
- I-348 (new) · design · key entries in the narrowing JSON, grant-version entries in `grt_version_quota`
- I-349 (new) · design · migration copies no legacy figures; aliases read with window = `limits.quota_window`
- I-350 (new) · design · `use_request_counter` owned by the usage module
- I-351 (new) · design · tokens summed by `started_at`, one call charged once
- I-352 (new) · design · settings `limits.quota_window_min` 1m, `quota_window_max` 720h, `counter_retention` 168h
- I-353 (new) · design · `CountAdmitted` prunes counters at most once per retention/4
- I-354 (new) · design · `CountAdmitted` reads the key and grant version (two extra reads)
- I-355 (new) · design · `grants.QuotaEntry`; `ValidateQuotas` also called by `auth.ParseNarrowing`, fails closed
- I-356 (new) · design · self-service rule: consumer principal, billing_mode none, any quota entry gets 403
- I-357 (new) · design · narrowing vs grant compared only for identical (metric, window) pairs
- I-358 (new) · design · `limits.StatusOf` returns one `QuotaStatus` per scope and window
- I-359 (new) · copy · entry errors name a dotted param
- I-360 (new) · scope · `warnings` only on `AccessKey` for now (PLT-B-053 builds the general mechanism)
- I-361 (new) · design · `GrantLimits.required` no longer lists `quota`
- I-362 (new) · design · `Consumer.billing_mode` optional in the view schema
- I-363 (new) · design · 403 declared as `components/responses/Forbidden` on two key operations
- I-364 (new) · design · `SetBillingMode` bumps `row_version` and captures a change
- I-365 (new) · design · oversized `window_seconds` clamped then refused 400
- I-366 (new) · copy · windows align from 0001-01-01 00:00 UTC
- I-367 (new) · copy · fixed admin error texts for the deprecated `quota` and the 403
- I-368 (new) · design · policy rules: mutable head row plus immutable `grt_policy_rule_version`, version 1 backfilled
- I-369 (new) · design · `allowed_request_properties` required and nullable (null any, `[]` none)
- I-370 (new) · scope · additive spec: `warnings` on `GrantVersion`, optional rule `name`/`position`/`enabled`, `GET /policy-rules/{id}/versions`
- I-371 (new) · design · policy-rule delete is soft; callers get 404
- I-372 (new) · copy · a rule without a name gets its id as name
- I-373 (new) · design · versions of a deleted rule 404; unknown `grant_id` on rule create 400
- I-374 (new) · design · refused rule document gets fixed text from the policy error code, never echoed
- I-375 (new) · design · rule document stored with sorted keys
- I-376 (new) · design · auto-disable tests a top-level `deny` key by presence; deny rules never auto-disabled; sweeper every 30s
- I-377 (new) · design · no new event type: `policy.rule_invalid` event plus audit row `policy_rule.auto_disabled`
- I-378 (new) · design · settings `policy.rule.max_failures` 5, `policy.rule.check_interval` 30s, `admin.model_default_allowed` false; `internal/admin/grants` imports `internal/scheduler`
- I-379 (new) · design · grant-edit model names resolve via `grt_model` with one engine sync; bad names 400
- I-380 (new) · design · closing a model does not edit existing grant versions
- I-381 (new) · design · `GET /models` with the engine down lists stored models as not loaded
- I-382 (new) · design · `PATCH /models` writes row, audit and preload setting in one `settings.Apply`; comma in a name 400
- I-383 (new) · scope · grant PUT always a new version; consumer missing 404, deleted 409, past `expires_at` 400
- I-384 (new) · design · stored `allowed_properties` that is not a JSON list shows as `[]`
- I-385 (new) · design · grant versions paged by opaque `v<n>` cursor, `admin.list_page_size`
- I-386 (new) · scope · `consumers.QuotaAlias` and `QuotaWarning` exported

Older `unreviewed` design and scope rows, newest first:
- I-345 · design · `InviteIssued.link` typed a URI reference
- I-343 · design · an access-key PATCH narrowing replaces the stored narrowing whole
- I-341 · design · `app.power` is an injection seam, tests never start a real helper
- I-340 · design · gauge `power_sleep_lock_held` emitted through the Telemetry port
- I-339 · design · sleep-lock helpers end with local-server (`caffeinate -i -w`, `systemd-inhibit`)
- I-338 · design · a missing helper tool gives a lock that holds nothing, logged once
- I-337 · design · an unreadable sharing state leaves the sleep lock as it is
- I-336 · design · `power.check_interval` of zero or less turns the sleep lock off
- and 289 more in `implicit-decisions.md`.

Older `unreviewed` copy rows: 12, in `implicit-decisions.md`.
