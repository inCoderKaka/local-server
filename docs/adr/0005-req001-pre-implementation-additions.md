# ADR 0005: req_001 pre-implementation additions

**Status:** Accepted
**Date:** 2026-10-31
**Extends:** [ADR 0002](0002-req001-system-design.md), [ADR 0004](0004-req001-pre-implementation-confirmations.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_001.md`
**Decisions:** D-57..D-63 in [`req_001.decisions.md`](../tracks/platform/phase_1/requirements/req_001.decisions.md). The log is the source.
**executive_inputs_reference:** docs/execetive_inputs/26-10-31_platform_req_001_pre-implementation-additions.md

## Context

The first implementation plan found two dependencies no decision had named: the OpenAPI generator and the log-rotation library. Before the build started, the owner also set new goals for the project: a public repository with strong software docs, Google Cloud only where it genuinely fits, and engineering practice that holds up under expert review. None of these goals changes a decided design. They add dependencies, test rules, release steps and a docs set.

## Decision

- **D-57:** ogen generates the server interfaces from `api/openapi.yaml`, and vacuum lints the spec. Both are pinned as `go tool` directives. A spike first confirms that ogen covers the spec's OpenAPI 3.1 features.
- **D-58:** `natefinch/lumberjack` rotates log files behind the `Telemetry` port. The log-retention settings drive it.
- **D-59:** Google Cloud Storage is a supported backup bucket, through its S3-compatible XML API. The platform-sync adapter is a generic Postgres adapter, with Supabase as the default configuration, so Cloud SQL or AlloyDB is a configuration change. The future central service is described as a GCP proposal and is decided in its own phase.
- **D-60:** every parser of untrusted input has a Go native fuzz target. CI runs the seed corpora on every run and fuzzes each target on a schedule.
- **D-61:** CI runs `go test -race` on Linux and macOS with cgo on. Every build, release and other test job stays `CGO_ENABLED=0`.
- **D-62:** releases publish SLSA build provenance through GitHub artifact attestations, and CI runs `govulncheck`. No binary is altered after the build.
- **D-63:** the whole repository is public, except an owner-only notes file that is gitignored. The software docs are the README, the roadmap, the HLD and LLD under `docs/design_docs/`, a user guide and a developer guide.

## Alternatives considered

- **D-57:** oapi-codegen plus another linter, whose 3.1 support is partial, and hand-written handlers checked by a contract test, which would stop the spec being the source of truth.
- **D-58:** our own rotation code.
- **D-59:** moving platform sync to GCP now. Cloud SQL has no free tier, and the Firestore Go client adds gRPC and binary size. A second option was no GCP at all.
- **D-63:** a private source repo with a generated public mirror, two repositories, one repository with git-crypt, and keeping everything private.

## Consequences

- **Tickets:** PLT-F-001, F-003, F-010, F-011, F-016, F-018, F-044, F-045, F-046 and PLT-C-001, C-003, C-004 carry the new artifacts and verify lines.
- **cgo:** the race job is the only place cgo is used. The release matrix stays pure Go.
- **Fuzzing:** each fuzz target's seed corpus becomes part of the regression suite.
- **GCS:** PLT-F-044's real-bucket test needs credentials in the environment, so it runs behind a build tag, like the Postgres test in PLT-F-046.
- **Docs:** `docs/architecture.md` becomes a pointer, so older links still resolve. The host setup steps move into `docs/user-guide.md`.
- **Visibility:** making the repository public is the owner's action on GitHub.
