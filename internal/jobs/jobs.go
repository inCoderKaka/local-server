package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/inCoderKaka/local-server/internal/hostkeys"
	"github.com/inCoderKaka/local-server/internal/notifier/events"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/storage"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Status is a job's state (req_001 D-38, D-54).
type Status string

// The states. Succeeded, Failed, Cancelled and Expired are terminal and never reopen.
const (
	Queued    Status = "queued"
	Running   Status = "running"
	Succeeded Status = "succeeded"
	Failed    Status = "failed"
	Cancelled Status = "cancelled"
	Expired   Status = "expired"
)

// Terminal reports whether s is a state a job never leaves.
func (s Status) Terminal() bool { return s != Queued && s != Running }

// Failure reasons that release the job's Idempotency-Key (req_001 D-38, D-54, D-68).
const (
	// ReasonHostRestart is the reason of a job that was running when the host stopped.
	ReasonHostRestart = "host_restart"
	// ReasonPayloadMinimized is the reason restore gives a job whose payload a backup left out.
	ReasonPayloadMinimized = "payload_minimized"
)

// Other failure reasons the worker itself sets, besides the error codes the Executor reports.
const (
	ReasonPayloadUnreadable = "payload_unreadable"
	ReasonInternal          = "internal_error"
)

// Errors returned by Service. None carries content.
var (
	// ErrNotFound means no job of the caller has this id. A job of another consumer is not found.
	ErrNotFound = errors.New("jobs: no such job")
	// ErrFinished means the job is already in a terminal state, so it cannot be cancelled.
	ErrFinished = errors.New("jobs: the job is already finished")
	// ErrNotSucceeded means the job has no result because it did not succeed (yet).
	ErrNotSucceeded = errors.New("jobs: the job has not succeeded")
	// ErrResultGone means the job's result was fetched whole, or its time-to-live passed.
	ErrResultGone = errors.New("jobs: the job's result is gone")
)

// Job is a job's metadata. It holds no content (req_001 D-31, D-40).
type Job struct {
	ID             string
	ConsumerID     string
	PrincipalID    string
	AccessKeyID    string
	GrantVersionID string
	IdempotencyKey string
	Model          string
	// Identity is the opaque snapshot NewJob carried, given back to the Executor.
	Identity        []byte
	Status          Status
	FailureReason   string
	CreatedAt       time.Time
	NotBefore       time.Time // zero: admissible at once
	DeadlineAt      time.Time // zero: never expires
	StartedAt       time.Time
	FinishedAt      time.Time
	ResultExpiresAt time.Time // zero unless a result can still be fetched
}

// ResultAvailable reports whether a result can still be fetched.
func (j Job) ResultAvailable() bool { return !j.ResultExpiresAt.IsZero() }

// NewJob is what Create needs. Payload and Identity are opaque to this package: the gateway
// encodes them. Payload is the request content and is sealed before it is stored.
type NewJob struct {
	ConsumerID     string
	PrincipalID    string
	AccessKeyID    string
	GrantVersionID string
	IdempotencyKey string
	Model          string
	// Identity is a JSON copy of the submitting key's identity and limits: ids, numbers, the key prefix and
	// the grant's policy fields (allowed properties, context-overflow rule). Never a secret or request content.
	Identity []byte
	// Payload is the request content.
	Payload []byte
	// NotBefore, when set, keeps the job from the scheduler until then (a closed policy window).
	NotBefore time.Time
}

// Result is a succeeded job's answer.
type Result struct {
	// Body is the content the Executor returned in Output.Body.
	Body    []byte
	Receipt string
	// ExpiresAt is when the result is deleted if it is not fetched whole before.
	ExpiresAt time.Time
}

// DataKeys supplies the host data key. *hostkeys.Keys satisfies it.
type DataKeys interface {
	DataKey() []byte
}

