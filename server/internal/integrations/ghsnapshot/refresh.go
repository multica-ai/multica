package ghsnapshot

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TxBeginner is the subset of a pgx pool the manager needs to open the
// snapshot-write transaction. *pgxpool.Pool satisfies it.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// address is the refresh unit and the dedup / single-in-flight key
// (acceptance criterion 3): one (installation, owner, repo, number) tuple, which
// may fan out to multiple github_pull_request rows across workspaces.
type address struct {
	InstallationID int64
	Owner          string
	Repo           string
	Number         int32
}

// Default tuning. The chase-window backoff climbs 30s → 1m → 2m → 5m and holds
// at 5m; a chase stops when the snapshot is decided, the PR closes, or
// maxChaseAttempts is reached — never unbounded (acceptance criterion 7). The
// TTL sweep and page-visit refresh recover anything a stopped chase misses.
var (
	defaultChaseBackoff  = []time.Duration{30 * time.Second, time.Minute, 2 * time.Minute, 5 * time.Minute}
	defaultConcurrency   = 12
	defaultViewTTL       = 60 * time.Second
	defaultSweepTTL      = 10 * time.Minute
	defaultSweepInterval = 10 * time.Minute
	defaultSweepMaxRows  = int32(200)
	maxChaseAttempts     = 12
	queueBuffer          = 2048
)

// Manager owns the outbound GitHub API refresh pipeline. A Manager whose client
// is nil/disabled is inert: every trigger method is a no-op, so a deployment
// without a GitHub App private key degrades the feature off without touching
// PR linking, merge→Done, or any other existing behavior (acceptance
// criterion 4).
type Manager struct {
	client    *Client
	queries   *db.Queries
	pool      TxBeginner
	onApplied func(ctx context.Context, prID pgtype.UUID)

	concurrency   int
	viewTTL       time.Duration
	sweepTTL      time.Duration
	sweepInterval time.Duration
	sweepMaxRows  int32
	chaseBackoff  []time.Duration
	now           func() time.Time
	jitter        func() time.Duration
	// fetch is the snapshot fetcher, a seam so tests can drive the queue /
	// backoff without a live GitHub. Defaults to FetchPRSnapshot.
	fetch func(ctx context.Context, c *Client, installationID int64, owner, repo string, number int32) (*PRSnapshot, error)

	queue chan address

	mu       sync.Mutex
	active   map[address]bool // queued OR in-flight → coalesce (single in-flight per PR)
	inFlight map[address]bool
	trailing map[address]bool // one event that arrived while active; replay once after the current fetch
	attempts map[address]int  // chase attempts for the current undecided window
	// Secondary rate limits are scoped to the installation whose token incurred
	// them. One customer must never pause every other installation.
	rateUntil map[int64]time.Time

	ctx     context.Context
	started bool
}

// NewManager wires the pipeline. onApplied is called once per PR row whose
// snapshot was actually written (guard passed), so the handler can broadcast a
// realtime PR update. Passing a nil client yields a disabled (no-op) manager.
func NewManager(client *Client, queries *db.Queries, pool TxBeginner, onApplied func(ctx context.Context, prID pgtype.UUID)) *Manager {
	return &Manager{
		client:        client,
		queries:       queries,
		pool:          pool,
		onApplied:     onApplied,
		concurrency:   defaultConcurrency,
		viewTTL:       defaultViewTTL,
		sweepTTL:      defaultSweepTTL,
		sweepInterval: defaultSweepInterval,
		sweepMaxRows:  defaultSweepMaxRows,
		chaseBackoff:  defaultChaseBackoff,
		now:           time.Now,
		jitter:        func() time.Duration { return time.Duration(rand.Int63n(int64(250 * time.Millisecond))) },
		fetch:         FetchPRSnapshot,
		queue:         make(chan address, queueBuffer),
		active:        map[address]bool{},
		inFlight:      map[address]bool{},
		trailing:      map[address]bool{},
		attempts:      map[address]int{},
		rateUntil:     map[int64]time.Time{},
	}
}

// Enabled reports whether the pipeline will actually do anything.
func (m *Manager) Enabled() bool { return m != nil && m.client.Enabled() }

// Start launches the worker pool and the TTL sweeper under ctx. No-op (and
// safe) when the manager is disabled.
func (m *Manager) Start(ctx context.Context) {
	if !m.Enabled() {
		return
	}
	m.mu.Lock()
	if m.started {
		m.mu.Unlock()
		return
	}
	m.started = true
	m.ctx = ctx
	m.mu.Unlock()

	for i := 0; i < m.concurrency; i++ {
		go m.worker(ctx)
	}
	go m.sweepLoop(ctx)
}

