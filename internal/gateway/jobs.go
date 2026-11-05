package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-faster/jx"
	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/api"
	"github.com/inCoderKaka/local-server/internal/auth"
	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/jobs"
	"github.com/inCoderKaka/local-server/internal/ledger"
	"github.com/inCoderKaka/local-server/internal/policy"
	"github.com/inCoderKaka/local-server/internal/scheduler"
)

// Jobs is the part of jobs.Service the gateway serves (req_001 D-26, D-39 F2). It is optional in
// Config: without it the four job endpoints answer 501 as an endpoint that is not available yet.
type Jobs interface {
	Create(ctx context.Context, n jobs.NewJob) (jobs.Job, bool, error)
	Get(ctx context.Context, consumerID, id string) (jobs.Job, error)
	Cancel(ctx context.Context, consumerID, id string) (jobs.Job, error)
	Result(ctx context.Context, consumerID, id string) (jobs.Result, error)
	DeleteResult(ctx context.Context, consumerID, id string) error
	FindByIdempotencyKey(ctx context.Context, accessKeyID, key string) (jobs.Job, bool, error)
	QueuedWaiting(ctx context.Context) (int, error)
}

// jobPayload is the request content of a job, as the worker gets it back when it is admitted. It
// is stored sealed by the jobs package and never leaves the host (req_001 D-40). The model is not
// in it: it is job metadata. Properties are the LS-Request-Properties field lines as received.
type jobPayload struct {
	Messages   []jobMessage `json:"messages"`
	MaxTokens  int64        `json:"max_tokens,omitempty"`
	Properties []string     `json:"properties,omitempty"`
}

type jobMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

func newJobPayload(in chatInput, properties []string) ([]byte, error) {
	p := jobPayload{MaxTokens: in.maxTokens, Properties: properties, Messages: make([]jobMessage, 0, len(in.messages))}
	for _, m := range in.messages {
		p.Messages = append(p.Messages, jobMessage{Role: string(m.Role), Content: m.Content})
	}
	return json.Marshal(p)
}

func (p jobPayload) input(model string) chatInput {
	in := chatInput{model: model, maxTokens: p.MaxTokens}
	for _, m := range p.Messages {
		in.messages = append(in.messages, engine.Message{Role: engine.Role(m.Role), Content: m.Content})
		in.promptBytes += int64(len(m.Content))
	}
	return in
}

// CreateJob implements POST /v1/jobs (F2, req_001 D-39): F1 steps 2 to 4, then the job is queued
// and answered 202 with its id. A duplicate Idempotency-Key from the same access key returns the
// existing job with 200 instead (req_001 D-26, D-38). A policy time window that is closed does not
// refuse the job: it is queued, not admissible before the window opens, and decided again at
// admission; every other policy error refuses it (req_001 D-38 kept by D-54).
func (g *Gateway) CreateJob(ctx context.Context, req *api.ChatCompletionRequest, params api.CreateJobParams) (api.CreateJobRes, error) {
	if g.cfg.Jobs == nil {
		return g.UnimplementedHandler.CreateJob(ctx, req, params)
	}
	c, err := g.newCall(ctx)
	if err != nil {
		return nil, err
	}
	if c.st.quotaErr != nil {
		// The quota is used up: only a job this Idempotency-Key already holds is answered.
		j, found, err := g.cfg.Jobs.FindByIdempotencyKey(ctx, c.id.KeyID, params.IdempotencyKey)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, c.st.quotaErr
		}
		out := toAPIJob(j)
		return (*api.CreateJobOK)(&out), nil
	}
	var notBefore time.Time
	windowClosed := false
	dec, err := g.evaluate(ctx, c, req.Model)
	if err != nil {
		var pe *policy.Error
		if !errors.As(err, &pe) || pe.Code != policy.CodeOutsideWindow {
			return nil, err
		}
		windowClosed, notBefore = true, pe.Opens
	}
	in, aerr := validateChat(req)
	if aerr != nil {
		return nil, aerr
	}
	if in.stream {
		return nil, invalidParam("stream", "A job cannot stream: send stream as false or leave it out.")
	}
	if !windowClosed { // with a closed window there are no caps yet; admission checks the size again
		if _, aerr := planChat(c.cfg, dec, c.id.Limits.OnContextOverflow, in); aerr != nil {
			return nil, aerr
		}
	}
	payload, err := newJobPayload(in, c.st.r.Header.Values(policy.HeaderName))
	if err != nil {
		return nil, err
	}
	identity, err := json.Marshal(c.id)
	if err != nil {
		return nil, err
	}
	j, created, err := g.cfg.Jobs.Create(ctx, jobs.NewJob{ConsumerID: c.id.ConsumerID, PrincipalID: c.id.PrincipalID,
		AccessKeyID: c.id.KeyID, GrantVersionID: c.id.GrantVersionID, IdempotencyKey: params.IdempotencyKey, Model: in.model,
		Identity: identity, Payload: payload, NotBefore: notBefore})
	if err != nil {
		return nil, err
	}
	out := toAPIJob(j)
	if created {
		return (*api.CreateJobAccepted)(&out), nil
	}
	return (*api.CreateJobOK)(&out), nil
}