// Config is what New needs. All fields except Telemetry and Clock are required.
type Config struct {
	DB       *storage.DB
	Settings *settings.Store
	Events   events.Notifier
	Keys     DataKeys
	// Telemetry nil means telemetry.Nop().
	Telemetry telemetry.Telemetry
	// Clock nil means time.Now.
	Clock func() time.Time
}

// Service stores jobs and runs the worker. It is safe for concurrent use.
type Service struct {
	cfg Config
	now func() time.Time
	log *slog.Logger

	finished telemetry.Counter // by status

	wake chan struct{}
	wg   sync.WaitGroup

	mu     sync.Mutex
	active map[string]context.CancelCauseFunc // jobs handed to the scheduler, or running
	hold   map[string]time.Time               // jobs whose retry time could not be stored; not offered to the scheduler again before it
}

// New registers the job settings and returns the Service.
func New(cfg Config) (*Service, error) {
	if cfg.DB == nil || cfg.Settings == nil || cfg.Events == nil || cfg.Keys == nil {
		return nil, errors.New("jobs: DB, Settings, Events and Keys are required")
	}
	if cfg.Telemetry == nil {
		cfg.Telemetry = telemetry.Nop()
	}
	if cfg.Clock == nil {
		cfg.Clock = time.Now
	}
	if err := cfg.Settings.Register(Defs()...); err != nil {
		return nil, err
	}
	if err := cfg.Settings.RegisterRetention(settings.Rule{Table: "sch_job", TimeColumn: "finished_at", Setting: KeyRetention,
		Where: "status IN ('succeeded', 'failed', 'cancelled', 'expired') AND NOT EXISTS (SELECT 1 FROM sch_job_result r WHERE r.job_id = sch_job.id)"}); err != nil {
		return nil, err
	}
	if err := setValidators(cfg.Settings); err != nil {
		return nil, err
	}
	return &Service{cfg: cfg, now: cfg.Clock, log: cfg.Telemetry.Logger(),
		finished: cfg.Telemetry.Counter("jobs_finished_total", "status"),
		wake:     make(chan struct{}, 1), active: map[string]context.CancelCauseFunc{}, hold: map[string]time.Time{}}, nil
}

func ms(t time.Time) int64 { return t.UnixMilli() }

func fromMS(v int64) time.Time { return time.UnixMilli(v).UTC() }

func optMS(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return ms(t)
}

// nullTime turns a nullable millisecond column into a time; nil is the zero time.
func nullTime(v *int64) time.Time {
	if v == nil {
		return time.Time{}
	}
	return fromMS(*v)
}

// aad binds a sealed blob to its table, job and consumer, so a blob copied to another row or
// another consumer does not open.
func aad(table, jobID, consumerID string) []byte {
	return []byte(table + ":" + jobID + ":" + consumerID)
}

const (
	tablePayload = "sch_job_payload"
	tableResult  = "sch_job_result"
)

func (s *Service) seal(table, jobID, consumerID string, plain []byte) ([]byte, error) {
	return hostkeys.Seal(s.cfg.Keys.DataKey(), plain, aad(table, jobID, consumerID))
}

func (s *Service) unseal(table, jobID, consumerID string, sealed []byte) ([]byte, error) {
	return hostkeys.Unseal(s.cfg.Keys.DataKey(), sealed, aad(table, jobID, consumerID))
}

// notReusable is the condition of the idempotency index: a job in these states holds its key.
// A job that failed with host_restart or payload_minimized does not. Keep it equal to the
// partial index in the migration so the planner can use the index.
const holdsKey = `NOT (j.status = 'failed' AND j.failure_reason IN ('host_restart', 'payload_minimized'))`

const jobColumns = `j.id, j.consumer_id, j.principal_id, j.access_key_id, j.grant_version_id, j.idempotency_key, j.model,
	j.identity, j.status, j.failure_reason, j.created_at, j.not_before, j.deadline_at, j.started_at, j.finished_at, r.expires_at`

