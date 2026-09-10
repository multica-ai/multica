package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/featureflags"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/plugincontract"
)

// Event-triggered hooks.
//
// The rule this file exists to keep: an event hook NEVER blocks the host. The
// event bus is synchronous — Bus.Publish runs its listeners inline, on the
// goroutine of whatever request produced the event — so a listener that dialled
// a third-party endpoint would put an outside server on the critical path of
// creating an issue. Everything here therefore hands off to a bounded worker
// pool and returns immediately.
//
// The same reasoning is why the agent execution path has no hook at all: a hook
// that must run before or after every agent turn is a third party holding the
// product's main loop open. Agents reach hooks in PR 4 by choosing to call one
// as a tool, which is a call they can decline.

const (
	// Queue depth. Full means events are arriving faster than endpoints can
	// answer; the overflow is dropped and counted rather than queued forever,
	// because an unbounded queue turns a slow plugin into a memory leak.
	dispatchQueueDepth = 512
	dispatchWorkers    = 4

	// How long a hook call stays on record. This table is operational telemetry,
	// not history: it answers "why is this endpoint failing right now", and the
	// circuit breaker and rate limiter only ever look minutes back. Keeping it
	// forever would grow an append-only table at the rate of the per-hook limit
	// — 120/minute/hook — for no reader.
	invocationRetention  = 7 * 24 * time.Hour
	invocationSweepEvery = time.Hour
)

// PluginEventDispatcher fans domain events out to the hooks that asked for them.
type PluginEventDispatcher struct {
	service *PluginService
	queue   chan dispatchJob
	wg      sync.WaitGroup
	stop    chan struct{}
	once    sync.Once

	// dropped counts events shed under backpressure, surfaced for triage.
	mu      sync.Mutex
	dropped int
}

type dispatchJob struct {
	installation db.PluginInstallation
	hook         plugincontract.Hook
	eventType    string
	issueID      pgtype.UUID
	payload      any
}

func NewPluginEventDispatcher(service *PluginService) *PluginEventDispatcher {
	dispatcher := &PluginEventDispatcher{
		service: service,
		queue:   make(chan dispatchJob, dispatchQueueDepth),
		stop:    make(chan struct{}),
	}
	for i := 0; i < dispatchWorkers; i++ {
		dispatcher.wg.Add(1)
		go dispatcher.work()
	}
	dispatcher.wg.Add(1)
	go dispatcher.sweepInvocations()
	return dispatcher
}

// sweepInvocations makes the table's "TTL-swept" description true.
//
// Nothing reads a row older than the breaker and rate-limit windows, both of
// which look minutes back, so without this the table only grows — at up to the
// per-hook limit of 120 rows a minute, per hook, forever.
//
// Runs on the dispatcher's own lifecycle because it is the same concern:
// bounded resources for something a third party's behaviour drives.
//
// The first sweep waits for the first tick rather than firing at construction.
// An earlier version swept immediately, on the theory that a deployment down for
// a week should not wait an hour — and it panicked in cmd/server's router test,
// where the dispatcher is built over a Queries whose pool was never opened. The
// benefit was one hour of retention on a cold start against a 7-day TTL; the
// cost was a goroutine touching the database before the process is known to have
// one.
func (d *PluginEventDispatcher) sweepInvocations() {
	defer d.wg.Done()
	ticker := time.NewTicker(invocationSweepEvery)
	defer ticker.Stop()
	for {
		select {
		case <-d.stop:
			return
		case <-ticker.C:
			d.sweepOnce()
		}
	}
}

// sweepOnce deletes what has aged out.
//
// Panics are contained for the same reason runGuarded exists: this runs on a
// bare goroutine, where an unrecovered panic is not a failed sweep but a dead
// process. A nil check on Queries is NOT enough to prevent one — sqlc's Queries
// wraps an executor, so a non-nil Queries over an unopened pool passes every
// check available here and then dereferences inside pgxpool. That is exactly how
// this took down cmd/server's test, and it is why the guard is a recover rather
// than one more nil comparison.
func (d *PluginEventDispatcher) sweepOnce() {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("plugins: panic while sweeping hook invocations", "recovered", recovered)
		}
	}()
	if d.service == nil || d.service.Queries == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cutoff := pgtype.Timestamptz{Time: time.Now().Add(-invocationRetention), Valid: true}
	removed, err := d.service.Queries.DeleteExpiredPluginInvocations(ctx, cutoff)
	if err != nil {
		slog.Warn("plugins: invocation sweep failed", "error", err)
		return
	}
	if removed > 0 {
		slog.Info("plugins: swept expired hook invocations", "removed", removed)
	}
}