// Enqueue schedules a refresh for a PR address. Repeated events coalesce, but
// an event that arrives while the address is queued or in flight leaves one
// trailing refresh behind. That trailing edge matters when a synchronize event
// advances the head while the old head's request is still running: the guarded
// old response is discarded, then the new head is fetched immediately. At most
// one request per address is in flight, and at most one trailing request is
// retained. Never blocks the caller.
func (m *Manager) Enqueue(installationID int64, owner, repo string, number int32) {
	if !m.Enabled() {
		return
	}
	addr := address{InstallationID: installationID, Owner: owner, Repo: repo, Number: number}
	m.mu.Lock()
	if m.active[addr] {
		if m.inFlight[addr] {
			m.trailing[addr] = true
		}
		m.mu.Unlock()
		return
	}
	m.active[addr] = true
	m.mu.Unlock()

	select {
	case m.queue <- addr:
	default:
		// Queue is saturated; drop and let the TTL sweep / next event recover
		// rather than block a webhook handler. Unmark so it can be re-enqueued.
		m.mu.Lock()
		delete(m.active, addr)
		delete(m.inFlight, addr)
		delete(m.trailing, addr)
		m.mu.Unlock()
		slog.Warn("ghsnapshot: refresh queue full, dropping enqueue")
	}
}

// MaybeEnqueueOnView is the page-visit trigger: refresh only when the snapshot
// is missing or older than the view TTL, so opening a card that already has
// fresh data costs nothing.
func (m *Manager) MaybeEnqueueOnView(installationID int64, owner, repo string, number int32, fetchedAt time.Time, hasFetched bool) {
	if !m.Enabled() {
		return
	}
	if hasFetched && m.now().Sub(fetchedAt) < m.viewTTL {
		return
	}
	m.Enqueue(installationID, owner, repo, number)
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case addr := <-m.queue:
			m.mu.Lock()
			m.inFlight[addr] = true
			m.mu.Unlock()
			m.process(ctx, addr)
			m.finish(addr)
		}
	}
}

func (m *Manager) process(ctx context.Context, addr address) {
	// Installation-scoped secondary-rate-limit pause + per-request jitter to
	// smooth bursts.
	pause := m.rateLimitPause(addr.InstallationID)
	if pause > 0 {
		if !sleepCtx(ctx, pause) {
			return
		}
	}
	if j := m.jitter(); j > 0 {
		if !sleepCtx(ctx, j) {
			return
		}
	}

	snap, err := m.fetch(ctx, m.client, addr.InstallationID, addr.Owner, addr.Repo, addr.Number)
	if err != nil {
		var rl *RateLimitError
		if asRateLimit(err, &rl) {
			m.extendRateLimit(addr.InstallationID, rl.RetryAfter)
			// Do not create an unbounded retry loop for a persistently limited
			// installation. The bounded TTL sweep (open/draft PRs only) or the
			// next webhook/view event hands the address back after Retry-After.
			return
		}
		// Transient/GitHub failure: keep the last-known snapshot (the row is
		// untouched, so the card shows stale data, never wrong data). No secret
		// is ever logged. The next trigger or the TTL sweep retries.
		slog.Warn("ghsnapshot: fetch failed", "owner", addr.Owner, "repo", addr.Repo, "number", addr.Number, "err", err.Error())
		return
	}

	rows, err := m.queries.ListGitHubPRRowsByAddress(ctx, db.ListGitHubPRRowsByAddressParams{
		InstallationID: addr.InstallationID,
		RepoOwner:      addr.Owner,
		RepoName:       addr.Repo,
		PrNumber:       addr.Number,
	})
	if err != nil {
		slog.Warn("ghsnapshot: list rows failed", "err", err.Error())
		return
	}

	anyApplied := false
	anyOpenApplied := false
	for _, row := range rows {
		applied, err := m.applySnapshot(ctx, row.ID, snap)
		if err != nil {
			slog.Warn("ghsnapshot: apply snapshot failed", "err", err.Error())
			continue
		}
		if !applied {
			continue
		}
		anyApplied = true
		if row.State == "open" || row.State == "draft" {
			anyOpenApplied = true
		}
		if m.onApplied != nil {
			m.onApplied(ctx, row.ID)
		}
	}

	// Chase decision. Chase only while the snapshot is undecided AND we still
	// have an open PR row on this head. If nothing applied (head advanced past
	// this response, or the PR is gone), the webhook that moved the head has
	// already enqueued the fresh head, so we stop here.
	if anyApplied && anyOpenApplied && !snap.Decided() {
		m.scheduleChase(addr)
	} else {
		m.mu.Lock()
		delete(m.attempts, addr)
		m.mu.Unlock()
	}
}

func (m *Manager) rateLimitPause(installationID int64) time.Duration {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.rateUntil[installationID].Sub(m.now())
}

func (m *Manager) extendRateLimit(installationID int64, retryAfter time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()
	until := m.now().Add(retryAfter)
	if until.After(m.rateUntil[installationID]) {
		m.rateUntil[installationID] = until
	}
}