// GetJob implements GET /v1/jobs/{job_id}. A job of another consumer looks like no job.
func (g *Gateway) GetJob(ctx context.Context, params api.GetJobParams) (*api.Job, error) {
	if g.cfg.Jobs == nil {
		return g.UnimplementedHandler.GetJob(ctx, params)
	}
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	j, err := g.cfg.Jobs.Get(ctx, id.ConsumerID, params.JobID.String())
	if err != nil {
		return nil, err
	}
	out := toAPIJob(j)
	return &out, nil
}

// CancelJob implements POST /v1/jobs/{job_id}/cancel. A job already in a terminal state is returned
// unchanged with 409.
func (g *Gateway) CancelJob(ctx context.Context, params api.CancelJobParams) (api.CancelJobRes, error) {
	if g.cfg.Jobs == nil {
		return g.UnimplementedHandler.CancelJob(ctx, params)
	}
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	j, err := g.cfg.Jobs.Cancel(ctx, id.ConsumerID, params.JobID.String())
	out := toAPIJob(j)
	switch {
	case errors.Is(err, jobs.ErrFinished):
		return (*api.CancelJobConflict)(&out), nil
	case err != nil:
		return nil, err
	}
	return (*api.CancelJobOK)(&out), nil
}

// GetJobResult implements GET /v1/jobs/{job_id}/result. It does not delete the result: it asks
// the request to delete it once the response has been written whole to the consumer, so a dropped
// connection leaves the result fetchable until its time-to-live (req_001 D-85, refining D-40).
// Gateway.serve runs the request after the handler returned.
func (g *Gateway) GetJobResult(ctx context.Context, params api.GetJobResultParams) (*api.JobResultHeaders, error) {
	if g.cfg.Jobs == nil {
		return g.UnimplementedHandler.GetJobResult(ctx, params)
	}
	id, err := g.caller(ctx)
	if err != nil {
		return nil, err
	}
	jobID := params.JobID.String()
	res, err := g.cfg.Jobs.Result(ctx, id.ConsumerID, jobID)
	if err != nil {
		return nil, err
	}
	var body api.JobResult
	if err := body.Decode(jx.DecodeBytes(res.Body)); err != nil {
		// The text of a decoding error can quote the stored content: say only that it is unreadable.
		return nil, errors.New("gateway: a stored job result cannot be read")
	}
	if st := stateOf(ctx); st != nil {
		st.afterDelivery = func(ctx context.Context) {
			if err := g.cfg.Jobs.DeleteResult(ctx, id.ConsumerID, jobID); err != nil {
				// The result stays until its time-to-live deletes it.
				g.log.WarnContext(ctx, "gateway: a delivered job result could not be deleted", "job_id", jobID, "err", err)
			}
		}
	}
	return &api.JobResultHeaders{LSReceipt: res.Receipt, Response: body}, nil
}

