package handler

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type wakeupActorKey struct{}
type wakeupActor struct{ kind, id, task string }
type wakeupHandedToKey struct{}

// withWakeupHandedTo names the agent a write hands the issue to. That agent
// gets the change as its own run, so the write's issue events skip its event
// wakeups (migration 558, MUL-7420).
func withWakeupHandedTo(ctx context.Context, agentID string) context.Context {
	if agentID == "" {
		return ctx
	}
	return context.WithValue(ctx, wakeupHandedToKey{}, agentID)
}

// Carry only server-resolved request identity into transaction-local settings.
// In particular, mutation of another agent's comment must not reuse its author
// or original run as the source of the new event.
func (h *Handler) withWakeupActor(r *http.Request) *http.Request {
	if _, ok := r.Context().Value(wakeupActorKey{}).(wakeupActor); ok {
		return r
	}
	kind, id := h.resolveActor(r, requestUserID(r), h.resolveWorkspaceID(r))
	source := h.wakeupSourceTaskID(r)
	var task string
	if source.Valid {
		task = uuidToString(source)
	}
	return r.WithContext(context.WithValue(r.Context(), wakeupActorKey{}, wakeupActor{kind, id, task}))
}

func (h *Handler) beginWakeupWrite(ctx context.Context) (pgx.Tx, error) {
	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		return nil, err
	}
	if actor, ok := ctx.Value(wakeupActorKey{}).(wakeupActor); ok {
		_, err = tx.Exec(ctx, `SELECT set_config('multica.actor_type',$1,true),set_config('multica.actor_id',$2,true),set_config('multica.source_task_id',$3,true)`, actor.kind, actor.id, actor.task)
		if err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	if agent, ok := ctx.Value(wakeupHandedToKey{}).(string); ok {
		if _, err = tx.Exec(ctx, `SELECT set_config('multica.handed_to_agent',$1,true)`, agent); err != nil {
			_ = tx.Rollback(ctx)
			return nil, err
		}
	}
	return tx, nil
}

// Single-statement mutation paths use the same source attribution as existing
// multi-statement transactions. Settings cannot leak into the pooled connection.
func wakeupWrite[T any](h *Handler, r *http.Request, write func(*db.Queries) (T, error)) (T, error) {
	var zero T
	r = h.withWakeupActor(r)
	tx, err := h.beginWakeupWrite(r.Context())
	if err != nil {
		return zero, err
	}
	defer tx.Rollback(r.Context())
	result, err := write(h.Queries.WithTx(tx))
	if err != nil {
		return zero, err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return zero, err
	}
	return result, nil
}
