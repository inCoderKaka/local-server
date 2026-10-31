package scheduler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/inCoderKaka/local-server/internal/engine"
	"github.com/inCoderKaka/local-server/internal/host"
	"github.com/inCoderKaka/local-server/internal/settings"
	"github.com/inCoderKaka/local-server/internal/telemetry"
)

// Engine is the part of the engine port the scheduler uses (req_001 D-22).
// engine.Engine and engine/fake.Engine satisfy it.
type Engine interface {
	Load(ctx context.Context, name string, keepAlive time.Duration) error
	Unload(ctx context.Context, name string) error
	Loaded(ctx context.Context) ([]engine.LoadedModel, error)
	ListModels(ctx context.Context) ([]engine.Model, error)
}

// Sharing reports whether the host is serving. *host.SharingState satisfies it.
type Sharing interface {
	Effective(ctx context.Context) (host.State, error)
}

// Class is the priority class of a request (req_001 D-26).
type Class string

// The classes.
const (
	// ClassInteractive is a synchronous call; it goes ahead of background work.
	ClassInteractive Class = "interactive"
	// ClassBackground is queued job work; it ages so it is never starved.
	ClassBackground Class = "background"
)

// Request is what Admit needs to know. It carries sizes and ids, never content
// (req_001 D-31, D-40).
type Request struct {
	GrantID string
	// GrantConcurrency is the grant's concurrency limit; 0 uses the
	// KeyGrantConcurrency setting.
	GrantConcurrency int
	Class            Class
	Model            string
	// EstimatedCtxTokens is the context the call will use (the num_ctx it sends).
	EstimatedCtxTokens int
}

// Options configure New. The zero value uses the real clock and telemetry.Nop until
// startup wires the real adapter.
type Options struct {
	Clock     Clock
	Telemetry telemetry.Telemetry
}

// Stats is a point-in-time count, for tests, metrics and the admin UI.
type Stats struct {
	Queued  int
	Running int
}

type waitState int

const (
	waiting waitState = iota
	admitted
	failed
)

type waiter struct {
	req      Request
	enq      time.Time
	seq      uint64
	ctxBytes int64
	state    waitState
	err      error
	ticket   *Ticket
	needLoad bool
	done     chan struct{}
}

// Scheduler admits requests and manages model lifecycle. Create it with New.
type Scheduler struct {
	eng     Engine
	st      *settings.Store
	sharing Sharing
	clock   Clock
	log     *slog.Logger

	refusals       telemetry.Counter // by code
	unloads        telemetry.Counter // by reason
	unloadFailures telemetry.Counter // by reason

	// dispatchMu serialises everything that admits, unloads or loads on behalf of the
	// queue (dispatch, Apply, Sweep), so a model is never unloaded while it is being
	// admitted. It is taken before mu and never while holding mu.
	dispatchMu sync.Mutex

	mu       sync.Mutex
	queue    []*waiter
	seq      uint64
	running  int
	byGrant  map[string]int
	byModel  map[string]int
	reserved int64            // estimated context bytes of running requests
	loading  map[string]int64 // models an admitted request is loading, with their size
	lastUsed map[string]time.Time
	pinned   map[string]bool // preloaded models, exempt from idle unload
}

// New registers the scheduler settings with st and returns the scheduler.
func New(eng Engine, st *settings.Store, sharing Sharing, opts Options) (*Scheduler, error) {
	if err := st.Register(Defs()...); err != nil {
		return nil, err
	}
	if err := setValidators(st); err != nil {
		return nil, err
	}
	if opts.Clock == nil {
		opts.Clock = realClock{}
	}
	if opts.Telemetry == nil {
		opts.Telemetry = telemetry.Nop()
	}
	return &Scheduler{
		eng: eng, st: st, sharing: sharing, clock: opts.Clock, log: opts.Telemetry.Logger(),
		refusals:       opts.Telemetry.Counter("scheduler_refusals_total", "code"),
		unloads:        opts.Telemetry.Counter("scheduler_unloads_total", "reason"),
		unloadFailures: opts.Telemetry.Counter("scheduler_unload_failures_total", "reason"),
		byGrant:        map[string]int{}, byModel: map[string]int{}, loading: map[string]int64{},
		lastUsed: map[string]time.Time{}, pinned: map[string]bool{},
	}, nil
}

// Stats returns the current queue and running counts.
func (s *Scheduler) Stats() Stats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return Stats{Queued: len(s.queue), Running: s.running}
}

