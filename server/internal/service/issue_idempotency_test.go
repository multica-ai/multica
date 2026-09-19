package service

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestIssueCreateRequestReplaysOneIssueAndOneAssignedTask(t *testing.T) {
	pool := newResolveOriginatorPool(t)
	ctx := context.Background()
	queries := db.New(pool)
	workspaceID, userID, agentID, _ := seedAttributionFixture(t, pool)
	workspaceUUID := util.MustParseUUID(workspaceID)
	userUUID := util.MustParseUUID(userID)
	agentUUID := util.MustParseUUID(agentID)

	bus := events.New()
	taskService := &TaskService{Queries: queries, TxStarter: pool, Bus: bus}
	issueService := NewIssueService(queries, pool, bus, nil, taskService)
	params := IssueCreateParams{
		WorkspaceID:        workspaceUUID,
		Title:              "Stable LifeOS request",
		Status:             "todo",
		Priority:           "medium",
		AssigneeType:       pgtype.Text{String: "agent", Valid: true},
		AssigneeID:         agentUUID,
		CreatorType:        "member",
		CreatorID:          userUUID,
		AllowDuplicate:     true,
		RequestKey:         pgtype.Text{String: "lifeos-action:stable-request", Valid: true},
		RequestPayloadHash: pgtype.Text{String: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Valid: true},
	}

	first, err := issueService.Create(ctx, params, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if first.Replayed || !first.AssignedTaskID.Valid {
		t.Fatalf("first result = replayed %v task %v", first.Replayed, first.AssignedTaskID)
	}

	second, err := issueService.Create(ctx, params, IssueCreateOpts{})
	if err != nil {
		t.Fatalf("second Create: %v", err)
	}
	if !second.Replayed || second.Issue.ID != first.Issue.ID {
		t.Fatalf("second result = replayed %v issue %s; want original %s", second.Replayed, util.UUIDToString(second.Issue.ID), util.UUIDToString(first.Issue.ID))
	}

	var issueCount, taskCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM issue WHERE workspace_id=$1 AND title=$2`, workspaceUUID, params.Title).Scan(&issueCount); err != nil {
		t.Fatalf("count issues: %v", err)
	}
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM agent_task_queue WHERE issue_id=$1`, first.Issue.ID).Scan(&taskCount); err != nil {
		t.Fatalf("count tasks: %v", err)
	}
	if issueCount != 1 || taskCount != 1 {
		t.Fatalf("persisted issues=%d tasks=%d; want 1 and 1", issueCount, taskCount)
	}

	conflict := params
	conflict.RequestPayloadHash.String = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, err := issueService.Create(ctx, conflict, IssueCreateOpts{}); !errors.Is(err, ErrIssueCreateRequestConflict) {
		t.Fatalf("same key with different payload error = %v, want ErrIssueCreateRequestConflict", err)
	}
}
