package events

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Type names a kind of domain event, "<subject>.<what happened>". The set is closed: Emit
// refuses a type that is not in Types. A later ticket adds its constant here and to Types.
type Type string

// Event types (D-29).
const (
	HostPaused             Type = "host.paused"
	QuotaNearLimit         Type = "quota.near_limit"
	ResourceRequestCreated Type = "resource_request.created"
	ResourceRequestDecided Type = "resource_request.decided"
	JobDone                Type = "job.done"
	MeteringFailed         Type = "metering.failed"
	PlatformSyncFailed     Type = "platform_sync.failed"
)

// Types lists every defined event type.
func Types() []Type {
	return []Type{HostPaused, QuotaNearLimit, ResourceRequestCreated, ResourceRequestDecided, JobDone, MeteringFailed, PlatformSyncFailed}
}

// Known reports whether t is a defined event type.
func (t Type) Known() bool {
	for _, k := range Types() {
		if k == t {
			return true
		}
	}
	return false
}

// Event is one domain event. Emit fills ID and OccurredAt. Payload is a JSON object of ids and
// counters; it must never hold a prompt, a response or a secret (D-31, D-40).
type Event struct {
	ID                string
	Type              Type
	OccurredAt        time.Time
	ActorPrincipalID  string // empty when the system caused the event
	TargetPrincipalID string // empty when the event is not about one principal
	Payload           json.RawMessage
}

var errPayload = errors.New("events: invalid payload")

// normalizePayload returns the payload to store: "{}" when empty, otherwise a JSON object
// without keys that name content or secrets.
func normalizePayload(p json.RawMessage) (json.RawMessage, error) {
	if len(p) == 0 {
		return json.RawMessage("{}"), nil
	}
	var obj map[string]any
	if err := json.Unmarshal(p, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("%w: must be a JSON object", errPayload)
	}
	// A guard against a slip, not a scanner: the emitting module owns what it puts in a payload.
	// The check covers every depth.
	if k, found := telemetry.FindContentKey(obj); found {
		return nil, fmt.Errorf("%w: key %q may name content or a secret (D-31, D-40)", errPayload, k)
	}
	// Copy, so a caller that reuses its buffer cannot change a pending event before dispatch.
	return append(json.RawMessage(nil), p...), nil
}
