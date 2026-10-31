// Package events is the events module (docs/modules/events.md): domain events written in the
// caller's unit of work (ntf_event) and handed to subscribers after it commits (D-29, D-39).
//
// This is a shared contract. A module emits with Notifier.Emit(tx, Event) inside the function
// it runs through Notifier.Write(ctx, db, fn), which replaces a bare storage.DB.Write for units
// that emit. The event row commits or rolls back with the unit. After the commit, Write calls
// the matching subscribers in registration order, synchronously on the caller's goroutine
// with a context that ignores the caller's cancellation; a subscriber that fails or panics is
// logged (event id and type, never the payload) and changes nothing else, so subscribers
// should return quickly and hand slow work to their own worker. Concurrent Write calls
// dispatch on their own goroutines, so a handler may run concurrently with itself and must be
// safe for concurrent use. Delivery is at most once:
// an event whose process dies between commit and dispatch keeps its row but is not
// redelivered (the webhook outbox of PLT-F-030 is the at-least-once channel).
//
// Emit through a unit not run by Write fails with ErrOutsideWrite, so the unit rolls back instead
// of silently notifying nobody.
//
// Payloads are JSON objects of ids and counters. They never hold prompts, responses or secrets
// (D-31, D-40); Emit refuses keys that name them, at any depth.
package events
