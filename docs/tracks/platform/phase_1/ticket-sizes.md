# PLT track — ticket sizes (phase_1)

The persisted id → big|small map for every feature and chore ticket in `backlog.md`. Bugs are counted apart and are not in it. Convention: `docs/tracks/README.md` (Per-phase file set). Read by `ops/bin/ticket-drift`.

**Map date:** 2026-11-07
**Covers ids up to:** PLT-F-111, PLT-C-019

**Rule.** *big* = the ticket touches 3 or more files across 2 or more directories, or changes a shared contract (`api/openapi.yaml`, the storage unit of work and migration layout, the event catalog, the module-boundary config). *small* = anything else. Test files and tracking docs do not count as files. Only the orchestrate session edits this file (the plan call adds a row for each new ticket and moves the two header lines above); builders, reviewers and docs sessions do not. A ticket with an id above the header's "Covers ids up to" is reported by `ticket-drift` as a warning (`WARN-unsized`) until the next plan call adds it; an older open ticket without a row is a finding.

**Seeding.** The open rows are the plan call's classification of 2026-11-06 (Effort *Rich* → big, *Quick win* → small). Chores were not in that call, so their rows were classified by the rule and the Why says so. Closed rows were classified by the rule from their build commit.

**Open totals (features and chores, 2026-11-08):** 19 big / 60 small (PLT-F-044 closed big; PLT-F-110 big and PLT-F-111 small added; PLT-F-107..109 small added; PLT-F-041 closed big, PLT-F-097 closed small; the bugs PLT-B-068..073 are not counted).

