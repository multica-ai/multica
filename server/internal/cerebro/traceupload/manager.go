package traceupload

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

// queueCapacity bounds the in-memory backlog of keys waiting for a worker.
// Overflow is harmless: the entry is already durable in the ledger, so the
// periodic sweep re-enqueues it.
const queueCapacity = 512

// defaultSweepInterval is how often the Manager re-scans the ledger for
// pending entries that fell out of the in-memory queue (overflow, lost retry
// timers across a restart). The boot-sweep runs once at Start; this is the
// recurring safety net.
const defaultSweepInterval = 5 * time.Minute

// Manager owns the ledger, the upload worker pool and the retry/sweep loops.
// A nil *Manager is a valid "disabled" value: every method is a safe no-op, so
// the daemon can hold a nil Manager when the flag is off and call Enqueue
// unconditionally.
type Manager struct {
	cfg      Config
	ledger   *Ledger
	uploader Uploader
	metrics  *Metrics
	logger   *slog.Logger
	now      func() time.Time

	queue chan string

	wsSemMu sync.Mutex
	wsSem   map[string]chan struct{}

	workers    int
	sweepEvery time.Duration
}

// NewManager builds a Manager from cfg. It returns (nil, nil) when the feature
// flag is off — the disabled case the daemon treats as "do nothing". When the
// flag is on but the endpoint or API key is missing it returns an error so the
// daemon can log a clear misconfiguration warning and continue without
// uploading (never a startup failure).
func NewManager(cfg Config, logger *slog.Logger) (*Manager, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cfg.Endpoint == "" || cfg.APIKey == "" {
		return nil, fmt.Errorf("trace upload enabled but %s / %s is empty", EnvEndpoint, EnvAPIKey)
	}
	if logger == nil {
		logger = slog.Default()
	}
	ledgerPath := ""
	if cfg.StateDir != "" {
		ledgerPath = cfg.StateDir + string(os.PathSeparator) + "ledger.json"
	}
	ledger, err := LoadLedger(ledgerPath)
	if err != nil {
		// A corrupt ledger must not brick uploads: log and start fresh.
		logger.Warn("trace upload: ledger unreadable, starting empty", "path", ledgerPath, "error", err)
		ledger = &Ledger{path: ledgerPath, entries: map[string]*Entry{}}
	}
	uploader := &HTTPUploader{
		Endpoint: cfg.Endpoint,
		APIKey:   cfg.APIKey,
		Client:   &http.Client{Timeout: 60 * time.Second},
	}
	return newManagerWith(cfg, ledger, uploader, logger), nil
}

// newManagerWith is the shared constructor used by NewManager and tests.
func newManagerWith(cfg Config, ledger *Ledger, uploader Uploader, logger *slog.Logger) *Manager {
	if cfg.MaxConcurrentPerWorkspace <= 0 {
		cfg.MaxConcurrentPerWorkspace = DefaultMaxConcurrentPerWorkspace
	}
	if cfg.MaxAge <= 0 {
		cfg.MaxAge = DefaultMaxAge
	}
	if len(cfg.Backoff) == 0 {
		cfg.Backoff = DefaultBackoff
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		cfg:        cfg,
		ledger:     ledger,
		uploader:   uploader,
		metrics:    &Metrics{},
		logger:     logger,
		now:        time.Now,
		queue:      make(chan string, queueCapacity),
		wsSem:      make(map[string]chan struct{}),
		workers:    4,
		sweepEvery: defaultSweepInterval,
	}
}

// Metrics returns the live counter set (nil-safe).
func (m *Manager) Metrics() Snapshot {
	if m == nil {
		return Snapshot{}
	}
	return m.metrics.Snapshot()
}

