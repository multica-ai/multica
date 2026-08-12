package main

import (
	"context"
	"log/slog"
)

// Domain event retention (MUL-4332 §4.1 / §9, TTL = 90 days) is still an explicit
// no-op. This slice ships the matcher and executor, so hook_execution now exists and
// events do reach `dispatched` — but the correct predicate is "dispatched AND older
// than the TTL AND every related hook_execution is terminal", and the sweeper for it
// (with its concurrent-sweep tests) is scheduled with the remaining action slices.
// Shipping a weaker "dispatched + TTL" sweep now would risk reclaiming the audit
// source of an execution that is still retrying.
//
// Until then `domain_event` only shrinks when a workspace is deleted, so the table
// grows with activity. That is bounded in practice — one row per real domain fact,
// not per request — but it is the thing to watch after this lands.
//
// The worker is kept wired (rather than silently omitted) so the intent is visible at
// the call site: it logs once and then idles until shutdown, doing no deletes.
func runDomainEventRetention(ctx context.Context) {
	slog.Info("domain event retention: no-op, pending the terminal-execution predicate (TTL=90d)")
	<-ctx.Done()
}