// toAPIJob maps a job to the Job schema.
func toAPIJob(j jobs.Job) api.Job {
	out := api.Job{Status: api.JobStatus(j.Status), Model: j.Model, IdempotencyKey: j.IdempotencyKey, CreatedAt: j.CreatedAt,
		ResultAvailable: j.ResultAvailable()}
	if id, err := uuid.Parse(j.ID); err == nil {
		out.ID = id
	}
	setTime := func(dst *api.OptNilDateTime, t time.Time) {
		if t.IsZero() {
			dst.SetToNull()
			return
		}
		*dst = api.NewOptNilDateTime(t)
	}
	if j.FailureReason == "" {
		out.FailureReason.SetToNull()
	} else {
		out.FailureReason = api.NewOptNilString(j.FailureReason)
	}
	setTime(&out.StartedAt, j.StartedAt)
	setTime(&out.FinishedAt, j.FinishedAt)
	setTime(&out.DeadlineAt, j.DeadlineAt)
	setTime(&out.ResultExpiresAt, j.ResultExpiresAt)
	return out
}

// ---- the executor the job worker runs with (F1 steps 3 and 5 to 8 for a queued job) ----

// JobExecutor returns the jobs.Executor for the job worker: it decides policy again, admits through
// the scheduler as a job, runs the engine call, meters it and signs the receipt, with the same
// pieces as a synchronous chat completion (req_001 D-39 F2).
func (g *Gateway) JobExecutor() jobs.Executor { return jobExecutor{g} }

type jobExecutor struct{ g *Gateway }

// Admit implements jobs.Executor. The key, the grant version's expiry and the quota are checked
// again, so a key revoked or a grant version expired while the job waited does not run it, then
// policy with the time window (req_001 D-38 kept by D-54), then the size against the caps in force
// now, then Scheduler.Admit. The job runs under the grant version saved at submission, so later
// grant changes (models, quotas, caps) are not applied to it (I-271).
func (x jobExecutor) Admit(ctx context.Context, j jobs.Job, payload []byte) (jobs.Slot, error) {
	g := x.g
	var id auth.Identity
	if err := json.Unmarshal(j.Identity, &id); err != nil {
		return nil, &jobs.FailError{Reason: jobs.ReasonInternal}
	}
	var p jobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return nil, &jobs.FailError{Reason: jobs.ReasonPayloadUnreadable}
	}
	cfg, err := g.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	c := call{id: &id, cfg: cfg, jobID: j.ID}
	if err := g.cfg.Limits.Check(ctx, &id); err != nil {
		return nil, g.jobAdmitError(ctx, err)
	}
	v, err := g.cfg.Grants.Versions.Get(ctx, g.cfg.DB.Reader(), id.GrantVersionID)
	if err != nil {
		return nil, fmt.Errorf("gateway: load grant version: %w", err)
	}
	if v.ExpiresAt != nil && !g.now().Before(*v.ExpiresAt) {
		return nil, g.jobAdmitError(ctx, auth.ErrGrantExpired)
	}
	dec, err := g.evaluateFor(ctx, cfg, &id, p.Properties, j.Model)
	if err != nil {
		return nil, g.jobAdmitError(ctx, err)
	}
	in := p.input(j.Model)
	pl, aerr := planChat(cfg, dec, id.Limits.OnContextOverflow, in)
	if aerr != nil {
		return nil, &jobs.FailError{Reason: jobFailureReason(aerr)}
	}
	tk, _, err := g.admit(ctx, c, j.Model, dec, pl)
	if err != nil {
		return nil, g.jobAdmitError(ctx, err)
	}
	return &jobSlot{g: g, c: c, in: in, p: pl, tk: tk}, nil
}

// jobAdmitError turns an error of the admission steps into what the worker acts on: a closed
// window, a full queue, an empty request allowance, a used-up quota or an engine that is down keep
// the job queued; a refusal that will not pass fails it with the API's own code as the reason; an
// internal failure is returned as it is, for the worker to log and retry (req_001 D-26, D-38).
func (g *Gateway) jobAdmitError(ctx context.Context, err error) error {
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		return err
	}
	e, internal := classify(err, g.now())
	if internal {
		return err
	}
	switch e.code {
	case codeOutsideTimeWindow, codeQueueFull, codeQuotaExceeded, codeRateLimitExceeded, codeHostPaused, codeEngineUnavailable:
		return &jobs.DeferError{After: e.retryAfter, Code: e.code}
	}
	return &jobs.FailError{Reason: jobFailureReason(e)}
}

