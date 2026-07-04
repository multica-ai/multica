package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	cerebrochannels "github.com/multica-ai/multica/server/internal/cerebro/channels"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// setChannelMentionGuard binds the notification listener's
// filterChannelMentionRecipients hook (declared in notification_listeners.go)
// to the real cerebro guard, closing over the cerebro + main query handles.
// Called from the router once cerebroQueries exists. FIR-2680.
//
// A router built without a DB pool (DB-less unit-test routers, e.g.
// NewRouter(nil, …)) must NOT wire the DB-backed guard: the hook is a
// process-global, so a nil-pool binding leaks into every later test in the
// package, and GetIssue would then panic on the nil pool inside the
// synchronous event bus. When pool is nil, reset to the identity hook so a
// DB-less router deterministically leaves mention routing untouched.
func setChannelMentionGuard(pool *pgxpool.Pool, cerebroQueries *cerebrodb.Queries, queries *db.Queries) {
	if pool == nil {
		filterChannelMentionRecipients = func(_ context.Context, _ events.Event, _ string, ids map[string]bool) map[string]bool {
			return ids
		}
		return
	}
	filterChannelMentionRecipients = func(ctx context.Context, e events.Event, issueID string, ids map[string]bool) map[string]bool {
		// parseUUID panics on a non-UUID; the notify path always carries a real
		// issue id, but guard the empty case so a malformed event never crashes
		// the synchronous bus dispatch.
		if issueID == "" {
			return ids
		}
		return cerebrochannels.FilterChannelMentionRecipients(
			ctx, queries, cerebroQueries, parseUUID(issueID), e.ActorType, e.ActorID, ids,
		)
	}
}
