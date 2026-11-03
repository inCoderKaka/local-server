package jobs

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/inCoderKaka/local-server/internal/storage"
)

// Causes a job's context is cancelled with, to tell them from the host stopping.
var (
	errCancelled = errors.New("jobs: cancelled by the consumer")
	errExpired   = errors.New("jobs: past its deadline")
)

// defaultPollInterval is used only when the poll interval setting cannot be read.
const defaultPollInterval = 2 * time.Second

// poke wakes the worker so a new job starts without waiting a poll interval.
func (s *Service) poke() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// abort cancels the context of a job the worker holds, if it holds it.
func (s *Service) abort(id string, cause error) {
	s.mu.Lock()
	cancel := s.active[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel(cause)
	}
}

// Run is the job worker (req_001 D-39 F2): until ctx ends it expires jobs past their deadline,
// deletes results past their time-to-live, and hands queued jobs to the scheduler through exec,
// oldest first; the scheduler orders them against interactive calls by priority and aging
// (req_001 D-26). A job whose admission or run was cut short by ctx stays as it is: a running one
// becomes failed(host_restart) at the next start. Run returns when ctx has ended and every job it
// started has settled. Call FailInterrupted before it.
func (s *Service) Run(ctx context.Context, exec Executor) {
	defer s.wg.Wait()
	for {
		if err := s.tick(ctx, exec); err != nil && ctx.Err() == nil {
			s.log.ErrorContext(ctx, "jobs: worker pass failed", "err", err)
		}
		wait := defaultPollInterval
		if d, err := s.cfg.Settings.Duration(ctx, KeyPollInterval, ""); err == nil {
			wait = d
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-s.wake:
			t.Stop()
		case <-t.C:
		}
		if ctx.Err() != nil {
			return
		}
	}
}

// tick is one pass: sweep, then start what fits.
func (s *Service) tick(ctx context.Context, exec Executor) error {
	sweepErr := s.sweep(ctx)
	startErr := s.dispatch(ctx, exec)
	return errors.Join(sweepErr, startErr)
}

