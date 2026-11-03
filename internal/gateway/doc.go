// Package gateway is the gateway module; see docs/modules/gateway.md. It serves the consumer
// API (req_001 D-10, D-32, D-33): the OpenAI-compatible POST /v1/chat/completions (streamed with
// stream: true), POST /v1/embeddings and GET /v1/models, the host status endpoints GET /v1/health
// and GET /v1/capabilities (D-05), and the ledger endpoints GET /v1/keys, GET
// /v1/ledger/checkpoint and GET /v1/usage/{usage_id}/proof. Every other /v1 operation answers 501
// until its ticket lands.
//
// # Pipeline
//
// F1 (req_001 D-39), in this order, for chat and embeddings:
//
//   - step 2, auth: limits.CheckLockout, auth.Authenticate, limits.AuthResult and
//     limits.Check run in the ogen security handler, so before the body is read. The gateway
//     applies limits.LimitBody to every /v1 request before routing, so routes that need no key
//     are capped too;
//   - step 3, policy: Policy.Evaluate with the request properties, model, time and grant (req_001 D-79);
//   - step 4, validate: fields the engine port cannot carry are refused (never silently ignored,
//     D-32), max_tokens is capped, the prompt size is pre-checked against the context cap
//     (context_too_long unless the grant's on_context_overflow is truncate, D-48);
//   - step 5, admit: scheduler.Admit with the grant's concurrency limit, then limits.CountAdmitted (D-100);
//   - step 6, the engine call;
//   - step 7, SSE, for stream: true (package stream, PLT-F-019): chunks flushed as they arrive, the
//     receipt as the comment line ": ls-receipt <JWS>" before data: [DONE]. The first-token, idle
//     and total timeouts (gateway.stream_*) cancel the engine call; a client that disconnects
//     cancels it too and its tokens so far are metered as aborted;
//   - steps 8 and 9, metering: one metering transaction (ledger.Record: usage_record, ledger leaf,
//     syn_change) and the signed receipt in the LS-Receipt header. If metering keeps failing the
//     answer is still returned with the signed unmetered receipt, the failure is logged and
//     counted, and a metering.failed event is emitted (D-39 step 9, D-51). A call the client
//     abandons while the engine runs is metered as aborted with the tokens received so far.
//
// # Jobs
//
// POST /v1/jobs (req_001 D-39 F2) runs F1 steps 2 to 4 and queues the request through
// internal/jobs; the job worker then runs it through JobExecutor: policy again (a closed time
// window keeps the job queued), the key and quota again, Scheduler.Admit as a background job, the
// engine call, metering under the job id and the signed receipt. A job's result is deleted only
// once GET /v1/jobs/{job_id}/result has written its body whole to the connection (req_001 D-85):
// the handler registers the deletion, and serve runs it after the response was written and
// flushed without error.
//
// # Errors
//
// Every error uses the OpenAI error envelope (api/openapi.yaml). Request decoding errors,
// unknown paths, wrong methods and panics are mapped to it too, and none of them quotes a byte
// of the request except a field name that passed a strict character check (PLT-B-003). The
// handler serves /v1 only: an /admin/v1 path answers 404 in the envelope (PLT-B-005).
//
// # Policy rule failures
//
// A rule that fails to parse fails the request closed. The gateway logs it, counts it
// (RuleFailures) and emits policy.rule_invalid, at most once per rule per
// gateway.rule_invalid_event_interval (req_001 D-73). Disabling a broken rule in storage is
// not done here.
//
// # Settings
//
// Every limit, time and threshold is a host-owned runtime setting (Defs, req_001 D-38). The
// three service-level targets of req_001 D-31, D-55 are settings too: the SLO tests read them,
// and the gateway counts each call whose own time misses the overhead target.
//
// # Content
//
// Prompts and responses live in request-scoped memory only. Nothing here logs or stores them;
// error text is fixed and never carries request bytes (req_001 D-31, D-40).
package gateway