// jobSelect reads jobs with the expiry of a result that can still be fetched. The first argument
// of the query is the current time.
func jobSelect(where string) string {
	return `SELECT ` + jobColumns + ` FROM sch_job j
		LEFT JOIN sch_job_result r ON r.job_id = j.id AND r.expires_at > ? WHERE ` + where
}

type scanner interface{ Scan(dest ...any) error }

func scanJob(r scanner) (Job, error) {
	var j Job
	var status string
	var identity string
	var reason *string
	var notBefore, deadline, started, finished, resExp *int64
	var created int64
	if err := r.Scan(&j.ID, &j.ConsumerID, &j.PrincipalID, &j.AccessKeyID, &j.GrantVersionID, &j.IdempotencyKey, &j.Model,
		&identity, &status, &reason, &created, &notBefore, &deadline, &started, &finished, &resExp); err != nil {
		return Job{}, err
	}
	j.Identity = []byte(identity)
	j.Status = Status(status)
	j.FailureReason = ""
	if reason != nil {
		j.FailureReason = *reason
	}
	j.CreatedAt = fromMS(created)
	j.NotBefore, j.DeadlineAt = nullTime(notBefore), nullTime(deadline)
	j.StartedAt, j.FinishedAt = nullTime(started), nullTime(finished)
	j.ResultExpiresAt = nullTime(resExp)
	return j, nil
}

// getTx reads one job inside a unit of work.
func getTx(tx *storage.Tx, now time.Time, where string, args ...any) (Job, bool, error) {
	j, err := scanJob(tx.QueryRow(jobSelect(where), append([]any{ms(now)}, args...)...))
	if errors.Is(err, storage.ErrNoRows) {
		return Job{}, false, nil
	}
	return j, err == nil, err
}

