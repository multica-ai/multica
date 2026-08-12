package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// resolveTaskWorkspaceForEvent must be fail-closed (MUL-4332 review point 4): a
// task with no resolvable attribution returns an invalid UUID, which the
// task-terminal callers treat as an error and roll the transition back rather
// than committing a fact with no event. It must also succeed via the agent
// fallback for a task that carries only an agent.
func TestResolveTaskWorkspaceForEventFailClosed(t *testing.T) {
	pool := newTaskClaimRacePool(t) // skips if no DB
	ctx := context.Background()
	queries := db.New(pool)
	svc := NewTaskService(queries, pool, nil, events.New())

	// Unresolvable: no issue / chat / autopilot / quick-create link and a bogus
	// agent id that matches no row → invalid workspace.
	orphan := db.AgentTaskQueue{
		ID:      pgtype.UUID{Bytes: uuid.New(), Valid: true},
		AgentID: pgtype.UUID{Bytes: uuid.New(), Valid: true},
	}
	if ws := svc.resolveTaskWorkspaceForEvent(ctx, queries, orphan); ws.Valid {
		t.Fatalf("expected unresolvable workspace to be invalid, got %s", util.UUIDToString(ws))
	}

	// Resolvable via the agent fallback: a task carrying a real agent resolves to
	// that agent's workspace.
	agentID := createClaimCapacityFixture(t, ctx, pool)
	task := db.AgentTaskQueue{
		ID:      pgtype.UUID{Bytes: uuid.New(), Valid: true},
		AgentID: util.MustParseUUID(agentID),
	}
	if ws := svc.resolveTaskWorkspaceForEvent(ctx, queries, task); !ws.Valid {
		t.Fatalf("expected agent fallback to resolve a workspace for agent %s", agentID)
	}
}
