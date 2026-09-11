package daemon

import (
	"context"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

// Per-provider concurrency limits.
//
// The daemon's global slot pool (MULTICA_DAEMON_MAX_CONCURRENT_TASKS) bounds
// how many tasks may be in flight at once but knows nothing about what each
// provider actually needs: a local GPU agent can run one job at a time, yet
// the global pool happily hands it ten. This file adds a per-provider token
// pool on top of the global slots. A claimed task whose provider is
// saturated parks in waitForProviderCapacity — still holding its global slot
// — until a token frees.
//
// Limits come from MULTICA_<PROVIDER>_MAX_CONCURRENT_TASKS, where the
// provider name is the runtime's provider field uppercased verbatim:
// "claude" -> MULTICA_CLAUDE_MAX_CONCURRENT_TASKS, "opencode" ->
// MULTICA_OPENCODE_MAX_CONCURRENT_TASKS. A custom runtime profile is capped
// through its protocol family's variable, not its display name. A provider
// with no variable set is unlimited.

// providerLimitEnvVar returns the environment variable that caps the
// concurrency for provider.
func providerLimitEnvVar(provider string) string {
	return "MULTICA_" + strings.ToUpper(strings.TrimSpace(provider)) + "_MAX_CONCURRENT_TASKS"
}

// providerPool returns the token pool for provider: a buffered channel of
// exactly limit capacity when the provider is capped, or nil when it is
// unlimited. The variable is read once per provider and memoized; the
// daemon's environment is fixed for the life of the process, so a memoized
// entry can never mask a changed value.
//
// The map is created on demand so test daemons built as bare literals share
// this path with New().
func (d *Daemon) providerPool(provider string) chan struct{} {
	d.providerCapMu.Lock()
	defer d.providerCapMu.Unlock()
	if d.providerCap == nil {
		d.providerCap = make(map[string]chan struct{})
	}
	if pool, ok := d.providerCap[provider]; ok {
		return pool
	}
	pool := d.buildProviderPool(provider)
	d.providerCap[provider] = pool
	return pool
}

// buildProviderPool parses the provider's limit variable. Invalid values are
// logged and treated as unlimited rather than failing the daemon: a typo in
// an optional knob must not take every task down with it.
func (d *Daemon) buildProviderPool(provider string) chan struct{} {
	varName := providerLimitEnvVar(provider)
	raw := strings.TrimSpace(os.Getenv(varName))
	if raw == "" {
		return nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit <= 0 {
		d.logger.Warn("ignoring invalid per-provider concurrency limit",
			"provider", provider, "var", varName, "value", raw)
		return nil
	}
	return make(chan struct{}, limit)
}

// waitForProviderCapacity blocks until a token from provider's pool is free
// and returns a function that releases it. A provider without a configured
// limit returns a no-op release immediately.
//
// While parked, the wait mirrors the local_directory wait:
//
//   - the task's prepare lease is extended on a fixed cadence, so the server
//     never reclaims the dispatched task and re-delivers it mid-wait;
//   - a cancellation watcher aborts the wait the moment the server moves the
//     task to a terminal state (or the row is deleted), in which case ok is
//     false and the task must not run;
//   - the parent ctx aborts the wait on daemon shutdown; the task is left
//     dispatched and the runtime-offline recovery path retries it, exactly
//     like any task that had not started when the daemon exited.
//
// ok is false — with a nil release — when the task must not run.
func (d *Daemon) waitForProviderCapacity(ctx context.Context, task Task, provider string, taskLog *slog.Logger) (release func(), ok bool) {
	pool := d.providerPool(provider)
	if pool == nil {
		return func() {}, true
	}

	waitCtx, waitCancel := context.WithCancel(ctx)
	defer waitCancel()

	stopLease := d.startTaskPrepareLeaseExtender(waitCtx, task, taskLog)
	defer stopLease()

	pollInterval := d.cancelPollInterval
	if pollInterval == 0 {
		pollInterval = 5 * time.Second
	}
	cancelledByPoll := d.watchTaskCancellation(waitCtx, task.ID, pollInterval, taskLog)
	go func() {
		select {
		case <-cancelledByPoll:
			waitCancel()
		case <-waitCtx.Done():
		}
	}()

	taskLog.Info("provider_capacity: waiting for capacity", "provider", provider, "var", providerLimitEnvVar(provider))
	select {
	case pool <- struct{}{}:
		taskLog.Info("provider_capacity: capacity acquired", "provider", provider)
		return func() { <-pool }, true
	case <-waitCtx.Done():
		select {
		case <-cancelledByPoll:
			taskLog.Info("provider_capacity: wait aborted by server-side terminal state")
		default:
			taskLog.Warn("provider_capacity: wait aborted by daemon shutdown")
		}
		return nil, false
	}
}