// Ticket is a granted slot. Release it exactly when the engine call ends; a second
// Release does nothing.
type Ticket struct {
	s         *Scheduler
	grant     string
	model     string
	ctxBytes  int64
	keepAlive time.Duration
	once      sync.Once
}

// Model is the model the slot is for.
func (t *Ticket) Model() string { return t.model }

// KeepAlive is the keep-alive the caller should send to the engine with the call (a
// negative value keeps the model until unloaded). It is the keep-alive policy setting,
// or "until unloaded" for a preloaded model.
func (t *Ticket) KeepAlive() time.Duration { return t.keepAlive }

// Release frees the slot and the reserved memory, records the model as just used and
// admits the next waiting request.
func (t *Ticket) Release() {
	t.once.Do(func() {
		s := t.s
		s.mu.Lock()
		s.running--
		s.byGrant[t.grant]--
		if s.byGrant[t.grant] <= 0 {
			delete(s.byGrant, t.grant)
		}
		s.byModel[t.model]--
		if s.byModel[t.model] <= 0 {
			delete(s.byModel, t.model)
		}
		s.reserved -= t.ctxBytes
		s.lastUsed[t.model] = s.clock.Now()
		s.mu.Unlock()
		if err := s.dispatch(context.Background()); err != nil {
			s.log.Warn("scheduler: dispatch after release failed", "err", err)
		}
	})
}

// Admit waits for a slot and returns its Ticket. The wait follows req_001 D-26 and
// D-39 F1 step 5:
//   - Interactive: ErrHostPaused at once while the host is paused or quiet; waits up to
//     the queue-wait setting, then ErrQueueFull.
//   - Background: waits while the host is paused or quiet, and has no wait limit but ctx.
//   - Both: ErrQueueFull at once when the queue is full, except that an interactive
//     request finding it full takes the place of the newest waiting background request,
//     which then gets ErrQueueFull; ErrOverBudget when the model plus the estimated
//     context can never fit the memory budget beside the preloaded models.
//
// If the model is not loaded, Admit loads it before returning. A cancelled ctx removes
// the request from the queue and returns ctx.Err().
func (s *Scheduler) Admit(ctx context.Context, req Request) (*Ticket, error) {
	tk, err := s.admit(ctx, req)
	var se *Error
	if errors.As(err, &se) {
		s.refusals.Inc(string(se.Code))
	}
	return tk, err
}

func (s *Scheduler) admit(ctx context.Context, req Request) (*Ticket, error) {
	if req.Model == "" || (req.Class != ClassInteractive && req.Class != ClassBackground) {
		return nil, errors.New("scheduler: request needs a model and a class")
	}
	if req.GrantID == "" {
		return nil, errors.New("scheduler: request needs a grant")
	}
	if req.EstimatedCtxTokens < 0 {
		return nil, errors.New("scheduler: negative estimated context")
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return nil, err
	}
	if req.Class == ClassInteractive {
		state, err := s.sharingState(ctx)
		if err != nil {
			return nil, err
		}
		if state != host.Sharing {
			return nil, pausedErr()
		}
	}
	ctxBytes := int64(req.EstimatedCtxTokens) * cfg.ctxBytes
	if cfg.budget > 0 {
		lm, err := s.eng.Loaded(ctx)
		if err != nil {
			return nil, err
		}
		loaded := make(map[string]int64, len(lm))
		for _, m := range lm {
			loaded[m.Name] = m.SizeBytes
		}
		size, err := s.modelSize(ctx, loaded, req.Model)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		pinnedBytes := s.pinnedLoadedBytesLocked(loaded, req.Model)
		s.mu.Unlock()
		if size+ctxBytes+pinnedBytes > cfg.budget {
			return nil, &Error{Code: CodeOverBudget,
				Message: fmt.Sprintf("model %s with a context of %d tokens needs %d bytes; the memory budget is %d bytes, of which preloaded models use %d", req.Model, req.EstimatedCtxTokens, size+ctxBytes, cfg.budget, pinnedBytes)}
		}
	}

	w := &waiter{req: req, enq: s.clock.Now(), ctxBytes: ctxBytes, done: make(chan struct{})}
	s.mu.Lock()
	s.seq++
	w.seq = s.seq
	s.queue = append(s.queue, w)
	s.mu.Unlock()

	dctx := context.WithoutCancel(ctx)
	if err := s.dispatch(dctx); err != nil {
		if s.cancelWaiter(w) {
			return nil, err
		}
	}
	s.mu.Lock()
	if w.state == waiting && len(s.queue) > cfg.queueLen {
		// An interactive arrival goes ahead of background work (req_001 D-26): it takes
		// the place of the newest waiting background request, which is refused.
		var v *waiter
		if req.Class == ClassInteractive {
			for _, q := range s.queue {
				if q != w && q.req.Class == ClassBackground && (v == nil || q.seq > v.seq) {
					v = q
				}
			}
		}
		if v == nil {
			s.removeLocked(w)
			s.mu.Unlock()
			return nil, queueFullErr(cfg)
		}
		s.removeLocked(v)
		v.err = queueFullErr(cfg)
		close(v.done)
	}
	s.mu.Unlock()

	var wait Timer
	var waitC <-chan time.Time
	if req.Class == ClassInteractive {
		wait = s.clock.NewTimer(cfg.queueWait)
		defer wait.Stop()
		waitC = wait.C()
	}
	recheck := s.clock.NewTimer(cfg.recheck)
	defer func() { recheck.Stop() }()
	var recheckErr error // the last recheck's failure; logged once per outage, not once per interval
	for {
		select {
		case <-w.done:
			return s.finish(ctx, w, cfg)
		case <-ctx.Done():
			if s.cancelWaiter(w) {
				return nil, ctx.Err()
			}
			<-w.done
			if w.err == nil {
				s.giveBack(w) // granted at the same moment: give it back
			}
			return nil, ctx.Err()
		case <-waitC:
			if s.cancelWaiter(w) {
				if recheckErr != nil {
					return nil, recheckErr // the engine is down; say so, not queue_full
				}
				return nil, queueFullErr(cfg)
			}
			waitC = nil
		case <-recheck.C():
			// Re-read the settings and the sharing state, so a waiting request notices
			// a raised limit, a resume or a pause even with no release to wake it.
			if c, err := s.loadConfig(ctx); err == nil {
				cfg = c
			}
			if err := s.dispatch(dctx); err != nil {
				if recheckErr == nil {
					s.log.Warn("scheduler: recheck failed", "err", err)
				}
				recheckErr = err
			} else {
				recheckErr = nil
			}
			recheck = s.clock.NewTimer(cfg.recheck)
		}
	}
}