// Create queues a job, or returns the job that already holds the Idempotency-Key of this access
// key. The bool is true when a job was created, false when an existing one was returned
// unchanged (req_001 D-26, D-38). A job that ended failed with host_restart or payload_minimized
// does not hold its key, so the key starts a new attempt. The consumer's jobs.deadline setting
// in force now fixes the new job's deadline.
func (s *Service) Create(ctx context.Context, n NewJob) (Job, bool, error) {
	if n.ConsumerID == "" || n.AccessKeyID == "" || n.IdempotencyKey == "" || n.Model == "" || len(n.Identity) == 0 || len(n.Payload) == 0 {
		return Job{}, false, errors.New("jobs: a job needs a consumer, an access key, an Idempotency-Key, a model, an identity and a payload")
	}
	deadline, err := s.cfg.Settings.Duration(ctx, KeyDeadline, n.ConsumerID)
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: read settings: %w", err)
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Job{}, false, err
	}
	sealed, err := s.seal(tablePayload, id.String(), n.ConsumerID, n.Payload)
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: seal payload: %w", err)
	}
	now := s.now()
	job := Job{ID: id.String(), ConsumerID: n.ConsumerID, PrincipalID: n.PrincipalID, AccessKeyID: n.AccessKeyID,
		GrantVersionID: n.GrantVersionID, IdempotencyKey: n.IdempotencyKey, Model: n.Model, Identity: n.Identity,
		Status: Queued, CreatedAt: fromMS(ms(now)), NotBefore: nullable(n.NotBefore)}
	if deadline > 0 {
		job.DeadlineAt = fromMS(ms(now.Add(deadline)))
	}
	created := false
	err = s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		created = false
		if ex, found, err := getTx(tx, now, `j.access_key_id = ? AND j.idempotency_key = ? AND `+holdsKey, n.AccessKeyID, n.IdempotencyKey); err != nil {
			return err
		} else if found {
			job = ex
			return nil
		}
		if _, err := tx.Exec(`INSERT INTO sch_job (id, consumer_id, principal_id, access_key_id, grant_version_id, idempotency_key, model,
			identity, status, created_at, not_before, deadline_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?, ?, ?)`,
			job.ID, job.ConsumerID, job.PrincipalID, job.AccessKeyID, job.GrantVersionID, job.IdempotencyKey, job.Model,
			string(job.Identity), ms(job.CreatedAt), optMS(job.NotBefore), optMS(job.DeadlineAt)); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sch_job_payload (job_id, sealed, created_at) VALUES (?, ?, ?)`, job.ID, sealed, ms(job.CreatedAt)); err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: create: %w", err)
	}
	if created {
		s.poke()
	}
	return job, created, nil
}

func nullable(t time.Time) time.Time {
	if t.IsZero() {
		return time.Time{}
	}
	return fromMS(ms(t))
}

// FindByIdempotencyKey returns the job that holds the Idempotency-Key of the access key, and
// whether there is one. A job that ended failed with host_restart or payload_minimized does not
// hold its key (req_001 D-26, D-38).
func (s *Service) FindByIdempotencyKey(ctx context.Context, accessKeyID, key string) (Job, bool, error) {
	j, err := scanJob(s.cfg.DB.Reader().QueryRow(ctx, jobSelect(`j.access_key_id = ? AND j.idempotency_key = ? AND `+holdsKey),
		ms(s.now()), accessKeyID, key))
	if errors.Is(err, storage.ErrNoRows) {
		return Job{}, false, nil
	}
	if err != nil {
		return Job{}, false, fmt.Errorf("jobs: find by idempotency key: %w", err)
	}
	return j, true, nil
}

// QueuedWaiting counts the queued jobs the worker has not handed to the scheduler: the ones still
// waiting in the job queue, including a job kept back in memory because its retry time could not be
// stored (s.hold). A job the worker has handed to the scheduler is waiting in the scheduler's queue,
// which counts it, or is admitted already.
func (s *Service) QueuedWaiting(ctx context.Context) (int, error) {
	s.mu.Lock()
	held := make([]any, 0, len(s.active))
	for id := range s.active {
		held = append(held, id)
	}
	s.mu.Unlock()
	q := `SELECT count(*) FROM sch_job WHERE status = 'queued'`
	if len(held) > 0 {
		q += ` AND id NOT IN (` + strings.TrimSuffix(strings.Repeat("?, ", len(held)), ", ") + `)`
	}
	var n int
	if err := s.cfg.DB.Reader().QueryRow(ctx, q, held...).Scan(&n); err != nil {
		return 0, fmt.Errorf("jobs: count queued jobs: %w", err)
	}
	return n, nil
}

// Get returns a job of the consumer, or ErrNotFound.
func (s *Service) Get(ctx context.Context, consumerID, id string) (Job, error) {
	j, err := scanJob(s.cfg.DB.Reader().QueryRow(ctx, jobSelect(`j.id = ? AND j.consumer_id = ?`), ms(s.now()), id, consumerID))
	if errors.Is(err, storage.ErrNoRows) {
		return Job{}, ErrNotFound
	}
	if err != nil {
		return Job{}, fmt.Errorf("jobs: get: %w", err)
	}
	return j, nil
}

// Cancel moves a queued or running job of the consumer to cancelled (req_001 D-38). The payload is
// deleted with it. A running job's engine call is cancelled, and the worker meters the tokens used
// so far. A job already in a terminal state is returned unchanged with ErrFinished; an unknown job,
// or one of another consumer, is ErrNotFound.
func (s *Service) Cancel(ctx context.Context, consumerID, id string) (Job, error) {
	now := s.now()
	var out Job
	finished := false
	err := s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		finished = false
		j, found, err := getTx(tx, now, `j.id = ? AND j.consumer_id = ?`, id, consumerID)
		if err != nil {
			return err
		}
		if !found {
			return ErrNotFound
		}
		out = j
		if j.Status.Terminal() {
			finished = true
			return nil
		}
		if _, err := tx.Exec(`UPDATE sch_job SET status = 'cancelled', finished_at = ? WHERE id = ? AND status IN ('queued', 'running')`, ms(now), id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM sch_job_payload WHERE job_id = ?`, id); err != nil {
			return err
		}
		out.Status, out.FinishedAt = Cancelled, fromMS(ms(now))
		return nil
	})
	switch {
	case errors.Is(err, ErrNotFound):
		return Job{}, ErrNotFound
	case err != nil:
		return Job{}, fmt.Errorf("jobs: cancel: %w", err)
	case finished:
		return out, ErrFinished
	}
	s.finished.Inc(string(Cancelled))
	s.abort(id, errCancelled)
	return out, nil
}