| ID | Size | Why |
|---|---|---|
| PLT-C-001 | big | Closed. By the rule from build commits 37c2603, d588ee1: 86 non-test file(s) in 52 directories. |
| PLT-C-002 | small | Closed. By the rule from build commit 3cf4f3f: 3 non-test file(s) in 1 directory. |
| PLT-C-003 | big | Classified by the rule (not in the plan call): release workflow, SPA build steps, 3-OS and 2-CPU matrix, checksums and attestation span `.github/workflows/`, `scripts/` and `Makefile`, 3+ files in 2+ directories. |
| PLT-C-004 | small | Classified by the rule (not in the plan call): docs only, `README.md` and the host section of `docs/user-guide.md`, no code and no shared contract. |
| PLT-C-005 | big | Closed. By the rule from build commits 87ab6b9, 5b68fe4, af8e07d, 369482c, 6e8f4eb: 12 non-test file(s) in 5 directories. |
| PLT-C-006 | small | Classified by the rule (not in the plan call): a legal review, no repo files. |
| PLT-C-007 | big | Closed. By the rule from build commits 3056812, f1f417b, 95a2b00, 69f2daa, d4907c7: 21 non-test file(s) in 6 directories. |
| PLT-C-008 | small | Classified by the rule (not in the plan call): one workflow file, `.github/workflows/fuzz.yml`. |
| PLT-C-009 | small | Closed. Classified by the rule: the existing generator is pointed at every module and the output regenerated; built 2026-11-07 (test moved to `cmd/local-server`). |
| PLT-C-010 | big | Closed. By the rule from build commit 4f3c801: 2 non-test file(s) in 2 directories; changes `api/openapi.yaml`. |
| PLT-C-011 | small | Classified by the rule (not in the plan call): runs an existing test against a real Ollama, no code change. |
| PLT-C-012 | big | Closed. By the rule from build commits ebd5245, b1d7f4f: 31 non-test file(s) in 11 directories; changes `api/openapi.yaml`. |
| PLT-C-013 | big | Closed. By the rule from build commits 734ae20, dbf79cf: 14 non-test file(s) in 8 directories; changes `api/openapi.yaml`. |
| PLT-C-014 | small | Closed. Sized small by the session's call (build commit 3879f98): stdlib scripts under `ops/bin/` and `ops/README.md`, no shared contract. |
| PLT-C-015 | small | Closed. Sized small by the session's call (build commit 3879f98): this file, no code and no shared contract. |
| PLT-F-001 | big | Closed. By the rule from build commit f00fa8a: 21 non-test file(s) in 4 directories; changes `api/openapi.yaml`. |
| PLT-F-002 | big | Closed. By the rule from build commit 6839035: 10 non-test file(s) in 4 directories. |
| PLT-F-003 | big | Closed. By the rule from build commit ef86df3: 6 non-test file(s) in 3 directories. |
| PLT-F-004 | big | Closed. By the rule from build commit 5cd11e2: 36 non-test file(s) in 11 directories. |
| PLT-F-005 | big | Closed. By the rule from build commits 939208f, fab7890: 20 non-test file(s) in 12 directories. |
| PLT-F-006 | big | Closed. By the rule from build commit 5cd11e2: 36 non-test file(s) in 11 directories. |
| PLT-F-007 | big | Closed. By the rule from build commit 5cd11e2: 36 non-test file(s) in 11 directories. |
| PLT-F-008 | big | Closed. By the rule from build commits 939208f, fab7890: 20 non-test file(s) in 12 directories. |
| PLT-F-009 | big | Closed. By the rule from build commits 939208f, fab7890: 20 non-test file(s) in 12 directories. |
| PLT-F-010 | big | Closed. By the rule from build commit 8d9cf62: 18 non-test file(s) in 6 directories. |
| PLT-F-011 | big | Closed. By the rule from build commit 8d9cf62: 18 non-test file(s) in 6 directories. |
| PLT-F-012 | big | Closed. By the rule from build commit 8d9cf62: 18 non-test file(s) in 6 directories. |
| PLT-F-013 | big | Closed. By the rule from build commit 9a2d64a: 8 non-test file(s) in 4 directories. |
| PLT-F-014 | small | Closed. By the rule from build commit e1089c9: 6 non-test file(s) in 1 directory. |
| PLT-F-015 | big | Closed. By the rule from build commit 5cf1311: 25 non-test file(s) in 10 directories; changes `api/openapi.yaml`. |
| PLT-F-016 | big | Closed. By the rule from build commits e8378b4, 46e5d77: 23 non-test file(s) in 10 directories; changes `api/openapi.yaml`. |
| PLT-F-017 | big | Closed. By the rule from build commits 957b5af, 46e5d77: 19 non-test file(s) in 9 directories; changes `api/openapi.yaml`. |
| PLT-F-018 | big | Closed. By the rule from build commits ab12bd8, ba604f9, fabd1d0: 28 non-test file(s) in 11 directories; changes `api/openapi.yaml`. |
| PLT-F-019 | big | Closed. By the rule from build commit 289168a: 13 non-test file(s) in 3 directories. |
| PLT-F-020 | big | Closed. By the rule from build commit b4fe621: 13 non-test file(s) in 3 directories. |
| PLT-F-021 | big | Closed. By the rule from build commit c767daa: 10 non-test file(s) in 6 directories; changes `api/openapi.yaml`. |
| PLT-F-022 | big | Closed. By the rule from build commit 88af070: 28 non-test file(s) in 8 directories; changes `api/openapi.yaml`. |
| PLT-F-023 | big | Closed. By the rule from build commit 831b93f: 7 non-test file(s) in 4 directories. |
| PLT-F-024 | big | Closed. By the rule from build commit 455b281: 28 non-test file(s) in 9 directories. |
| PLT-F-025 | big | Closed. By the rule from build commits 1eb7ba8, 4b94c59: 23 non-test file(s) in 6 directories. |
| PLT-F-026 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-027 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `internal/consumerauth/`. |
| PLT-F-028 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/requests/`. |
| PLT-F-029 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-030 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-031 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-032 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `web/admin/`. |
| PLT-F-033 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-034 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-035 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-036 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `web/consumer/`. |
| PLT-F-037 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-038 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-039 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-040 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-041 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-042 | big | Closed. By the rule from build commit a457ece: 13 non-test file(s) in 3 directories. |
| PLT-F-043 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `scripts/install.ps1`, `scripts/install.sh`. |
| PLT-F-044 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-045 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-046 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-047 | big | Seeded from the 2026-11-06 plan call: Effort Rich. |
| PLT-F-048 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-049 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `internal/transport/`. |
| PLT-F-050 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/consumerauth/`. |
| PLT-F-051 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `internal/consumerauth/`. |
| PLT-F-052 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `api/openapi.yaml`. |
| PLT-F-053 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/telemetry/slogprom`. |
| PLT-F-054 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `api/openapi.yaml`, `internal/requests/`. |
| PLT-F-055 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/admin/auth.go`, `internal/audit/`. |
| PLT-F-056 | big | Closed. By the rule from build commit 372ba75: 3 non-test file(s) in 3 directories. |
| PLT-F-057 | small | Closed. By the rule from build commit fa4878c: 2 non-test file(s) in 1 directory. |
| PLT-F-058 | big | Closed. By the rule from build commit 372ba75: 3 non-test file(s) in 3 directories. |
| PLT-F-059 | big | Closed. By the rule from build commit 7bce1b1: 4 non-test file(s) in 3 directories. |
| PLT-F-060 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/identity`. |
| PLT-F-061 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/notifier/events`. |
| PLT-F-062 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/hostkeys`. |
| PLT-F-063 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/audit`. |
| PLT-F-064 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/policy`. |
| PLT-F-065 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `api/openapi.yaml`, `internal/policy/rule_v1.schema.json`. |
| PLT-F-066 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/policy`. |
| PLT-F-067 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. |
| PLT-F-068 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/notifier/events`. |
| PLT-F-069 | big | Closed. By the rule from build commit 372ba75: 3 non-test file(s) in 3 directories. |
| PLT-F-070 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `cmd/local-server`, `internal/hostmetrics`. |
| PLT-F-071 | big | Closed. By the rule from build commit d03d916: 4 non-test file(s) in 2 directories; changes `api/openapi.yaml`. |
| PLT-F-072 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/audit/`, `internal/limits/lockout.go`. |
| PLT-F-073 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/gateway/gateway.go`, `internal/limits/limiter.go`. |
| PLT-F-074 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/gateway/gateway.go`. |
| PLT-F-075 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/scheduler/scheduler.go`. |
| PLT-F-076 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `cmd/local-server/main.go`, `cmd/local-server/serve.go`. |
| PLT-F-077 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/gateway/stream`. |
| PLT-F-078 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/engine`, `internal/gateway/embeddings.go`. |
| PLT-F-079 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `cmd/local-server/doctor.go`. |
| PLT-F-080 | big | Seeded from the 2026-11-06 plan call: Effort Rich. Artifact cites `internal/offhost/snapshot/`. |
| PLT-F-081 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/gateway/status.go`. |
| PLT-F-082 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/limits/`. |
| PLT-F-083 | small | Closed. No build commit in the worklog that git can show; classified by the rule from the Artifact column (1 cited path(s)). |
| PLT-F-084 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/gateway/jobs.go`. |
| PLT-F-085 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/jobs/settings.go`. |
| PLT-F-086 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/events/`, `internal/jobs`. |
| PLT-F-087 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/settings`. |
| PLT-F-088 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/usage/migrations/`. |
| PLT-F-089 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/admin/`. |
| PLT-F-090 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/auth/credential/session.go`. |
| PLT-F-091 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/auth/credential/password.go`. |
| PLT-F-092 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `internal/auth/credential/guard.go`. |
| PLT-F-093 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `web/admin/`. |
| PLT-F-094 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `api/openapi.yaml`, `internal/jobs`. |
| PLT-F-095 | small | Seeded from the 2026-11-06 plan call: Effort Quick win. Artifact cites `cmd/local-server/recovery.go`. |
| PLT-F-096 | big | Windowed quota list, billing mode, persisted counters: migration, openapi change, regenerated `internal/api`, limits and usage modules (D-107). |
| PLT-F-097 | small | Purge of soft-deleted consumers after a retention setting (D-106); one module. |
| PLT-F-098 | small | Idea captured 2026-11-06 (P3); one module and one endpoint, no shared contract expected. |
| PLT-F-099 | small | Idea captured 2026-11-06 (P3); one module and one endpoint, no shared contract expected. |
| PLT-F-100 | small | Idea captured 2026-11-06 (P3); one module and one endpoint, no shared contract expected. |
| PLT-F-101 | small | Idea captured 2026-11-06 (P3); one module and one endpoint, no shared contract expected. |
| PLT-F-102 | big | Idea captured 2026-11-07 (P3); a migration and the limiter, usage and gateway modules. |
| PLT-C-016 | small | Chore captured 2026-11-07 (P3); one field removed and three test sites edited. |
| PLT-C-017 | small | Chore captured 2026-11-07 (P3); one docs line. |
| PLT-F-103 | small | Idea captured 2026-11-07 (P3); one test file. |
| PLT-F-104 | small | Idea captured 2026-11-07 (P3); one test file. |
| PLT-C-018 | small | Chore logged 2026-11-07; docs only. |
| PLT-C-019 | big | Chore logged 2026-11-07; 85 code files reviewed and fixed across modules. |
| PLT-F-105 | small | Idea captured 2026-11-07 (P3); two call sites in the gateway. |
| PLT-F-106 | big | Idea captured 2026-11-07 (P3); a consumer route, so `api/openapi.yaml` and `internal/api` change. |
| PLT-F-107 | small | Idea captured 2026-11-08 (P3); one install path in the service CLI. |
| PLT-F-108 | small | Idea captured 2026-11-08 (P3); one settings field in the admin UI. |
| PLT-F-109 | small | Idea captured 2026-11-08 (P3); one field in the invite view. |
| PLT-F-110 | big | Idea captured 2026-11-08 (P2); key custody across `hostkeys`, restore and the snapshot module. |
| PLT-F-111 | small | Idea captured 2026-11-08 (P3); one display in `doctor` and the admin status screen. |
