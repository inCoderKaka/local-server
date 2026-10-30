> **Provenance note.** Verbatim from the second design chat on 2026-10-30, after ticketize. It covers the review gate's two deferred findings and the check of the user's answers against the log. Recorded as D-51..D-56.

agent questions:
1. "If writing a usage record keeps failing, the answer is still returned (D-39). But D-16 says every response carries a signed receipt, and there's no ledger entry to point to. What should that response carry?"
   - Signed 'unmetered' receipt (Recommended): A signed receipt marked `unmetered`, with the token counts and no ledger index. Keeps 'a receipt in every response', and gives the consumer signed proof the host can't later bill that call.
   - No receipt header: Leave out LS-Receipt on that path. The spec says a missing receipt means the call was not metered.
2. "Receipts are signed when a request ends, but ledger checkpoints are made later, every N entries or T seconds. What should a receipt carry about the checkpoint?"
   - Leaf index + tree size (Recommended): The receipt carries its leaf index and the tree size that leaf creates. The consumer fetches the first covering checkpoint and the inclusion proof later from /v1/usage/{id}/proof. No added latency.
   - Checkpoint per request: Write a signed checkpoint at the end of every request so the receipt can name it. Simpler to verify, but adds signing work and latency to every call.
3. "D-42: you said "keep unified princepal as well which would foreign key refer to the per type tables". The log reads it the other way round: each per-type table (idn_admin_user, idn_consumer, plt_admin) holds a principal_id FK pointing to idn_principal. Which direction?"
   - Type tables → principal (Recommended): As logged. Each type row has principal_id (UNIQUE, FK). Adding a new user type never changes idn_principal; shared properties live only on the principal. Standard pattern, cheapest to extend.
   - Principal → type tables: Literal reading: idn_principal holds a FK to its type row. Needs a nullable column per type (or a polymorphic id), and adding a type alters the principal table.
4. "D-38: you said values like "after 7 days" must be configurable by "the respective required party be it admin/host/consumer". The log adds a hierarchy: each value has ONE owning party, and a lower party may only tighten it within the higher party's bounds (e.g. a consumer can pick a job-result TTL only inside the host's min/max). Is that right?"
   - Yes, owner + bounds (Recommended): Platform admin sets platform minimums, host sets host values and the range consumers may choose, consumer sets own preferences within that range.
   - No bounds: Each party sets its own values freely; no hierarchy.
5. "D-31 says SLO targets (gateway overhead, admission latency, receipt-signing time) are 'set in LLD', but none were set. How should they be set?"
   - Measure, then you approve (Recommended): The gateway ticket (PLT-F-018) benchmarks on the fake engine and proposes targets; you approve them and they're recorded as a decision row before the gateway is gated.
   - Set numbers now: I propose concrete targets in this chat (e.g. gateway overhead p95 under 5 ms) and record them before building.
   - No SLOs in Phase 1: Record that Phase 1 has no SLO targets; only metrics are exported.
6. "D-04: you said "auto-update feature later". The log reads 'later' as a later phase, not Phase 1 — your 'Both correct' confirmed only the table prefixes and the notification channels. Is auto-update out of Phase 1?"
   - Later phase (Recommended): Not built in Phase 1; the install script's upgrade command covers manual upgrades.
   - Late in Phase 1: Add an auto-update ticket at the end of Phase 1 (after the installers).

user response:
1. "Signed 'unmetered' receipt (Recommended)"
2. "Leaf index + tree size (Recommended)"
3. "Type tables → principal (Recommended)"
4. "No bounds"
5. "Measure, then you approve (Recommended)"
6. "Later phase (Recommended)"
