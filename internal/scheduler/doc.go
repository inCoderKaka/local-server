// Package scheduler is part of the scheduler module; see docs/modules/scheduler.md.
//
// It is the in-process admission scheduler and the model lifecycle manager
// (req_001 D-26). The gateway (F1 step 5, req_001 D-39) and the job worker call
// Scheduler.Admit before every engine call and Ticket.Release after it.
//
// Admission:
//   - Two classes. Interactive goes ahead of background; a waiting background
//     request ages (BackgroundAging) so steady interactive load never starves it.
//   - Concurrency is capped per grant and in total. The total never exceeds the
//     host's OLLAMA_NUM_PARALLEL (the KeyOllamaNumParallel setting), so Ollama's own
//     queue never fills.
//   - Memory admission: the model's size (when it is not loaded yet) plus the
//     estimated context memory of this and every running request must fit the memory
//     budget. Idle models are unloaded to make room (reclaim); a request that can
//     never fit beside the preloaded models fails with ErrOverBudget.
//   - The queue is bounded. A full queue, or a wait past the queue-wait limit, fails
//     fast with ErrQueueFull carrying a Retry-After. An interactive request finding the
//     queue full takes the place of the newest waiting background request, which then
//     gets ErrQueueFull. While the host is paused or in quiet hours, interactive
//     requests fail with ErrHostPaused and background ones stay queued.
//   - A running request is never interrupted: shrinking a limit only delays new
//     admissions.
//
// Model lifecycle (Scheduler.Apply, Sweep and Run): preload at start and when the
// preload setting changes, a keep-alive policy, and idle unload.
//
// Every time, count and size is a host-owned runtime setting read at the moment of use
// (req_001 D-38, D-54); see Defs. The scheduler sees only sizes and ids, never prompt
// or response content (req_001 D-31, D-40).
package scheduler