// Result returns the answer of a succeeded job of the consumer. It does not delete it: the caller
// deletes it with DeleteResult once the response has been written whole (req_001 D-85), so a
// dropped connection leaves it fetchable until its time-to-live. ErrNotFound for an unknown job or
// one of another consumer, ErrNotSucceeded for a job that has not succeeded, ErrResultGone for a
// result already delivered or past its time-to-live.
func (s *Service) Result(ctx context.Context, consumerID, id string) (Result, error) {
	j, err := s.Get(ctx, consumerID, id)
	if err != nil {
		return Result{}, err
	}
	if j.Status != Succeeded {
		return Result{}, ErrNotSucceeded
	}
	var sealed []byte
	var receipt string
	var exp int64
	err = s.cfg.DB.Reader().QueryRow(ctx, `SELECT sealed, receipt, expires_at FROM sch_job_result WHERE job_id = ?`, id).Scan(&sealed, &receipt, &exp)
	if errors.Is(err, storage.ErrNoRows) {
		return Result{}, ErrResultGone
	}
	if err != nil {
		return Result{}, fmt.Errorf("jobs: read result: %w", err)
	}
	if !s.now().Before(fromMS(exp)) {
		return Result{}, ErrResultGone
	}
	body, err := s.unseal(tableResult, id, consumerID, sealed)
	if err != nil {
		return Result{}, fmt.Errorf("jobs: open result: %w", err)
	}
	return Result{Body: body, Receipt: receipt, ExpiresAt: fromMS(exp)}, nil
}

// DeleteResult deletes the result of a job of the consumer. Call it once a fetch has written the
// result whole to the consumer (req_001 D-85, refining D-40).
func (s *Service) DeleteResult(ctx context.Context, consumerID, id string) error {
	err := s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`DELETE FROM sch_job_result WHERE job_id = ? AND job_id IN (SELECT id FROM sch_job WHERE consumer_id = ?)`, id, consumerID)
		return err
	})
	if err != nil {
		return fmt.Errorf("jobs: delete result: %w", err)
	}
	return nil
}

// FailInterrupted marks every running job failed with reason host_restart and deletes their
// payloads (req_001 D-38, D-54). Ollama cannot resume a generation, so a job interrupted by a
// stop is not resumed; the client resubmits with its Idempotency-Key. Call it once at startup,
// before Run. It returns how many jobs it marked.
func (s *Service) FailInterrupted(ctx context.Context) (int, error) {
	now := s.now()
	var n int64
	err := s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		res, err := tx.Exec(`UPDATE sch_job SET status = 'failed', failure_reason = ?, finished_at = ? WHERE status = 'running'`, ReasonHostRestart, ms(now))
		if err != nil {
			return err
		}
		if n, err = res.RowsAffected(); err != nil {
			return err
		}
		return deleteTerminalPayloads(tx)
	})
	if err != nil {
		return 0, fmt.Errorf("jobs: mark interrupted jobs: %w", err)
	}
	if n > 0 {
		s.finished.Add(float64(n), string(Failed))
	}
	return int(n), nil
}

// deleteTerminalPayloads deletes the payload of every job that is not queued or running.
func deleteTerminalPayloads(tx *storage.Tx) error {
	_, err := tx.Exec(`DELETE FROM sch_job_payload WHERE job_id IN (SELECT id FROM sch_job WHERE status NOT IN ('queued', 'running'))`)
	return err
}

