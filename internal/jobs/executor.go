package jobs

import (
	"context"
	"time"
)

// Executor runs a job's request on behalf of the worker. The gateway implements it, so the job
// worker goes through the same steps as an interactive call (req_001 D-39: F1 steps 3 and 5 to 8):
// policy again, admission, the engine call, metering and the signed receipt. It lives behind this
// interface because the gateway imports this package, not the other way round.
type Executor interface {
	// Admit checks the job again and waits for a scheduler slot: its key and quota, the policy
	// decision with the policy's time window, and Scheduler.Admit with scheduler.Request.Job set
	// and the background class. payload is the request content, opened. It returns a Slot, or:
	//   - *DeferError when the job cannot run yet and stays queued (the policy window is closed,
	//     the admission queue is full, the engine is down, the request allowance is empty);
	//   - *FailError when the job can never run (policy denies it, it can never fit the memory
	//     budget, the key was revoked);
	//   - ctx's error when ctx ended (the job was cancelled or expired, or the host is stopping);
	//   - any other error: the worker logs it and tries the job again after jobs.retry_interval.
	Admit(ctx context.Context, j Job, payload []byte) (Slot, error)
}

// Slot is an admitted job. The caller calls Run once, or Release when it does not run.
type Slot interface {
	// Run makes the engine call and meters it with the signed receipt. It returns the result, or:
	//   - *FailError when the engine failed or timed out (the tokens used are metered);
	//   - ctx's error when ctx was cancelled (the tokens so far are metered with outcome aborted).
	// It releases the scheduler slot as soon as the engine call ends.
	Run(ctx context.Context) (Output, error)
	// Release gives the slot back without running. A second call, or a call after Run, does nothing.
	Release()
}

// Output is a successful run.
type Output struct {
	// Body is the answer, stored sealed and handed back by Service.Result. This package does not
	// read it.
	Body []byte
	// Receipt is the signed usage receipt (JWS) that goes in the LS-Receipt header with the result.
	Receipt string
}

// DeferError means the job stays queued and the worker tries it again after After. A zero After
// means the jobs.retry_interval setting.
type DeferError struct {
	After time.Duration
	// Code names why, for the log: a typed code, never content.
	Code string
}

func (e *DeferError) Error() string { return "jobs: not admissible now: " + e.Code }

// FailError ends the job in the failed state with Reason (req_001 D-38): an error code such as
// engine_unavailable or policy_denied, never content.
type FailError struct {
	Reason string
}

func (e *FailError) Error() string { return "jobs: failed: " + e.Reason }