// finish releases an address after a worker completes it, or turns the single
// coalesced trailing edge into the next queued refresh without ever allowing
// two workers to own the same address concurrently.
func (m *Manager) finish(addr address) {
	m.mu.Lock()
	if !m.trailing[addr] {
		delete(m.active, addr)
		delete(m.inFlight, addr)
		m.mu.Unlock()
		return
	}
	delete(m.trailing, addr)
	delete(m.inFlight, addr)
	m.mu.Unlock()

	select {
	case m.queue <- addr:
	default:
		// The worker just consumed one slot, so saturation is unlikely, but keep
		// the webhook path non-blocking and let the TTL sweep recover.
		m.mu.Lock()
		delete(m.active, addr)
		delete(m.inFlight, addr)
		delete(m.trailing, addr)
		m.mu.Unlock()
		slog.Warn("ghsnapshot: refresh queue full, dropping trailing enqueue")
	}
}

// applySnapshot performs the head-SHA-guarded atomic batch replace for one PR
// row: guarded UPDATE of the snapshot columns, then a full DELETE + INSERT of
// the per-check rows — all in one transaction. Returns applied=false (and
// writes nothing) when the row's head has advanced past the snapshot's head
// (acceptance criterion 1).
func (m *Manager) applySnapshot(ctx context.Context, prID pgtype.UUID, snap *PRSnapshot) (bool, error) {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx)
	q := m.queries.WithTx(tx)

	rollup := pgtype.Text{}
	if snap.HasChecks {
		rollup = textOrNull(snap.RollupState)
	}
	n, err := q.UpdateGitHubPRSnapshot(ctx, db.UpdateGitHubPRSnapshotParams{
		ApiMergeable:        textOrNull(snap.Mergeable),
		ApiMergeStateStatus: textOrNull(snap.MergeStateStatus),
		ChecksRollupState:   rollup,
		HeadSha:             snap.HeadSHA,
		FetchedAt:           tsFromTime(m.now()),
		PrID:                prID,
	})
	if err != nil {
		return false, err
	}
	if n == 0 {
		// Head advanced — discard the entire response, including the per-check
		// rows. Nothing is written.
		return false, nil
	}
	if err := q.DeleteGitHubPRCheckRuns(ctx, prID); err != nil {
		return false, err
	}
	for i, c := range snap.Contexts {
		if err := q.InsertGitHubPRCheckRun(ctx, db.InsertGitHubPRCheckRunParams{
			PrID:            prID,
			HeadSha:         snap.HeadSHA,
			Ordinal:         int32(i),
			Name:            c.Name,
			Status:          c.Status,
			Conclusion:      textOrNull(c.Conclusion),
			DetailsUrl:      textOrNull(c.DetailsURL),
			IsStatusContext: c.IsStatusContext,
		}); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, err
	}
	return true, nil
}

// scheduleChase re-enqueues the address after the current backoff step. Bounded
// by maxChaseAttempts so an endlessly-pending CI or a wedged mergeability
// verdict can never spin forever (acceptance criterion 7).
func (m *Manager) scheduleChase(addr address) {
	m.mu.Lock()
	attempt := m.attempts[addr]
	if attempt >= maxChaseAttempts {
		delete(m.attempts, addr)
		m.mu.Unlock()
		return
	}
	m.attempts[addr] = attempt + 1
	idx := attempt
	if idx >= len(m.chaseBackoff) {
		idx = len(m.chaseBackoff) - 1
	}
	delay := m.chaseBackoff[idx]
	m.mu.Unlock()
	m.scheduleRetry(addr, delay)
}

// scheduleRetry re-enqueues the address after delay, unless the manager is
// shutting down.
func (m *Manager) scheduleRetry(addr address, delay time.Duration) {
	ctx := m.ctx
	time.AfterFunc(delay, func() {
		if ctx != nil && ctx.Err() != nil {
			return
		}
		m.Enqueue(addr.InstallationID, addr.Owner, addr.Repo, addr.Number)
	})
}

func (m *Manager) sweepLoop(ctx context.Context) {
	ticker := time.NewTicker(m.sweepInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			m.sweepOnce(ctx)
		}
	}
}

// sweepOnce enqueues a refresh for every open PR whose snapshot is missing or
// older than the sweep TTL. Bounded by sweepMaxRows. This is the safety net for
// base-branch changes that produce no pull_request webhook on this PR, and for
// any webhook that was dropped during a deploy.
func (m *Manager) sweepOnce(ctx context.Context) {
	rows, err := m.queries.ListStaleOpenGitHubPRs(ctx, db.ListStaleOpenGitHubPRsParams{
		OlderThan: tsFromTime(m.now().Add(-m.sweepTTL)),
		MaxRows:   m.sweepMaxRows,
	})
	if err != nil {
		slog.Warn("ghsnapshot: sweep query failed", "err", err.Error())
		return
	}
	for _, r := range rows {
		m.Enqueue(r.InstallationID, r.RepoOwner, r.RepoName, r.PrNumber)
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func asRateLimit(err error, target **RateLimitError) bool {
	if rl, ok := err.(*RateLimitError); ok {
		*target = rl
		return true
	}
	return false
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func tsFromTime(t time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: t, Valid: true}
}

// sleepCtx sleeps for d or until ctx is cancelled; returns false if cancelled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