// finish returns the outcome of a decided waiter, loading its model if needed.
func (s *Scheduler) finish(ctx context.Context, w *waiter, cfg config) (*Ticket, error) {
	if w.err != nil {
		return nil, w.err
	}
	if w.needLoad {
		err := s.eng.Load(ctx, w.req.Model, w.ticket.keepAlive)
		s.mu.Lock()
		delete(s.loading, w.req.Model)
		s.mu.Unlock()
		if err != nil {
			w.ticket.Release()
			return nil, err
		}
	}
	return w.ticket, nil
}

// giveBack returns a ticket granted to a waiter whose caller has gone. If the grant
// made the waiter load its model, the load marker is cleared first, so the model is
// not left looking as if it were loading for good.
func (s *Scheduler) giveBack(w *waiter) {
	if w.needLoad {
		s.mu.Lock()
		delete(s.loading, w.req.Model)
		s.mu.Unlock()
	}
	w.ticket.Release()
}

func (s *Scheduler) sharingState(ctx context.Context) (host.State, error) {
	state, err := s.sharing.Effective(ctx)
	if err != nil && !errors.Is(err, host.ErrInvalidSchedule) {
		return "", fmt.Errorf("scheduler: sharing state: %w", err)
	}
	// With an unusable quiet-hours schedule Effective still returns the manual state, so
	// a pause is honoured (host.SharingState.EffectiveAt).
	return state, nil
}

// pausedErr carries no Retry-After: the scheduler does not know when a pause or quiet
// hours end.
func pausedErr() *Error {
	return &Error{Code: CodeHostPaused, Message: "the host is paused or in quiet hours"}
}

func queueFullErr(cfg config) *Error {
	return &Error{Code: CodeQueueFull, RetryAfter: cfg.retryAfter, Message: "the admission queue is full"}
}

// removeLocked drops w from the queue. Callers hold s.mu.
func (s *Scheduler) removeLocked(w *waiter) {
	for i, q := range s.queue {
		if q == w {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			break
		}
	}
	w.state = failed
}

// cancelWaiter removes w if it is still waiting and reports whether it did.
func (s *Scheduler) cancelWaiter(w *waiter) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.state != waiting {
		return false
	}
	s.removeLocked(w)
	return true
}

