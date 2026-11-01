package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
	"github.com/inCoderKaka/local-server/internal/storage"
)

func typeName(v any) string { return fmt.Sprintf("%T", v) }

// call is what the two inference handlers share once auth has passed.
type call struct {
	id  *auth.Identity
	st  *reqState
	cfg config
}

func (g *Gateway) newCall(ctx context.Context) (call, error) {
	id, st := identityOf(ctx), stateOf(ctx)
	if id == nil || st == nil {
		return call{}, errors.New("gateway: request reached a handler without authentication")
	}
	cfg, err := g.loadConfig(ctx)
	if err != nil {
		return call{}, fmt.Errorf("gateway: read settings: %w", err)
	}
	return call{id: id, st: st, cfg: cfg}, nil
}

// evaluate is F1 step 3: the policy decision for the request's properties, model and time.
func (g *Gateway) evaluate(ctx context.Context, c call, model string) (policy.Decision, error) {
	models, err := g.cfg.Grants.Models.List(ctx, g.cfg.DB.Reader(), false)
	if err != nil {
		return policy.Decision{}, fmt.Errorf("gateway: read models: %w", err)
	}
	names := make(map[string]string, len(models))
	for _, m := range models {
		names[m.ID] = m.Name
	}
	lim := c.id.Limits
	grant := policy.Grant{ID: c.id.GrantID, AllowedProperties: lim.AllowedProperties, MaxConcurrency: lim.MaxConcurrency}
	for _, m := range lim.Models {
		if name, ok := names[m.ModelID]; ok { // a retired model is no longer granted
			grant.Models = append(grant.Models, policy.GrantModel{Name: name, ContextCap: m.ContextCap, MaxOutputTokens: m.MaxOutputTokens})
		}
	}
	dec, err := g.cfg.Policy.Evaluate(ctx, policy.Input{
		Properties: c.st.r.Header.Values(policy.HeaderName), Model: model, Time: g.now(), Grant: grant})
	if err != nil {
		g.noteRuleFailure(ctx, c.cfg, err)
	}
	return dec, err
}

// admit is F1 step 5. A refusal is a *scheduler.Error. The call is counted once admitted
// (req_001 D-100). It returns how long Admit took. A synchronous call is never a job: whatever
// its priority class it gets host_paused while paused and waits at most the queue-wait limit.
func (g *Gateway) admit(ctx context.Context, c call, model string, dec policy.Decision, p plan) (*scheduler.Ticket, time.Duration, error) {
	class := scheduler.ClassInteractive
	if p.background {
		class = scheduler.ClassBackground
	}
	conc := 0
	if mc := dec.Caps.MaxConcurrency; mc != nil {
		conc = int(min(max(*mc, 0), math.MaxInt32))
	}
	t0 := time.Now()
	tk, err := g.cfg.Scheduler.Admit(ctx, scheduler.Request{GrantID: c.id.GrantID, GrantConcurrency: conc,
		Class: class, Model: model, EstimatedCtxTokens: p.numCtx})
	d := time.Since(t0)
	g.admissionSeconds.Observe(d.Seconds())
	if err != nil {
		return nil, d, err
	}
	if err := g.cfg.Limits.CountAdmitted(ctx, c.id); err != nil {
		g.log.WarnContext(ctx, "gateway: request counter could not be seeded", "err", err)
	}
	return tk, d, nil
}

// metered is the outcome of the metering step.
type metered struct {
	usageID string
	receipt string // the JWS for the LS-Receipt header
	// committed is false when the receipt is the unmetered one.
	committed bool
}

