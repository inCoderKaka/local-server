# grants module

## Purpose

Per-consumer grants, the models they allow and policy rules, all versioned or soft-deleted, never edited in place.

## Owns

- Packages: `internal/grants/` (built by PLT-F-007; PLT-F-026 added policy rule versions and the model configuration).
- Tables: `grt_model`, `grt_grant`, `grt_version`, `grt_version_model`, `grt_version_quota`, `grt_policy_rule`, `grt_policy_rule_version`; migrations in `internal/grants/migrations/`. Triggers refuse any update or delete of `grt_version`, `grt_version_model` and `grt_version_quota`, so versions are immutable. Register after identity. `grt_version_quota` (migration 2, req_001 D-107) holds a version's windowed quota entries `{metric, limit, window}`, at most one per metric and window (`grants.QuotaEntry`, `grants.ValidateQuotas`; `Version.Quotas`). The columns `grt_version.quota_tokens_per_day` and `quota_requests_per_day` stay as a deprecated read-only alias: new versions leave them empty and the limiter reads an old figure as one entry with the window `limits.quota_window`.
- Policy rule versions (migration 3, req_001 D-87): `grt_policy_rule_version` holds every version of a rule (`rule_id`, `version_no`, `name`, `position`, `format_version`, `body`, `created_by`, `created_at`) and triggers refuse any update or delete of it. `grt_policy_rule` stays the rule's head: its id, its scope (`grant_id`), the `enabled` switch, `version_no` (the live version) and a copy of the live version's name, position, format version and body, so the gateway reads one row per rule. `PolicyRuleRepo.UpdateBy` stores a new version (number + 1) and copies it to the head in the caller's unit of work when the name, order, format version or body differ; `SetEnabled` changes the head only, so switching a rule on or off makes no version. A rule given no name is named by its id. `Delete` is a soft delete (`deleted_at`): the rule stops applying and is no longer listed by `List`, `ListScoped` or `ListEnabled`, and `Get` still returns it with `DeletedAt` set. `Versions` returns the history newest first. The migration backfills every existing rule as version 1 with an empty `created_by`.
- Model configuration (migration 3): `grt_model.allowed` (1 when new grants may include the model; existing rows are 1, a row the admin API adds starts from the host setting `admin.model_default_allowed`) and `grt_model.keep_alive_seconds` (empty means the host default). `ModelRepo.SetConfig` writes both. Closing a model never edits a grant version that already holds it. The per-model keep-alive is stored and shown, but the scheduler still uses its global `scheduler.keep_alive`; reading it per model is planned.
- Grant versions are listed newest first with `VersionRepo.ListPage`.
- Port: none defined here.

## Imports

Allowed and forbidden imports are enforced by `tools/archcheck` (`go run ./tools/archcheck`): no import cycles between modules, adapters imported only from `cmd/`, drivers only in their owning module. Core modules import ports, never adapters.

## Decisions

D-25, D-44, D-87, D-107. The module map is in `docs/design_docs/lld.md` section 1.
