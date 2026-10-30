# Legal

The repo keeps decision traces from day one, so future legal questions can be answered from the record (req_001 D-45 item 6). Per the user's rule, only the **current phase's** legal needs are discussed and drafted. Future phases' legal needs are discussed when those phases are designed.

## Documents

| Document | Status | Notes |
|---|---|---|
| [Licence](../../LICENSE) | In force | Apache-2.0 (ADR 0001, D-18) |
| [Terms of Use](terms-of-use.md) | Placeholder | To be drafted under PLT-C-002; the user approves it before the consumer-accounts ticket (PLT-F-027) builds. Acceptance is recorded in `idn_consent` |
| [Privacy and consent](privacy-consent.md) | Placeholder | Covers sign-up privacy acceptance and sharing-grant consent. Acceptance is recorded in `idn_consent` |

## Where the decision traces are

- **ADRs:** [`docs/adr/`](../adr/). Each one summarises a stage of a requirement's decisions.
- **Executive inputs:** [`docs/execetive_inputs/`](../execetive_inputs/). These are dated records of the questions asked and the user's answers. The verbatim trace of every question and answer in the req_001 design chats is [`26-10-30_platform_req_001_design-sessions-verbatim.md`](../execetive_inputs/26-10-30_platform_req_001_design-sessions-verbatim.md).
- **Decisions logs:** `docs/tracks/<module>/phase_<N>/requirements/req_<N>.decisions.md`. They are the source of truth for every `D-NN`.

## Decision index (legal-relevant)

| Topic | Decision | Record |
|---|---|---|
| Licence | Apache-2.0 | D-18, [ADR 0001](../adr/0001-req001-scope-and-product-decisions.md) |
| Prompt/response content | Never logged or stored by default. Job content is encrypted and deleted at completion or on fetch | D-31, D-40, [ADR 0002](../adr/0002-req001-system-design.md) |
| Personal data on deletion | Personal fields are erased while the row stays for ledger references | D-40, ADR 0002 |
| Data location | Decentralised by default (host's own storage and bucket). Platform sync is opt-in per host | D-41, ADR 0002 |
| Data sharing | Per-dataset sharing grants with purpose, terms version, expiry, audit and revocation. Phase 1 grantee: the platform admin only | D-43, ADR 0002 |
| Consent records | `idn_consent`: sign-up terms/privacy acceptance and sharing-grant consent in Phase 1. Content capture is off and requires matching consent | D-44, D-45, ADR 0002 |
| Usage integrity | Tamper-evident ledger and signed receipts for future billing disputes | D-16, D-27 |
| Audit | Append-only log of admin actions | D-07 |