// meter is F1 step 8 and the failure path of step 9 (req_001 D-39, D-51). It writes the
// usage record, the ledger leaf and the change row in one transaction, retrying in memory per
// the settings. If every attempt fails it still returns a receipt: the signed unmetered one,
// after it logged and counted the failure and emitted metering.failed. It ignores the
// client disconnecting, so a call that was served is always recorded. It returns an error only
// when no receipt can be signed, because no answer goes out without a receipt (D-16, D-51 over
// D-39 step 9); the failure is then still logged, counted and emitted as
// metering.failed.
func (g *Gateway) meter(ctx context.Context, c call, u ledger.Usage) (metered, error) {
	mctx := context.WithoutCancel(ctx)
	var last error
	for attempt := int64(1); attempt <= c.cfg.meteringAttempts; attempt++ {
		actx, cancel := context.WithTimeout(mctx, c.cfg.meteringTimeout)
		rec, err := g.cfg.Ledger.Record(actx, u)
		cancel()
		if err == nil {
			return metered{usageID: rec.UsageID, receipt: rec.Receipt.JWS, committed: true}, nil
		}
		last = err
		if attempt < c.cfg.meteringAttempts && c.cfg.meteringRetryDelay > 0 {
			time.Sleep(c.cfg.meteringRetryDelay)
		}
	}
	g.meteringFailures.Inc(u.Operation)
	g.log.ErrorContext(ctx, "gateway: metering failed, answering with an unmetered receipt",
		"usage_id", u.UsageID, "consumer_id", u.ConsumerID, "operation", u.Operation, "attempts", c.cfg.meteringAttempts, "err", last)
	t0 := time.Now()
	signed, err := g.cfg.Ledger.SignUnmetered(u)
	g.observeSigning(time.Since(t0), c.cfg)
	if err != nil {
		g.log.ErrorContext(ctx, "gateway: unmetered receipt could not be signed", "usage_id", u.UsageID, "err", err)
		g.emitMeteringFailed(mctx, c, u)
		return metered{}, fmt.Errorf("gateway: sign unmetered receipt: %w", err)
	}
	g.emitMeteringFailed(mctx, c, u)
	return metered{usageID: u.UsageID, receipt: signed.JWS}, nil
}

// meterAborted meters a call the client abandoned while the engine ran (req_001 D-38, D-39). The
// prompt is not counted: the engine reports its exact count only on the final chunk, and an
// estimate is never billed. Completion tokens are the content chunks received, one token each on
// Ollama's stream. The receipt is dropped, because the client is gone.
func (g *Gateway) meterAborted(ctx context.Context, c call, op, model string, completionTokens int64, startedAt time.Time) {
	u := ledger.Usage{UsageID: ledger.NewUsageID(), ConsumerID: c.id.ConsumerID, AccessKeyID: c.id.KeyID,
		GrantVersionID: c.id.GrantVersionID, Operation: op, Model: model, Outcome: ledger.OutcomeAborted,
		PromptTokens: 0, CompletionTokens: completionTokens, StartedAt: startedAt, FinishedAt: g.now()}
	m, err := g.meter(ctx, c, u)
	if err != nil {
		return // meter already logged, counted and emitted the failure
	}
	if m.committed {
		g.afterMetered(ctx, c)
	}
}

// meteringFailedPayload carries ids and counters only (req_001 D-31); it extends the
// MeteringFailedEvent schema's usage_id.
type meteringFailedPayload struct {
	UsageID     string `json:"usage_id"`
	ConsumerID  string `json:"consumer_id"`
	AccessKeyID string `json:"access_key_id"`
	Operation   string `json:"operation"`
	Attempts    int64  `json:"attempts"`
}

func (g *Gateway) emitMeteringFailed(ctx context.Context, c call, u ledger.Usage) {
	payload, err := json.Marshal(meteringFailedPayload{UsageID: u.UsageID, ConsumerID: u.ConsumerID,
		AccessKeyID: u.AccessKeyID, Operation: u.Operation, Attempts: c.cfg.meteringAttempts})
	if err == nil {
		actx, cancel := context.WithTimeout(ctx, c.cfg.meteringTimeout)
		defer cancel()
		err = g.cfg.Events.Write(actx, g.cfg.DB, func(tx *storage.Tx) error {
			_, err := g.cfg.Events.Emit(tx, events.Event{Type: events.MeteringFailed, Payload: payload})
			return err
		})
	}
	if err != nil {
		g.log.ErrorContext(ctx, "gateway: metering.failed event not emitted", "usage_id", u.UsageID, "err", err)
	}
}

// afterMetered raises quota.near_limit once a record is committed. Its failure never fails the call.
func (g *Gateway) afterMetered(ctx context.Context, c call) {
	if err := g.cfg.Limits.Record(context.WithoutCancel(ctx), c.id); err != nil {
		g.log.WarnContext(ctx, "gateway: near-limit check failed", "err", err)
	}
}

// observeOverhead records the gateway's own time for a call: everything except the engine call
// and the admission, metering write included (req_001 D-31, D-55).
func (g *Gateway) observeOverhead(op string, c call, engineTime, admission time.Duration) {
	d := max(time.Since(c.st.started)-engineTime-admission, 0)
	g.overheadSeconds.Observe(d.Seconds(), op)
	if d > c.cfg.sloOverhead {
		g.sloBreaches.Inc("overhead")
	}
}

func (g *Gateway) observeSigning(d time.Duration, cfg config) {
	g.signingSeconds.Observe(d.Seconds())
	if d > cfg.sloReceiptSigning {
		g.sloBreaches.Inc("receipt_signing")
	}
}
