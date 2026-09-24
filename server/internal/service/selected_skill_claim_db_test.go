package service

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/events"
	dbfx "github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestSelectedSkillClaimPersistsAcrossDescriptionMutation(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)
	taskID, userID, workspaceID := dispatchedCommentTaskFixture(t, ctx, pool)
	task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load dispatched task: %v", err)
	}
	fixture := dbfx.New(pool, workspaceID, userID)
	skillID := fixture.Insert(t, "skill", dbfx.Cols{
		"workspace_id": workspaceID, "name": "db-selected", "description": "test",
		"content": "body", "config": dbfx.Raw("'{}'::jsonb"), "created_by": userID,
	})
	svc := NewTaskService(queries, pool, nil, events.New())
	loaded, err := svc.LoadWorkspaceSkillsByIDs(ctx, util.MustParseUUID(workspaceID), []string{skillID})
	if err != nil || len(loaded) != 1 {
		t.Fatalf("same-workspace selected skill load = %d, err=%v", len(loaded), err)
	}
	wrongWorkspace := util.MustParseUUID("ffffffff-ffff-ffff-ffff-ffffffffffff")
	loaded, err = svc.LoadWorkspaceSkillsByIDs(ctx, wrongWorkspace, []string{skillID})
	if err != nil || len(loaded) != 0 {
		t.Fatalf("cross-workspace selected skill load = %d, err=%v", len(loaded), err)
	}
	selected := []pgtype.UUID{util.MustParseUUID(skillID)}
	_, err = svc.FinalizeTaskClaimWithSelectedSkills(ctx, task, db.CreateTaskTokenParams{
		TokenHash: fmt.Sprintf("selected-skill-%d", time.Now().UnixNano()), TaskID: task.ID,
		AgentID: task.AgentID, WorkspaceID: util.MustParseUUID(workspaceID), UserID: util.MustParseUUID(userID),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, nil, false, nil, nil, selected)
	if err != nil {
		t.Fatalf("finalize selected-skill claim: %v", err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("read task context: %v", err)
	}
	var contextValue struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(raw, &contextValue); err != nil || len(contextValue.SelectedSkillIDs) != 1 {
		t.Fatalf("task context selected_skill_ids = %s: %v", raw, err)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET description = 'mutated after claim' WHERE id = (SELECT issue_id FROM agent_task_queue WHERE id = $1)`, taskID); err != nil {
		t.Fatalf("mutate issue description: %v", err)
	}
	var after []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&after); err != nil {
		t.Fatalf("read context after description mutation: %v", err)
	}
	if string(after) != string(raw) {
		t.Fatalf("task grant changed after description mutation: before=%s after=%s", raw, after)
	}
}

func TestCreateEnqueueFreezesSelectionBeforeDescriptionMutation(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	fixture := dbfx.New(pool, workspaceID, userID)
	originalSkillID := fixture.Insert(t, "skill", dbfx.Cols{
		"workspace_id": workspaceID, "name": "original", "description": "test",
		"content": "body", "config": dbfx.Raw("'{}'::jsonb"), "created_by": userID,
	})
	initialDescription := fmt.Sprintf("use [/original](slash://skill/%s)", originalSkillID)
	if _, err := pool.Exec(ctx, `UPDATE issue SET description = $1 WHERE id = $2`, initialDescription, issueID); err != nil {
		t.Fatalf("set initial issue description: %v", err)
	}
	issue := db.Issue{ID: util.MustParseUUID(issueID), WorkspaceID: util.MustParseUUID(workspaceID), AssigneeID: util.MustParseUUID(agentID), AssigneeType: pgtype.Text{String: "agent", Valid: true}, CreatorType: "member", CreatorID: util.MustParseUUID(userID), Priority: "medium", Description: pgtype.Text{String: initialDescription, Valid: true}}
	initialSelected := selectedSkillIDsForMemberIssueCreate(issue)
	svc := &TaskService{Queries: db.New(pool), TxStarter: pool, Bus: events.New()}
	created, err := svc.EnqueueTaskForIssueWithSelectedSkills(ctx, issue, initialSelected)
	if err != nil {
		t.Fatalf("create assigned task: %v", err)
	}
	var beforeClaim []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, created.ID).Scan(&beforeClaim); err != nil {
		t.Fatalf("read create-time task context: %v", err)
	}
	var frozen struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(beforeClaim, &frozen); err != nil || len(frozen.SelectedSkillIDs) != 1 || frozen.SelectedSkillIDs[0] != originalSkillID {
		t.Fatalf("enqueue did not persist create-time selected_skill_ids: %s", beforeClaim)
	}
	if _, err := pool.Exec(ctx, `UPDATE issue SET description = 'replace with [/new](slash://skill/ffffffff-ffff-ffff-ffff-ffffffffffff)' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("mutate issue before claim: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`, created.ID); err != nil {
		t.Fatalf("dispatch created task: %v", err)
	}
	task, err := db.New(pool).GetAgentTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("reload dispatched task: %v", err)
	}
	var reloadedContext struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(task.Context, &reloadedContext); err != nil || len(reloadedContext.SelectedSkillIDs) != 1 || reloadedContext.SelectedSkillIDs[0] != originalSkillID {
		t.Fatalf("reloaded task lost create-time selection: %s", task.Context)
	}
	reloadedSelected := []pgtype.UUID{util.MustParseUUID(reloadedContext.SelectedSkillIDs[0])}
	_, err = svc.FinalizeTaskClaimWithSelectedSkills(ctx, task, db.CreateTaskTokenParams{
		TokenHash: fmt.Sprintf("create-freeze-%d", time.Now().UnixNano()), TaskID: task.ID, AgentID: task.AgentID,
		WorkspaceID: util.MustParseUUID(workspaceID), UserID: util.MustParseUUID(userID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, nil, false, nil, nil, reloadedSelected)
	if err != nil {
		t.Fatalf("claim frozen assigned task: %v", err)
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, created.ID).Scan(&raw); err != nil {
		t.Fatalf("read frozen task context: %v", err)
	}
	var stored struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil || len(stored.SelectedSkillIDs) != 1 || stored.SelectedSkillIDs[0] != originalSkillID {
		t.Fatalf("frozen selection = %s, want original skill only", raw)
	}
}

func TestSelectedSkillClaimFailureRollsBackGrantAndToken(t *testing.T) {
	ctx := context.Background()
	pool := newTaskClaimRacePool(t)
	queries := db.New(pool)
	taskID, userID, workspaceID := dispatchedCommentTaskFixture(t, ctx, pool)
	task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("load dispatched task: %v", err)
	}
	svc := NewTaskService(queries, pool, nil, events.New())
	_, err = svc.FinalizeTaskClaimWithSelectedSkills(ctx, task, db.CreateTaskTokenParams{
		TokenHash: fmt.Sprintf("selected-skill-fail-%d", time.Now().UnixNano()), TaskID: task.ID,
		AgentID: task.AgentID, WorkspaceID: util.MustParseUUID(workspaceID), UserID: util.MustParseUUID(userID),
		ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
	}, []pgtype.UUID{util.MustParseUUID("11111111-1111-1111-1111-111111111111")}, true, nil, nil,
		[]pgtype.UUID{util.MustParseUUID("00000000-0000-0000-0000-000000000002")})
	if err == nil {
		t.Fatal("expected invalid receipt to fail finalization")
	}
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("read rolled-back context: %v", err)
	}
	var rolledBack struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if len(raw) != 0 && string(raw) != "null" && json.Unmarshal(raw, &rolledBack) == nil && rolledBack.SelectedSkillIDs != nil {
		t.Fatalf("selected grant survived failed finalization: %s", raw)
	}
	var tokenCount int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM task_token WHERE task_id = $1`, taskID).Scan(&tokenCount); err != nil {
		t.Fatalf("count rolled-back tokens: %v", err)
	}
	if tokenCount != 0 {
		t.Fatalf("token count after failed finalization = %d, want 0", tokenCount)
	}
}

func TestSelectedSkillClaimPreservesLegacyContextShapes(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name       string
		input      string
		legacyKey  string
		wantObject bool
	}{
		{name: "null", input: "NULL", wantObject: true},
		{name: "object", input: "'{\"head_sha\":\"abc\"}'", legacyKey: "head_sha", wantObject: true},
		{name: "array", input: "'[\"legacy\"]'", legacyKey: "legacy_context", wantObject: true},
		{name: "scalar", input: "'\"legacy\"'", legacyKey: "legacy_context", wantObject: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pool := newTaskClaimRacePool(t)
			queries := db.New(pool)
			taskID, userID, workspaceID := dispatchedCommentTaskFixture(t, ctx, pool)
			if _, err := pool.Exec(ctx, "UPDATE agent_task_queue SET context = "+tc.input+"::jsonb WHERE id = $1", taskID); err != nil {
				t.Fatalf("set %s legacy context: %v", tc.name, err)
			}
			task, err := queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
			if err != nil {
				t.Fatalf("reload %s task: %v", tc.name, err)
			}
			selectedID := util.MustParseUUID("00000000-0000-0000-0000-000000000002")
			svc := NewTaskService(queries, pool, nil, events.New())
			_, err = svc.FinalizeTaskClaimWithSelectedSkills(ctx, task, db.CreateTaskTokenParams{
				TokenHash: fmt.Sprintf("context-shape-%s-%d", tc.name, time.Now().UnixNano()), TaskID: task.ID,
				AgentID: task.AgentID, WorkspaceID: util.MustParseUUID(workspaceID), UserID: util.MustParseUUID(userID),
				ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true},
			}, nil, false, nil, nil, []pgtype.UUID{selectedID})
			if err != nil {
				t.Fatalf("finalize %s context: %v", tc.name, err)
			}
			var raw []byte
			if err := pool.QueryRow(ctx, "SELECT context FROM agent_task_queue WHERE id = $1", taskID).Scan(&raw); err != nil {
				t.Fatalf("read %s context: %v", tc.name, err)
			}
			var got map[string]json.RawMessage
			if err := json.Unmarshal(raw, &got); err != nil || !tc.wantObject {
				t.Fatalf("%s context is not an object: %s (%v)", tc.name, raw, err)
			}
			if _, ok := got["selected_skill_ids"]; !ok {
				t.Fatalf("%s context lost selected_skill_ids: %s", tc.name, raw)
			}
			if tc.legacyKey != "" {
				if _, ok := got[tc.legacyKey]; !ok {
					t.Fatalf("%s context lost legacy field %q: %s", tc.name, tc.legacyKey, raw)
				}
			}
		})
	}
}

func TestDeferredAndSquadEnqueueFreezeSelectedSkillsBeforeClaim(t *testing.T) {
	ctx := context.Background()
	pool := newResolveOriginatorPool(t)
	queries := db.New(pool)
	svc := &TaskService{Queries: queries, TxStarter: pool, Bus: events.New()}

	workspaceID, userID, agentID, issueID := seedAttributionFixture(t, pool)
	fixture := dbfx.New(pool, workspaceID, userID)
	deferredSkillID := fixture.Insert(t, "skill", dbfx.Cols{
		"workspace_id": workspaceID, "name": "deferred-selected", "description": "test",
		"content": "body", "config": dbfx.Raw("'{}'::jsonb"), "created_by": userID,
	})
	deferredIssue := db.Issue{ID: util.MustParseUUID(issueID), WorkspaceID: util.MustParseUUID(workspaceID), AssigneeID: util.MustParseUUID(agentID), AssigneeType: pgtype.Text{String: "agent", Valid: true}, CreatorType: "member", CreatorID: util.MustParseUUID(userID), Priority: "medium", Description: pgtype.Text{String: "[/deferred](slash://skill/" + deferredSkillID + ")", Valid: true}}
	deferredSelected := []pgtype.UUID{util.MustParseUUID(deferredSkillID)}
	deferred, err := svc.EnqueueDeferredChannelIssueTaskWithSelectedSkills(ctx, deferredIssue, time.Now().Add(time.Hour), deferredSelected)
	if err != nil {
		t.Fatalf("enqueue deferred task: %v", err)
	}
	assertTaskContextSelected(t, ctx, pool, util.UUIDToString(deferred.ID), deferredSkillID)
	if _, err := pool.Exec(ctx, `UPDATE issue SET description = 'mutated before deferred claim' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("mutate deferred issue: %v", err)
	}
	if err := svc.PromoteDeferredChannelIssueTask(ctx, deferred.ID); err != nil {
		t.Fatalf("promote deferred task: %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1`, deferred.ID); err != nil {
		t.Fatalf("dispatch deferred task: %v", err)
	}
	claimTaskWithPersistedSelection(t, ctx, svc, pool, util.UUIDToString(deferred.ID), workspaceID, userID, deferredSkillID)

	workspaceID, userID, agentID, issueID = seedAttributionFixture(t, pool)
	fixture = dbfx.New(pool, workspaceID, userID)
	squadSkillID := fixture.Insert(t, "skill", dbfx.Cols{
		"workspace_id": workspaceID, "name": "squad-selected", "description": "test",
		"content": "body", "config": dbfx.Raw("'{}'::jsonb"), "created_by": userID,
	})
	squad, err := queries.CreateSquad(ctx, db.CreateSquadParams{WorkspaceID: util.MustParseUUID(workspaceID), Name: "selected-skill squad", Description: "test", LeaderID: util.MustParseUUID(agentID), CreatorID: util.MustParseUUID(userID)})
	if err != nil {
		t.Fatalf("create squad fixture: %v", err)
	}
	squadIssue := db.Issue{ID: util.MustParseUUID(issueID), WorkspaceID: util.MustParseUUID(workspaceID), AssigneeID: util.MustParseUUID(agentID), AssigneeType: pgtype.Text{String: "agent", Valid: true}, CreatorType: "member", CreatorID: util.MustParseUUID(userID), Priority: "medium", Description: pgtype.Text{String: "[/squad](slash://skill/" + squadSkillID + ")", Valid: true}}
	squadTask, err := svc.EnqueueTaskForSquadLeaderWithSelectedSkills(ctx, squadIssue, util.MustParseUUID(agentID), squad.ID, pgtype.UUID{}, []pgtype.UUID{util.MustParseUUID(squadSkillID)})
	if err != nil {
		t.Fatalf("enqueue squad task: %v", err)
	}
	assertTaskContextSelected(t, ctx, pool, util.UUIDToString(squadTask.ID), squadSkillID)
	if _, err := pool.Exec(ctx, `UPDATE issue SET description = 'mutated before squad claim' WHERE id = $1`, issueID); err != nil {
		t.Fatalf("mutate squad issue: %v", err)
	}
	claimTaskWithPersistedSelection(t, ctx, svc, pool, util.UUIDToString(squadTask.ID), workspaceID, userID, squadSkillID)

	// A later task gets a fresh context; the previous run's one-Run grant is
	// never copied implicitly.
	workspaceID, userID, agentID, issueID = seedAttributionFixture(t, pool)
	laterIssue := db.Issue{ID: util.MustParseUUID(issueID), WorkspaceID: util.MustParseUUID(workspaceID), AssigneeID: util.MustParseUUID(agentID), AssigneeType: pgtype.Text{String: "agent", Valid: true}, CreatorType: "member", CreatorID: util.MustParseUUID(userID), Priority: "medium"}
	later, err := svc.EnqueueTaskForIssue(ctx, laterIssue)
	if err != nil {
		t.Fatalf("enqueue later task: %v", err)
	}
	var laterContext []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, later.ID).Scan(&laterContext); err != nil {
		t.Fatalf("read later task context: %v", err)
	}
	var laterStored struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(laterContext, &laterStored); err == nil && len(laterStored.SelectedSkillIDs) != 0 {
		t.Fatalf("later task inherited prior selected grant: %s", laterContext)
	}
}