// sweep expires queued jobs past their deadline (req_001 D-38), deletes results past their
// time-to-live (D-40) and deletes any payload left behind a terminal job. It reads before it
// writes, so a quiet host does not occupy the single writer.
func (s *Service) sweep(ctx context.Context) error {
	now := s.now()
	rd := s.cfg.DB.Reader()
	var errs []error

	ids, err := queryIDs(ctx, rd, `SELECT id FROM sch_job WHERE status = 'queued' AND deadline_at IS NOT NULL AND deadline_at <= ?`, ms(now))
	errs = append(errs, err)
	for _, id := range ids {
		moved, err := s.transition(ctx, id, Expired, "", Queued)
		errs = append(errs, err)
		if moved {
			s.abort(id, errExpired)
		}
	}

	var n int
	err = rd.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM sch_job_result WHERE expires_at <= ? LIMIT 1)`, ms(now)).Scan(&n)
	errs = append(errs, err)
	if err == nil && n > 0 {
		errs = append(errs, s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
			_, err := tx.Exec(`DELETE FROM sch_job_result WHERE expires_at <= ?`, ms(now))
			return err
		}))
	}

	err = rd.QueryRow(ctx, `SELECT count(*) FROM (SELECT 1 FROM sch_job_payload p JOIN sch_job j ON j.id = p.job_id WHERE j.status NOT IN ('queued', 'running') LIMIT 1)`).Scan(&n)
	errs = append(errs, err)
	if err == nil && n > 0 {
		errs = append(errs, s.cfg.DB.Write(ctx, deleteTerminalPayloads))
	}
	return errors.Join(errs...)
}

func queryIDs(ctx context.Context, rd *storage.Reader, query string, args ...any) ([]string, error) {
	rows, err := rd.Query(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// dispatch starts queued jobs, oldest first, while fewer than jobs.max_active are active.
func (s *Service) dispatch(ctx context.Context, exec Executor) error {
	limit, err := s.cfg.Settings.Int(ctx, KeyMaxActive, "")
	if err != nil {
		return fmt.Errorf("jobs: read settings: %w", err)
	}
	s.mu.Lock()
	free := int(limit) - len(s.active)
	nHeld := len(s.active)
	s.mu.Unlock()
	if free <= 0 {
		return nil
	}
	now := s.now()
	// A job already held is still queued while it waits for a slot, so read past them.
	ids, err := queryIDs(ctx, s.cfg.DB.Reader(),
		`SELECT id FROM sch_job WHERE status = 'queued' AND (not_before IS NULL OR not_before <= ?) ORDER BY created_at, id LIMIT ?`,
		ms(now), free+nHeld)
	if err != nil {
		return err
	}
	for _, id := range ids {
		s.mu.Lock()
		_, held := s.active[id]
		full := len(s.active) >= int(limit)
		s.mu.Unlock()
		if held {
			continue
		}
		if full {
			break
		}
		j, err := scanJob(s.cfg.DB.Reader().QueryRow(ctx, jobSelect(`j.id = ?`), ms(now), id))
		if errors.Is(err, storage.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		if j.Status != Queued {
			continue
		}
		jctx, cancel := context.WithCancelCause(ctx)
		s.mu.Lock()
		s.active[id] = cancel
		s.mu.Unlock()
		s.wg.Add(1)
		go s.process(jctx, ctx, cancel, exec, j)
	}
	return nil
}

// process takes one job from queued to a terminal state, or leaves it queued to try again.
// jctx ends when the job is cancelled or expires, or when the worker stops (parent).
func (s *Service) process(jctx, parent context.Context, cancel context.CancelCauseFunc, exec Executor, j Job) {
	defer s.wg.Done()
	// Writes that settle the job must outlive a stop: the work is done and metered.
	wctx := context.WithoutCancel(parent)
	defer func() {
		if p := recover(); p != nil {
			// A panic value can hold request bytes; log its type only (req_001 D-31).
			s.log.ErrorContext(wctx, "jobs: job panicked", "job_id", j.ID, "panic_type", fmt.Sprintf("%T", p))
			s.settleFailed(wctx, j, ReasonInternal, Queued, Running)
		}
		cancel(nil)
		s.mu.Lock()
		delete(s.active, j.ID)
		s.mu.Unlock()
		s.poke()
	}()

	payload, err := s.payload(jctx, j)
	switch {
	case errors.Is(err, ErrNotFound):
		// No payload for a queued job: nothing can run it. This is what a restore from a backup
		// that left the content out leaves behind (req_001 D-68).
		s.settleFailed(wctx, j, ReasonPayloadMinimized, Queued)
		return
	case errors.Is(err, errUnreadable):
		s.settleFailed(wctx, j, ReasonPayloadUnreadable, Queued)
		return
	case err != nil:
		if jctx.Err() == nil {
			s.retryLater(wctx, jctx, j, 0, err)
		}
		return
	}

	slot, err := exec.Admit(jctx, j, payload)
	if err != nil {
		s.admitFailed(wctx, jctx, j, err)
		return
	}
	defer slot.Release() // a no-op after Run; frees the slot if anything below panics
	started, err := s.markRunning(wctx, j.ID)
	if err != nil || !started {
		if err != nil {
			s.log.ErrorContext(jctx, "jobs: job could not be marked running", "job_id", j.ID, "err", err)
		}
		return
	}
	out, err := slot.Run(jctx)
	slot.Release()
	switch {
	case err == nil:
		ok, serr := s.succeed(wctx, j, out)
		if serr != nil {
			s.log.ErrorContext(jctx, "jobs: result could not be stored", "job_id", j.ID, "err", serr)
			s.settleFailed(wctx, j, ReasonInternal, Running)
		} else if !ok {
			s.log.InfoContext(jctx, "jobs: job finished after it was cancelled; its result was discarded", "job_id", j.ID)
		}
	case jctx.Err() != nil:
		// Cancelled by the consumer: Cancel already moved the job. The host stopping: the job
		// stays running and becomes failed(host_restart) at the next start.
	default:
		var fe *FailError
		reason := ReasonInternal
		if errors.As(err, &fe) {
			reason = fe.Reason
		} else {
			s.log.ErrorContext(jctx, "jobs: run failed", "job_id", j.ID, "err", err)
		}
		s.settleFailed(wctx, j, reason, Running)
	}
}

// admitFailed settles a job whose Admit returned err.
func (s *Service) admitFailed(wctx, jctx context.Context, j Job, err error) {
	if jctx.Err() != nil {
		return // cancelled, expired, or the host is stopping: the job keeps its state
	}
	var de *DeferError
	var fe *FailError
	switch {
	case errors.As(err, &fe):
		s.settleFailed(wctx, j, fe.Reason, Queued)
	case errors.As(err, &de):
		s.retryLater(wctx, jctx, j, de.After, nil)
	default:
		s.retryLater(wctx, jctx, j, 0, err)
	}
}

// retryLater keeps a job queued and not admissible until after has passed (jobs.retry_interval
// when after is zero). cause, when set, is logged.
func (s *Service) retryLater(wctx, jctx context.Context, j Job, after time.Duration, cause error) {
	if cause != nil {
		s.log.WarnContext(jctx, "jobs: job could not be started; it stays queued", "job_id", j.ID, "err", cause)
	}
	if after <= 0 {
		d, err := s.cfg.Settings.Duration(wctx, KeyRetryInterval, "")
		if err != nil {
			d = 30 * time.Second
		}
		after = d
	}
	err := s.cfg.DB.Write(wctx, func(tx *storage.Tx) error {
		_, err := tx.Exec(`UPDATE sch_job SET not_before = ? WHERE id = ? AND status = 'queued'`, ms(s.now().Add(after)), j.ID)
		return err
	})
	if err != nil {
		s.log.ErrorContext(jctx, "jobs: retry time could not be stored", "job_id", j.ID, "err", err)
	}
}

func (s *Service) settleFailed(wctx context.Context, j Job, reason string, from ...Status) {
	if _, err := s.transition(wctx, j.ID, Failed, reason, from...); err != nil {
		s.log.ErrorContext(wctx, "jobs: job could not be marked failed", "job_id", j.ID, "reason", reason, "err", err)
	}
}

// markRunning moves a queued job to running, and reports whether it was still queued.
func (s *Service) markRunning(ctx context.Context, id string) (bool, error) {
	var moved bool
	err := s.cfg.DB.Write(ctx, func(tx *storage.Tx) error {
		res, err := tx.Exec(`UPDATE sch_job SET status = 'running', started_at = ?, not_before = NULL WHERE id = ? AND status = 'queued'`, ms(s.now()), id)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		moved = n > 0
		return err
	})
	return moved, err
}

var errUnreadable = errors.New("jobs: payload cannot be opened")

// payload reads and opens a job's request content. ErrNotFound means there is none.
func (s *Service) payload(ctx context.Context, j Job) ([]byte, error) {
	var sealed []byte
	err := s.cfg.DB.Reader().QueryRow(ctx, `SELECT sealed FROM sch_job_payload WHERE job_id = ?`, j.ID).Scan(&sealed)
	if errors.Is(err, storage.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("jobs: read payload: %w", err)
	}
	plain, err := s.unseal(tablePayload, j.ID, j.ConsumerID, sealed)
	if err != nil {
		return nil, errUnreadable
	}
	return plain, nil
}