// Enqueue records a freshly-closed task's transcript and queues it for upload.
// It is best-effort and MUST NOT fail the task: all errors are returned only so
// the caller can log them, never to abort. A nil Manager (flag off) is a no-op
// and performs zero work — no disk access, no network.
func (m *Manager) Enqueue(s Sidecar, jsonlPath string) error {
	if m == nil {
		return nil
	}
	if !s.Valid() {
		return fmt.Errorf("trace upload: incomplete sidecar (task=%q session=%q runtime=%q), skipping", s.TaskID, s.SessionID, s.Runtime)
	}
	sha, size, err := hashFile(jsonlPath)
	if err != nil {
		return fmt.Errorf("trace upload: hash %s: %w", jsonlPath, err)
	}
	entry, err := m.ledger.Upsert(s, jsonlPath, sha, size, m.now())
	if err != nil {
		return fmt.Errorf("trace upload: ledger upsert: %w", err)
	}
	if entry.Status == StatusIngested || entry.Status == StatusDeadLetter {
		return nil // already settled; nothing to do
	}
	m.enqueueKey(entry.Key)
	return nil
}

// enqueueKey pushes a key without blocking. On overflow the entry stays pending
// in the ledger and the periodic sweep will pick it up.
func (m *Manager) enqueueKey(key string) {
	select {
	case m.queue <- key:
	default:
		m.logger.Debug("trace upload: queue full, deferring to sweep", "key", key)
	}
}

// Start launches the worker pool, the recurring sweep, and an immediate
// boot-sweep that re-enqueues every pending ledger entry (F2.3). It returns
// after spawning the goroutines; they run until ctx is cancelled. A nil
// Manager is a no-op.
func (m *Manager) Start(ctx context.Context) {
	if m == nil {
		return
	}
	for i := 0; i < m.workers; i++ {
		go m.worker(ctx)
	}
	go m.sweepLoop(ctx)

	pending := m.ledger.Pending()
	m.logger.Info("trace upload: boot-sweep", "pending", len(pending), "endpoint", m.cfg.Endpoint)
	for _, e := range pending {
		m.enqueueKey(e.Key)
	}
}

func (m *Manager) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case key := <-m.queue:
			outcome := m.processOnce(ctx, key)
			if outcome == outcomeRetry {
				m.scheduleRetry(ctx, key)
			}
		}
	}
}

func (m *Manager) sweepLoop(ctx context.Context) {
	t := time.NewTicker(m.sweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			for _, e := range m.ledger.Pending() {
				m.enqueueKey(e.Key)
			}
		}
	}
}

// scheduleRetry re-enqueues key after the backoff for its current attempt
// count, unless ctx is cancelled first.
func (m *Manager) scheduleRetry(ctx context.Context, key string) {
	entry := m.ledger.Get(key)
	if entry == nil {
		return
	}
	delay := m.backoffFor(entry.Attempts)
	go func() {
		t := time.NewTimer(delay)
		defer t.Stop()
		select {
		case <-ctx.Done():
		case <-t.C:
			m.enqueueKey(key)
		}
	}()
}

// backoffFor returns the wait before the next attempt. attempts is the number
// of failures so far; attempt 1 uses Backoff[0], capped at the last bucket.
func (m *Manager) backoffFor(attempts int) time.Duration {
	if attempts <= 0 {
		return m.cfg.Backoff[0]
	}
	idx := attempts - 1
	if idx >= len(m.cfg.Backoff) {
		idx = len(m.cfg.Backoff) - 1
	}
	return m.cfg.Backoff[idx]
}

type attemptOutcome int

const (
	outcomeSkip attemptOutcome = iota // not pending / not found / already settled
	outcomeIngested
	outcomeRetry
	outcomeDeadLetter
)