// jobDonePayload carries ids and times only (req_001 D-31, D-40). data mirrors the JobDoneEvent
// schema of the webhook, with the consumer id the delivery routes by.
type jobDonePayload struct {
	JobID           string `json:"job_id"`
	ConsumerID      string `json:"consumer_id"`
	Status          string `json:"status"`
	ResultExpiresAt string `json:"result_expires_at"`
}

// errLost means another transition (cancel, expiry) got to the job first.
var errLost = errors.New("jobs: the job left this state first")

// succeed stores the result, finishes the job and emits job.done in one unit of work. A false
// result means the job was cancelled first: nothing is stored.
func (s *Service) succeed(ctx context.Context, j Job, out Output) (bool, error) {
	ttl, err := s.cfg.Settings.Duration(ctx, KeyResultTTL, j.ConsumerID)
	if err != nil {
		return false, fmt.Errorf("jobs: read settings: %w", err)
	}
	sealed, err := s.seal(tableResult, j.ID, j.ConsumerID, out.Body)
	if err != nil {
		return false, fmt.Errorf("jobs: seal result: %w", err)
	}
	now := s.now()
	exp := now.Add(ttl)
	payload, err := json.Marshal(jobDonePayload{JobID: j.ID, ConsumerID: j.ConsumerID, Status: string(Succeeded),
		ResultExpiresAt: fromMS(ms(exp)).Format(time.RFC3339Nano)})
	if err != nil {
		return false, err
	}
	err = s.cfg.Events.Write(ctx, s.cfg.DB, func(tx *storage.Tx) error {
		res, err := tx.Exec(`UPDATE sch_job SET status = 'succeeded', finished_at = ? WHERE id = ? AND status = 'running'`, ms(now), j.ID)
		if err != nil {
			return err
		}
		if n, err := res.RowsAffected(); err != nil {
			return err
		} else if n == 0 {
			return errLost
		}
		if _, err := tx.Exec(`DELETE FROM sch_job_payload WHERE job_id = ?`, j.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO sch_job_result (job_id, sealed, receipt, created_at, expires_at) VALUES (?, ?, ?, ?, ?)`,
			j.ID, sealed, out.Receipt, ms(now), ms(exp)); err != nil {
			return err
		}
		_, err = s.cfg.Events.Emit(tx, events.Event{Type: events.JobDone, TargetPrincipalID: j.PrincipalID, Payload: payload})
		return err
	})
	if errors.Is(err, errLost) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("jobs: store result: %w", err)
	}
	s.finished.Inc(string(Succeeded))
	return true, nil
}

// transition moves a job from one of the states in from to a terminal state and deletes its
// payload. It reports whether the job was still in one of them.
func (s *Service) transition(ctx context.Context, id string, to Status, reason string, from ...Status) (bool, error) {
	if len(from) == 0 {
		return false, errors.New("jobs: transition needs a source state")
	}
	marks := strings.TrimSuffix(strings.Repeat("?, ", len(from)), ", ")
	args := []any{string(to), nilIfEmpty(reason), ms(s.now()), id}
	for _, f := range from {
		args = append(args, string(f))
	}
	var moved bool
	err := s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		moved = false
		res, err := tx.Exec(`UPDATE sch_job SET status = ?, failure_reason = ?, finished_at = ? WHERE id = ? AND status IN (`+marks+`)`, args...)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if moved = n > 0; !moved {
			return nil
		}
		_, err = tx.Exec(`DELETE FROM sch_job_payload WHERE job_id = ?`, id)
		return err
	})
	if err != nil {
		return false, fmt.Errorf("jobs: move job to %s: %w", to, err)
	}
	if moved {
		s.finished.Inc(string(to))
	}
	return moved, nil
}

func nilIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}