// snapshot is the engine state one dispatch decides on.
type snapshot struct {
	loaded    map[string]int64 // loaded model -> bytes
	sizes     map[string]int64 // installed model -> bytes (only filled under a budget)
	noReclaim map[string]bool  // models whose unload just failed
}

func (s *Scheduler) takeSnapshot(ctx context.Context, cfg config) (*snapshot, error) {
	snap := &snapshot{loaded: map[string]int64{}, sizes: map[string]int64{}, noReclaim: map[string]bool{}}
	lm, err := s.eng.Loaded(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range lm {
		snap.loaded[m.Name] = m.SizeBytes
	}
	if cfg.budget > 0 {
		need := false
		s.mu.Lock()
		for _, w := range s.queue {
			if _, ok := snap.loaded[w.req.Model]; !ok {
				need = true
			}
		}
		s.mu.Unlock()
		if need {
			ms, err := s.eng.ListModels(ctx)
			if err != nil {
				return nil, err
			}
			for _, m := range ms {
				snap.sizes[m.Name] = m.SizeBytes
			}
		}
	}
	for n, b := range snap.loaded {
		snap.sizes[n] = b
	}
	return snap, nil
}

// modelSize is the memory a model takes: its size in loaded (the loaded models and their
// sizes), else its installed size.
func (s *Scheduler) modelSize(ctx context.Context, loaded map[string]int64, name string) (int64, error) {
	if b, ok := loaded[name]; ok {
		return b, nil
	}
	ms, err := s.eng.ListModels(ctx)
	if err != nil {
		return 0, err
	}
	for _, m := range ms {
		if m.Name == name {
			return m.SizeBytes, nil
		}
	}
	return 0, engine.Errorf(engine.CodeModelNotFound, nil, "model %s", name)
}

// dispatch admits every queued request that can run now, in priority order with
// aging, and fails queued interactive requests while the host is paused or quiet.
// Admission is the only thing that starts work; nothing here touches a running request
// (req_001 D-26).
func (s *Scheduler) dispatch(ctx context.Context) error {
	s.dispatchMu.Lock()
	defer s.dispatchMu.Unlock()
	s.mu.Lock()
	n := len(s.queue)
	s.mu.Unlock()
	if n == 0 {
		return nil
	}
	cfg, err := s.loadConfig(ctx)
	if err != nil {
		return err
	}
	state, err := s.sharingState(ctx)
	if err != nil {
		return err
	}
	if state != host.Sharing {
		s.failInteractive(pausedErr())
		return nil // background work stays queued while paused or quiet (req_001 D-38 host sharing)
	}
	snap, err := s.takeSnapshot(ctx, cfg)
	if err != nil {
		return err
	}
	for {
		w, unloads := s.pick(cfg, snap)
		if w == nil {
			return nil
		}
		unloaded := true
		for _, m := range unloads {
			if err := s.eng.Unload(ctx, m); err != nil {
				s.log.Warn("scheduler: reclaim unload failed", "model", m, "err", err)
				s.unloadFailures.Inc("reclaim")
				snap.noReclaim[m] = true
				unloaded = false
				break
			}
			s.unloads.Inc("reclaim")
			delete(snap.loaded, m)
			s.mu.Lock()
			delete(s.lastUsed, m)
			s.mu.Unlock()
		}
		if unloaded {
			s.commit(w, cfg, snap)
		}
	}
}

// failInteractive fails every queued interactive request with err.
func (s *Scheduler) failInteractive(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := s.queue[:0]
	for _, w := range s.queue {
		if w.req.Class == ClassInteractive {
			w.state, w.err = failed, err
			close(w.done)
			continue
		}
		kept = append(kept, w)
	}
	s.queue = kept
}

// key orders the queue: the smallest goes first. A background request is treated as
// arriving BackgroundAging later than it did, so interactive goes ahead until the
// background request has waited that long; after that it beats newer interactive
// arrivals and cannot starve (req_001 D-26).
func key(w *waiter, aging time.Duration) time.Time {
	if w.req.Class == ClassBackground {
		return w.enq.Add(aging)
	}
	return w.enq
}

// pick chooses the best queued request that can run now. It returns the idle models to
// unload first to make room (nil when none are needed). A request blocked by its
// grant's limit does not hold up the ones behind it. Nor does an interactive request
// blocked by memory. A background request blocked by memory does hold them up when it
// would fit once the running work drains, so that after it has aged it is not starved
// by newer requests that keep its room busy (req_001 D-26).
func (s *Scheduler) pick(cfg config, snap *snapshot) (*waiter, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running >= cfg.total {
		return nil, nil
	}
	order := append([]*waiter(nil), s.queue...)
	sort.SliceStable(order, func(i, j int) bool {
		ki, kj := key(order[i], cfg.aging), key(order[j], cfg.aging)
		if !ki.Equal(kj) {
			return ki.Before(kj)
		}
		return order[i].seq < order[j].seq
	})
	for _, w := range order {
		limit := w.req.GrantConcurrency
		if limit <= 0 {
			limit = cfg.grantCap
		}
		if s.byGrant[w.req.GrantID] >= limit {
			continue
		}
		unloads, ok := s.fitsLocked(cfg, snap, w)
		if ok {
			return w, unloads
		}
		if w.req.Class == ClassBackground && s.fitsWhenIdleLocked(cfg, snap, w) {
			return nil, nil
		}
	}
	return nil, nil
}

// fitsWhenIdleLocked reports whether w would fit the memory budget beside the loaded
// preloaded models once every running request has finished. A model whose reclaim just
// failed counts as staying loaded, like a preloaded one.
func (s *Scheduler) fitsWhenIdleLocked(cfg config, snap *snapshot, w *waiter) bool {
	stuck := s.pinnedLoadedBytesLocked(snap.loaded, w.req.Model)
	for n := range snap.noReclaim {
		if b, ok := snap.loaded[n]; ok && n != w.req.Model && !s.pinned[n] {
			stuck += b
		}
	}
	return snap.sizes[w.req.Model]+w.ctxBytes+stuck <= cfg.budget
}

// pinnedLoadedBytesLocked is the memory of the loaded preloaded models other than
// except. Callers hold s.mu.
func (s *Scheduler) pinnedLoadedBytesLocked(loaded map[string]int64, except string) int64 {
	var n int64
	for name, b := range loaded {
		if name != except && s.pinned[name] {
			n += b
		}
	}
	return n
}

// fitsLocked checks memory admission for w: the loaded models, the models being loaded,
// the model w needs if it is not loaded, and the estimated context of every running
// request and of w must fit the budget. If they do not, it lists the idle models whose
// unload would make them fit.
func (s *Scheduler) fitsLocked(cfg config, snap *snapshot, w *waiter) ([]string, bool) {
	if cfg.budget <= 0 {
		return nil, true
	}
	used := s.reserved + w.ctxBytes
	for _, b := range snap.loaded {
		used += b
	}
	for n, b := range s.loading {
		if _, ok := snap.loaded[n]; !ok {
			used += b
		}
	}
	_, isLoaded := snap.loaded[w.req.Model]
	_, isLoading := s.loading[w.req.Model]
	if !isLoaded && !isLoading {
		used += snap.sizes[w.req.Model]
	}
	if used <= cfg.budget {
		return nil, true
	}
	// Reclaim: idle, unpinned models other than w's, least recently used first.
	var cands []string
	for n := range snap.loaded {
		if n == w.req.Model || s.byModel[n] > 0 || s.pinned[n] || snap.noReclaim[n] {
			continue
		}
		if _, ok := s.loading[n]; ok {
			continue
		}
		cands = append(cands, n)
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := s.lastUsed[cands[i]], s.lastUsed[cands[j]]
		if !a.Equal(b) {
			return a.Before(b)
		}
		return cands[i] < cands[j]
	})
	var unloads []string
	for _, n := range cands {
		unloads = append(unloads, n)
		used -= snap.loaded[n]
		if used <= cfg.budget {
			return unloads, true
		}
	}
	return nil, false
}

// commit turns a picked waiter into a running request.
func (s *Scheduler) commit(w *waiter, cfg config, snap *snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if w.state != waiting {
		return // cancelled while the unloads ran
	}
	s.removeLocked(w)
	w.state = admitted
	s.running++
	s.byGrant[w.req.GrantID]++
	s.byModel[w.req.Model]++
	s.reserved += w.ctxBytes
	s.lastUsed[w.req.Model] = s.clock.Now()
	if _, ok := snap.loaded[w.req.Model]; !ok {
		if _, busy := s.loading[w.req.Model]; !busy {
			s.loading[w.req.Model] = snap.sizes[w.req.Model]
			w.needLoad = true
			snap.loaded[w.req.Model] = snap.sizes[w.req.Model]
		}
	}
	keep := cfg.keepAlive
	if s.pinned[w.req.Model] {
		keep = -1
	}
	w.ticket = &Ticket{s: s, grant: w.req.GrantID, model: w.req.Model, ctxBytes: w.ctxBytes, keepAlive: keep}
	close(w.done)
}