// processOnce performs exactly one upload attempt for key and updates the
// ledger + metrics. It is the deterministic, side-effect-contained unit the
// tests drive directly. It never returns an error: every failure mode is
// folded into the ledger state and the returned outcome.
func (m *Manager) processOnce(ctx context.Context, key string) attemptOutcome {
	entry := m.ledger.Get(key)
	if entry == nil || entry.Status != StatusPending {
		return outcomeSkip
	}
	now := m.now()

	// Dead-letter check: outlived MaxAge without success (F2.6).
	if now.Sub(entry.FirstSeen) > m.cfg.MaxAge {
		_ = m.ledger.MarkDeadLetter(key, now, "exceeded max age")
		m.metrics.incDeadLetter()
		m.logger.Error("trace upload: DEAD LETTER — giving up after max age",
			"key", key, "attempts", entry.Attempts, "first_seen", entry.FirstSeen,
			"max_age", m.cfg.MaxAge, "last_error", entry.LastError)
		return outcomeDeadLetter
	}

	jsonl, err := os.ReadFile(entry.FilePath)
	if err != nil {
		m.metrics.incFailed()
		_ = m.ledger.MarkFailed(key, now, fmt.Sprintf("read transcript: %v", err))
		m.logger.Warn("trace upload: cannot read transcript", "key", key, "path", entry.FilePath, "error", err)
		return outcomeRetry
	}

	// Claude-resume delta: if this session has already had its first N bytes
	// confirmed ingested under a prior task, only ship the suffix [N:]. The
	// meta-line still tags the new bytes with the CURRENT task_id, so the
	// registry attributes the new events correctly without double-counting the
	// prior ones. Fall back to a full upload if the prefix bytes diverge (file
	// truncated/rewritten) or the recorded watermark exceeds current size —
	// for codex (new rollout file per task under a reused session_id) the
	// fallback is the correct path.
	fullSize := int64(len(jsonl))
	body := jsonl
	prevState := m.ledger.SessionState(entry.Sidecar.SessionID)
	if prevState != nil && prevState.LastUploadedSize > 0 && prevState.LastUploadedSize <= fullSize {
		prefixSHA := sha256.Sum256(jsonl[:prevState.LastUploadedSize])
		if hex.EncodeToString(prefixSHA[:]) == prevState.LastUploadedSHA256 {
			body = jsonl[prevState.LastUploadedSize:]
			if len(body) == 0 {
				// Resumed task closed without producing new events — settle
				// the ledger row as ingested without a redundant network call.
				_ = m.ledger.MarkIngested(key, m.now())
				m.metrics.incSuccess()
				m.logger.Debug("trace upload: no new events since prior upload, skipping",
					"key", key, "session_id", entry.Sidecar.SessionID,
					"prev_size", prevState.LastUploadedSize)
				return outcomeIngested
			}
			m.logger.Debug("trace upload: sending delta",
				"key", key, "session_id", entry.Sidecar.SessionID,
				"prev_size", prevState.LastUploadedSize, "delta_bytes", len(body))
		} else {
			m.logger.Warn("trace upload: session prefix hash diverged, sending full file",
				"key", key, "session_id", entry.Sidecar.SessionID,
				"prev_size", prevState.LastUploadedSize, "full_size", fullSize)
		}
	}

	// Per-workspace rate limit (F2.9).
	release := m.acquire(ctx, entry.Sidecar.WorkspaceID)
	if release == nil {
		return outcomeRetry // ctx cancelled while waiting
	}
	res, err := m.uploader.Upload(ctx, entry.Sidecar, body)
	release()

	if err != nil {
		m.metrics.incFailed()
		_ = m.ledger.MarkFailed(key, m.now(), err.Error())
		m.logger.Warn("trace upload: attempt failed", "key", key, "attempts", entry.Attempts+1,
			"retryable", IsRetryable(err), "error", err)
		return outcomeRetry
	}

	_ = m.ledger.MarkIngested(key, m.now())
	// Advance the session watermark to the FULL current file size + full-file
	// sha so the next task on this session_id picks up from here. We store the
	// full hash (not the prefix hash) so a later prefix check against this
	// watermark is computed over the exact bytes we just confirmed delivered.
	fullSHA := sha256.Sum256(jsonl)
	_ = m.ledger.MarkSessionUploaded(entry.Sidecar.SessionID, fullSize, hex.EncodeToString(fullSHA[:]), m.now())
	m.metrics.incSuccess()
	m.logger.Debug("trace upload: ingested", "key", key, "status", res.Status, "events", res.EventCount)
	return outcomeIngested
}

// acquire blocks until a per-workspace upload slot is free or ctx is cancelled.
// Returns a release func, or nil if ctx was cancelled.
func (m *Manager) acquire(ctx context.Context, workspaceID string) func() {
	m.wsSemMu.Lock()
	sem, ok := m.wsSem[workspaceID]
	if !ok {
		sem = make(chan struct{}, m.cfg.MaxConcurrentPerWorkspace)
		m.wsSem[workspaceID] = sem
	}
	m.wsSemMu.Unlock()

	select {
	case sem <- struct{}{}:
		return func() { <-sem }
	case <-ctx.Done():
		return nil
	}
}
