package main

import (
	"context"
	"log/slog"
)

// Domain event retention (MUL-4332 §4.1 / §9) is DELIBERATELY an explicit no-op
// in PR1. The correct retention predicate is "dispatched AND older than the TTL
// AND every related hook_execution is terminal" — but hook_execution does not
// exist until PR3, and PR1 has no dispatcher, so nothing is ever eligible for
// deletion. Shipping a weaker "dispatched + TTL" sweep now would, the moment PR3
// flips dispatching on, risk reclaiming events whose executions are still
// running or retrying (review point 5). Retention therefore lands in PR3
// alongside hook_execution, the full terminal predicate, and concurrent-sweeper
// tests.
//
// The worker is kept wired (rather than silently omitted) so the intent is
// visible at the call site: it logs once and then idles until shutdown, doing no
// deletes.
func runDomainEventRetention(ctx context.Context) {
	slog.Info("domain event retention: explicit no-op in PR1, deferred to PR3 (needs hook_execution terminal predicate)")
	<-ctx.Done()
}
