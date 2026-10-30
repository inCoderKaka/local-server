# ADR 0004: req_001 pre-implementation confirmations

**Status:** Accepted
**Date:** 2026-10-30
**Extends:** [ADR 0002](0002-req001-system-design.md), [ADR 0003](0003-req001-pre-ticketize-verification.md)
**Requirement:** `docs/tracks/platform/phase_1/requirements/req_001.md`
**Decisions:** D-51..D-56 in [`req_001.decisions.md`](../tracks/platform/phase_1/requirements/req_001.decisions.md). The log is the source.
**executive_inputs_reference:** execetive_inputs/26-10-30_platform_req_001_pre-implementation.md, execetive_inputs/26-10-30_platform_req_001_design-sessions-verbatim.md

## Context

Before Phase 1 implementation, two review-gate findings were deferred to the user (receipts), and a check of the user's verbatim answers against the log found four interpretations the user had never confirmed.

## Decision

- **D-51:** if the metering write keeps failing, the response still carries a signed receipt, marked `unmetered`, with token counts and no ledger index.
- **D-52:** a receipt carries its `leaf_index` and the tree size its leaf creates. The covering checkpoint and inclusion proof are fetched later from `/v1/usage/{id}/proof`.
- **D-53:** confirms D-42's direction: each per-type table holds `principal_id` → `idn_principal`.
- **D-54 (supersedes D-38):** everything in D-38 stands except the bounds hierarchy. Each party sets the values it is responsible for freely.
- **D-55:** SLO targets are measured by the gateway ticket, then approved by the user as a decision row before its review gate.
- **D-56:** auto-update is a later phase, not Phase 1.

## Alternatives considered

The user rejected each of these:
- **D-51:** no receipt on that path.
- **D-52:** a checkpoint per request.
- **D-53:** a principal → type-table FK.
- **D-54:** owner + bounds.
- **D-55:** set numbers now, or no SLOs.
- **D-56:** auto-update late in Phase 1.

## Consequences

- **Receipts:** every response has a receipt. A consumer can tell metered from unmetered calls and verify ledger inclusion asynchronously.
- **Settings:** the settings module has no cross-party range checks. A consumer's job-result TTL is not capped by the host, so host storage for job results depends on consumers' choices.
- **SLOs:** PLT-F-018's review gate waits on the user's approval of the measured targets.