// jobFailureReason is the failure_reason of a job that failed with e: the error's code, else a
// name for the kind of failure.
func jobFailureReason(e *apiError) string {
	if e.code != "" {
		return e.code
	}
	switch e.status {
	case http.StatusGatewayTimeout:
		return "engine_timeout"
	case http.StatusNotFound:
		return "model_not_found"
	case http.StatusBadRequest:
		return "invalid_request"
	}
	return "engine_error"
}

// jobSlot is an admitted job.
type jobSlot struct {
	g  *Gateway
	c  call
	in chatInput
	p  plan
	tk *scheduler.Ticket
}

// Release implements jobs.Slot.
func (s *jobSlot) Release() { s.tk.Release() }

// Run implements jobs.Slot: F1 steps 6 and 8 for a non-streamed answer. The tokens a failed or
// cancelled run used are metered, as for a synchronous call the client abandoned (req_001 D-38).
func (s *jobSlot) Run(ctx context.Context) (jobs.Output, error) {
	g, c, in := s.g, s.c, s.in
	startedAt := g.now()
	text, last, chunks, err := g.runChat(ctx, c, s.tk.KeepAlive(), in, s.p)
	s.tk.Release()
	if err != nil {
		if ctx.Err() != nil { // cancelled by the consumer, or the host is stopping
			g.meterAborted(ctx, c, ledger.NewUsageID(), ledger.OpChatCompletions, in.model, int64(chunks), startedAt)
			return jobs.Output{}, ctx.Err()
		}
		e, internal := classify(err, g.now())
		var ee *engine.Error
		switch {
		case internal:
			g.log.ErrorContext(ctx, "gateway: job failed", "job_id", c.jobID, "err", err)
		case errors.As(err, &ee):
			g.log.WarnContext(ctx, "gateway: job's engine call failed", "job_id", c.jobID, "status", e.status, "code", string(ee.Code))
		}
		m, merr := g.meterPartial(ctx, c, ledger.NewUsageID(), ledger.OpChatCompletions, in.model, ledger.OutcomeFailed, int64(chunks), startedAt)
		if merr == nil && m.committed {
			g.afterMetered(ctx, c)
		}
		return jobs.Output{}, &jobs.FailError{Reason: jobFailureReason(e)}
	}
	u := ledger.Usage{UsageID: ledger.NewUsageID(), ConsumerID: c.id.ConsumerID, AccessKeyID: c.id.KeyID,
		GrantVersionID: c.id.GrantVersionID, JobID: c.jobID, Operation: ledger.OpChatCompletions, Model: in.model,
		Outcome: ledger.OutcomeCompleted, PromptTokens: int64(last.PromptTokens), CompletionTokens: int64(last.CompletionTokens),
		StartedAt: startedAt, FinishedAt: g.now()}
	m, err := g.meter(ctx, c, u)
	if err != nil {
		return jobs.Output{}, &jobs.FailError{Reason: jobs.ReasonInternal}
	}
	if m.committed {
		g.afterMetered(ctx, c)
	}
	finish := api.FinishReasonStop
	if last.DoneReason == "length" {
		finish = api.FinishReasonLength
	}
	res := api.JobResult{ID: "chatcmpl-" + m.usageID, Object: "chat.completion", Created: u.FinishedAt.Unix(), Model: in.model,
		Choices: []api.ChatCompletionChoice{{Index: 0, FinishReason: finish,
			Message: api.ChatCompletionMessage{Role: api.ChatCompletionMessageRoleAssistant, Content: api.NewNilString(text)}}},
		Usage: api.CompletionUsage{PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens, TotalTokens: u.TotalTokens()}}
	var enc jx.Encoder
	res.Encode(&enc)
	return jobs.Output{Body: enc.Bytes(), Receipt: m.receipt}, nil
}