// Dispatch is what the bus listener calls. It must return promptly: the caller
// is a live request that has already done its real work.
//
// Note it takes no context from that request. Tying an outbound hook to the
// request that triggered it would cancel the hook the moment the browser got
// its response, which is exactly when the hook is only just starting.
func (d *PluginEventDispatcher) Dispatch(eventType, workspaceID string, payload any) {
	if d == nil || d.service == nil || workspaceID == "" {
		return
	}
	parsedWorkspace, err := parseUUIDValue(workspaceID)
	if err != nil {
		return
	}

	// Nothing is inspected here. The installation lookup is a database read and
	// finding the issue id is a JSON round-trip; both belong on a worker, and
	// both sit behind the feature-flag check so a deployment with plugins off
	// pays for neither.
	select {
	case d.queue <- dispatchJob{
		installation: db.PluginInstallation{WorkspaceID: parsedWorkspace},
		eventType:    eventType,
		payload:      payload,
	}:
	default:
		d.mu.Lock()
		d.dropped++
		dropped := d.dropped
		d.mu.Unlock()
		slog.Warn("plugins: event dispatch queue full, dropping event", "event_type", eventType, "dropped_total", dropped)
	}
}

// Dropped reports how many events were shed under backpressure.
func (d *PluginEventDispatcher) Dropped() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

func (d *PluginEventDispatcher) work() {
	defer d.wg.Done()
	for {
		select {
		case <-d.stop:
			return
		case job := <-d.queue:
			d.runGuarded(job)
		}
	}
}

// runGuarded contains a panic to the delivery that caused it.
//
// Bus.Publish recovers panics in its listeners so one bad handler cannot take
// down the request that published. Moving delivery onto a worker goroutine
// steps outside that protection, and a panic on a bare goroutine is not a
// failed hook but a dead process. Restoring the guarantee is the cost of the
// hand-off, not an optional extra.
func (d *PluginEventDispatcher) runGuarded(job dispatchJob) {
	defer func() {
		if recovered := recover(); recovered != nil {
			slog.Error("plugins: panic while delivering an event hook",
				"event_type", job.eventType, "recovered", recovered)
		}
	}()
	d.run(job)
}

// run resolves which hooks want this event and calls each one.
func (d *PluginEventDispatcher) run(job dispatchJob) {
	if d.service.Queries == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	// The flag gates this path too, and checking it HERE rather than at
	// subscription is the point: a deployment that turns plugins off after
	// something was installed must stop the outbound calls, not just hide the
	// UI. Reading it per delivery means the flip takes effect immediately
	// instead of at the next restart.
	//
	// It also keeps the flag-off cost at zero. Without this every dispatched
	// event ran a ListWorkspacePluginInstallations query to discover there was
	// nothing to call.
	if !featureflags.PluginsV1Enabled(ctx, d.service.FeatureFlags) {
		return
	}

	// Only now, past the flag: the id is needed to narrow the callback grant,
	// and finding it means parsing the payload.
	job.issueID = issueIDFromPayload(job.payload)

	installations, err := d.service.Queries.ListWorkspacePluginInstallations(ctx, job.installation.WorkspaceID)
	if err != nil {
		slog.Warn("plugins: event dispatch could not list installations", "error", err)
		return
	}
	for _, installation := range installations {
		if !installation.Enabled {
			continue
		}
		manifest, err := ParseInstallationManifest(installation)
		if err != nil {
			continue
		}
		for _, hook := range manifest.Contributes.Hooks {
			if !HookAllowsTrigger(hook, plugincontract.TriggerEvent) || !hookWantsEvent(hook, job.eventType) {
				continue
			}
			d.deliver(ctx, installation, hook, job)
		}
	}
}

func hookWantsEvent(hook plugincontract.Hook, eventType string) bool {
	for _, declared := range hook.Events {
		if declared == eventType {
			return true
		}
	}
	return false
}

// deliver runs one hook with the event retry schedule.
func (d *PluginEventDispatcher) deliver(ctx context.Context, installation db.PluginInstallation, hook plugincontract.Hook, job dispatchJob) {
	// A hook whose endpoint has been failing is not retried on every event.
	// Without this, an endpoint that has been down for an hour receives one
	// doomed request per workspace event, forever.
	if d.service.HookBreakerOpen(ctx, installation.ID, hook.Key) {
		slog.Info("plugins: hook circuit open, skipping event", "hook", hook.Key, "event_type", job.eventType)
		return
	}

	invocation := HookInvocation{
		Installation: installation,
		Hook:         hook,
		Trigger:      plugincontract.TriggerEvent,
		EventType:    job.eventType,
		// An event has no person behind it. Writes it produces are the
		// plugin's own, attributed to the installation.
		Actor:   HookActor{Type: "plugin", ID: installation.ID},
		IssueID: job.issueID,
		Input:   job.payload,
	}

	for attempt := 1; attempt <= hookEventAttempts; attempt++ {
		_, err := d.service.InvokeHook(ctx, invocation, attempt)
		if err == nil {
			return
		}
		// A refusal is a decision, not an outage: retrying a hook that is
		// disabled, out of scope or rate limited just burns the budget.
		if hookFailureStatus(err) == "refused" {
			slog.Info("plugins: event hook refused", "hook", hook.Key, "error", redactHookError(err))
			return
		}
		if attempt == hookEventAttempts {
			slog.Warn("plugins: event hook failed after retries", "hook", hook.Key, "event_type", job.eventType, "error", redactHookError(err))
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * hookEventBackoff):
		}
	}
}

// Close stops the workers. Safe to call more than once.
func (d *PluginEventDispatcher) Close() {
	if d == nil {
		return
	}
	d.once.Do(func() {
		close(d.stop)
		d.wg.Wait()
	})
}