func assertTaskContextSelected(t *testing.T, ctx context.Context, pool *pgxpool.Pool, taskID, expected string) {
	t.Helper()
	var raw []byte
	if err := pool.QueryRow(ctx, `SELECT context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&raw); err != nil {
		t.Fatalf("read task context: %v", err)
	}
	var stored struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(raw, &stored); err != nil || len(stored.SelectedSkillIDs) != 1 || stored.SelectedSkillIDs[0] != expected {
		t.Fatalf("task context selected_skill_ids = %s, want %s", raw, expected)
	}
}

func claimTaskWithPersistedSelection(t *testing.T, ctx context.Context, svc *TaskService, pool *pgxpool.Pool, taskID, workspaceID, userID, expected string) {
	t.Helper()
	if _, err := pool.Exec(ctx, `UPDATE agent_task_queue SET status = 'dispatched', dispatched_at = now() WHERE id = $1 AND status = 'queued'`, taskID); err != nil {
		t.Fatalf("dispatch queued task: %v", err)
	}
	task, err := svc.Queries.GetAgentTask(ctx, util.MustParseUUID(taskID))
	if err != nil {
		t.Fatalf("reload task: %v", err)
	}
	var stored struct {
		SelectedSkillIDs []string `json:"selected_skill_ids"`
	}
	if err := json.Unmarshal(task.Context, &stored); err != nil || len(stored.SelectedSkillIDs) != 1 || stored.SelectedSkillIDs[0] != expected {
		t.Fatalf("reloaded task selection = %s, want %s", task.Context, expected)
	}
	_, err = svc.FinalizeTaskClaimWithSelectedSkills(ctx, task, db.CreateTaskTokenParams{TokenHash: fmt.Sprintf("freeze-%d", time.Now().UnixNano()), TaskID: task.ID, AgentID: task.AgentID, WorkspaceID: util.MustParseUUID(workspaceID), UserID: util.MustParseUUID(userID), ExpiresAt: pgtype.Timestamptz{Time: time.Now().Add(time.Hour), Valid: true}}, nil, false, nil, nil, []pgtype.UUID{util.MustParseUUID(stored.SelectedSkillIDs[0])})
	if err != nil {
		t.Fatalf("claim persisted selection: %v", err)
	}
}
