package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type failCommentBranchInsertTxStarter struct {
	inner txStarter
}

func (s failCommentBranchInsertTxStarter) Begin(ctx context.Context) (pgx.Tx, error) {
	tx, err := s.inner.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failCommentBranchInsertTx{Tx: tx}, nil
}

type failCommentBranchInsertTx struct {
	pgx.Tx
}

func (tx *failCommentBranchInsertTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	if strings.Contains(sql, "-- name: CreateDeferredCommentBranchTask") {
		return errorRow{err: pgx.ErrNoRows}
	}
	return tx.Tx.QueryRow(ctx, sql, args...)
}

type errorRow struct {
	err error
}

func (r errorRow) Scan(...any) error {
	return r.err
}

func TestCommentBranchRequestLockNamespace(t *testing.T) {
	requestID := pgtype.UUID{Bytes: uuid.MustParse("1b8f44b8-dc4c-47a5-89a0-b24ecee2821e"), Valid: true}
	const rawID = "1b8f44b8-dc4c-47a5-89a0-b24ecee2821e"
	if got := commentBranchRequestLockID(requestID); got != "request:"+rawID {
		t.Fatalf("request lock id = %q, want an isolated request namespace", got)
	}
	if commentBranchRequestLockID(requestID) == rawID {
		t.Fatal("request lock id collides with the issue/agent queue-lock namespace")
	}
}

func TestCommentBranchRejectsMachineCredentialActors(t *testing.T) {
	for _, actorSource := range []string{"task_token", "cloud_pat"} {
		t.Run(actorSource, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/api/comments/ignored/branch", nil)
			req.Header.Set("X-User-ID", uuid.New().String())
			req.Header.Set("X-Actor-Source", actorSource)

			(&Handler{}).CreateCommentBranch(w, req)

			if w.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want %d: %s", w.Code, http.StatusForbidden, w.Body.String())
			}
		})
	}
}

// TestCommentBranchRoutingRoleMatrix covers the de-duplicated equivalence
// classes from comment provenance × issue assignee × explicit target. The
// server must preserve routing precedence while every successful result stays
// a fresh direct run, including when the chosen agent leads a squad.
func TestCommentBranchRoutingRoleMatrix(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeIDs, agentIDs, squadIDs, issueIDs []string
	insertRuntime := func(label, status string, branchCapable bool) string {
		t.Helper()
		metadata := `{}`
		if branchCapable {
			metadata = `{"capabilities":["comment-branch-v1"]}`
		}
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (
				workspace_id, name, runtime_mode, provider, status, device_info,
				metadata, visibility, owner_id
			) VALUES ($1, $2, 'local', 'codex', $3, 'test', $4::jsonb, 'private', $5)
			RETURNING id
		`, testWorkspaceID, fmt.Sprintf("branch-matrix-%s-%d", label, suffix), status, metadata, testUserID).Scan(&id); err != nil {
			t.Fatalf("insert %s runtime: %v", label, err)
		}
		runtimeIDs = append(runtimeIDs, id)
		return id
	}
	insertAgent := func(label, runtimeID string, archived bool) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (
				workspace_id, name, description, runtime_mode, runtime_config,
				runtime_id, visibility, permission_mode, max_concurrent_tasks,
				owner_id, archived_at
			) VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4,
				CASE WHEN $5::boolean THEN now() ELSE NULL END)
			RETURNING id
		`, testWorkspaceID, fmt.Sprintf("branch-matrix-%s-%d", label, suffix), runtimeID, testUserID, archived).Scan(&id); err != nil {
			t.Fatalf("insert %s agent: %v", label, err)
		}
		agentIDs = append(agentIDs, id)
		return id
	}
	insertSquad := func(label, leaderID string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
			VALUES ($1, $2, '', $3, $4) RETURNING id
		`, testWorkspaceID, fmt.Sprintf("branch-matrix-%s-%d", label, suffix), leaderID, testUserID).Scan(&id); err != nil {
			t.Fatalf("insert %s squad: %v", label, err)
		}
		squadIDs = append(squadIDs, id)
		return id
	}

	onlineRuntimeID := insertRuntime("online", "online", true)
	offlineRuntimeID := insertRuntime("offline", "offline", true)
	blockedRuntimeID := insertRuntime("blocked", "offline", true)
	if _, err := testPool.Exec(ctx, `
		UPDATE agent_runtime
		SET metadata = '{"capabilities":["comment-branch-v1"],"offline_reason":{"code":"not_executable","detail":"test runtime cannot execute the agent CLI"}}'::jsonb
		WHERE id = $1
	`, blockedRuntimeID); err != nil {
		t.Fatalf("mark blocked runtime: %v", err)
	}
	unsupportedRuntimeID := insertRuntime("unsupported", "online", false)
	ordinaryAgentID := insertAgent("ordinary", onlineRuntimeID, false)
	secondAgentID := insertAgent("second", onlineRuntimeID, false)
	issueLeaderID := insertAgent("issue-leader", onlineRuntimeID, false)
	otherLeaderID := insertAgent("other-leader", onlineRuntimeID, false)
	offlineAgentID := insertAgent("offline", offlineRuntimeID, false)
	blockedAgentID := insertAgent("blocked", blockedRuntimeID, false)
	unsupportedAgentID := insertAgent("unsupported", unsupportedRuntimeID, false)
	archivedAgentID := insertAgent("archived", onlineRuntimeID, true)
	issueSquadID := insertSquad("issue", issueLeaderID)
	otherSquadID := insertSquad("other", otherLeaderID)
	offlineSquadID := insertSquad("offline", offlineAgentID)
	blockedSquadID := insertSquad("blocked", blockedAgentID)
	foreignIssueID := insertWorkflowTestIssue(t, "branch matrix foreign source", int(suffix%1_000_000)+9_000_000)
	issueIDs = append(issueIDs, foreignIssueID)

	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM squad WHERE id = ANY($1::uuid[])`, squadIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = ANY($1::uuid[])`, agentIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = ANY($1::uuid[])`, runtimeIDs)
	})

	type matrixCase struct {
		name, authorType, assigneeType, assigneeID string
		sourceAgentID, sourceIssueID               string
		sourceIsLeader, sourceIsBranch             bool
		sourceSquadID, explicitAgentID             string
		wantStatus                                 int
		wantAgentID                                string
		wantSourceTaskSet                          bool
	}
	cases := []matrixCase{
		{name: "automatic from agent assignee", assigneeType: "agent", assigneeID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "automatic from squad assignee", assigneeType: "squad", assigneeID: issueSquadID, wantStatus: http.StatusCreated, wantAgentID: issueLeaderID},
		{name: "automatic has no target for member assignee", assigneeType: "member", assigneeID: testUserID, wantStatus: http.StatusUnprocessableEntity},
		{name: "automatic has no target when unassigned", wantStatus: http.StatusUnprocessableEntity},
		{name: "explicit ordinary overrides squad assignee", assigneeType: "squad", assigneeID: issueSquadID, explicitAgentID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "explicit issue squad leader stays direct", assigneeType: "squad", assigneeID: issueSquadID, explicitAgentID: issueLeaderID, wantStatus: http.StatusCreated, wantAgentID: issueLeaderID},
		{name: "explicit other squad leader stays direct", assigneeType: "squad", assigneeID: issueSquadID, explicitAgentID: otherLeaderID, wantStatus: http.StatusCreated, wantAgentID: otherLeaderID},
		{name: "explicit target works for member assignee", assigneeType: "member", assigneeID: testUserID, explicitAgentID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "explicit target works when unassigned", explicitAgentID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "system comment uses explicit target", authorType: "system", explicitAgentID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "ordinary source overrides squad assignee", assigneeType: "squad", assigneeID: issueSquadID, sourceAgentID: secondAgentID, wantStatus: http.StatusCreated, wantAgentID: secondAgentID, wantSourceTaskSet: true},
		{name: "leader source becomes direct branch", assigneeType: "agent", assigneeID: ordinaryAgentID, sourceAgentID: issueLeaderID, sourceIsLeader: true, sourceSquadID: issueSquadID, wantStatus: http.StatusCreated, wantAgentID: issueLeaderID, wantSourceTaskSet: true},
		{name: "other leader source becomes direct branch", assigneeType: "squad", assigneeID: issueSquadID, sourceAgentID: otherLeaderID, sourceIsLeader: true, sourceSquadID: otherSquadID, wantStatus: http.StatusCreated, wantAgentID: otherLeaderID, wantSourceTaskSet: true},
		{name: "prior branch source keeps agent", assigneeType: "squad", assigneeID: issueSquadID, sourceAgentID: secondAgentID, sourceIsBranch: true, wantStatus: http.StatusCreated, wantAgentID: secondAgentID, wantSourceTaskSet: true},
		{name: "explicit target overrides leader source", assigneeType: "squad", assigneeID: issueSquadID, sourceAgentID: issueLeaderID, sourceIsLeader: true, sourceSquadID: issueSquadID, explicitAgentID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID, wantSourceTaskSet: true},
		{name: "foreign issue source is ignored", assigneeType: "agent", assigneeID: ordinaryAgentID, sourceAgentID: secondAgentID, sourceIssueID: foreignIssueID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "offline source stays selected and waits", assigneeType: "agent", assigneeID: ordinaryAgentID, sourceAgentID: offlineAgentID, wantStatus: http.StatusCreated, wantAgentID: offlineAgentID, wantSourceTaskSet: true},
		{name: "blocked source does not fall back", assigneeType: "agent", assigneeID: ordinaryAgentID, sourceAgentID: blockedAgentID, wantStatus: http.StatusUnprocessableEntity, wantSourceTaskSet: true},
		{name: "unsupported source does not fall back", assigneeType: "agent", assigneeID: ordinaryAgentID, sourceAgentID: unsupportedAgentID, wantStatus: http.StatusUnprocessableEntity, wantSourceTaskSet: true},
		{name: "archived source does not fall back", assigneeType: "agent", assigneeID: ordinaryAgentID, sourceAgentID: archivedAgentID, wantStatus: http.StatusUnprocessableEntity, wantSourceTaskSet: true},
		{name: "offline squad leader stays selected and waits", assigneeType: "squad", assigneeID: offlineSquadID, wantStatus: http.StatusCreated, wantAgentID: offlineAgentID},
		{name: "blocked squad leader does not fall back", assigneeType: "squad", assigneeID: blockedSquadID, wantStatus: http.StatusUnprocessableEntity},
		{name: "explicit ready target overrides offline leader", assigneeType: "squad", assigneeID: offlineSquadID, explicitAgentID: ordinaryAgentID, wantStatus: http.StatusCreated, wantAgentID: ordinaryAgentID},
		{name: "explicit offline target stays selected and waits", assigneeType: "agent", assigneeID: ordinaryAgentID, explicitAgentID: offlineAgentID, wantStatus: http.StatusCreated, wantAgentID: offlineAgentID},
		{name: "explicit blocked target does not fall back", assigneeType: "agent", assigneeID: ordinaryAgentID, explicitAgentID: blockedAgentID, wantStatus: http.StatusUnprocessableEntity},
	}

	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			issueID := insertWorkflowTestIssue(t, "branch matrix "+tc.name, int(suffix%1_000_000)+9_100_000+i)
			issueIDs = append(issueIDs, issueID)
			if tc.assigneeType != "" {
				if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = $2, assignee_id = $3 WHERE id = $1`, issueID, tc.assigneeType, tc.assigneeID); err != nil {
					t.Fatalf("assign issue: %v", err)
				}
			}

			var sourceTaskID any
			var sourceTaskIDText string
			if tc.sourceAgentID != "" {
				sourceIssueID := issueID
				if tc.sourceIssueID != "" {
					sourceIssueID = tc.sourceIssueID
				}
				var squadArg any
				if tc.sourceSquadID != "" {
					squadArg = tc.sourceSquadID
				}
				var branchContext any
				if tc.sourceIsBranch {
					branchContext = `{"version":1}`
				}
				if err := testPool.QueryRow(ctx, `
					INSERT INTO agent_task_queue (
						agent_id, runtime_id, issue_id, status, priority, completed_at,
						is_leader_task, squad_id, branch_context
					) VALUES ($1, (SELECT runtime_id FROM agent WHERE id = $1), $2, 'completed', 0, now(), $3, $4, $5::jsonb)
					RETURNING id
				`, tc.sourceAgentID, sourceIssueID, tc.sourceIsLeader, squadArg, branchContext).Scan(&sourceTaskIDText); err != nil {
					t.Fatalf("insert source task: %v", err)
				}
				sourceTaskID = sourceTaskIDText
			}

			authorType := tc.authorType
			if authorType == "" {
				authorType = "member"
			}
			authorID := testUserID
			if tc.sourceAgentID != "" {
				authorType, authorID = "agent", tc.sourceAgentID
			}
			var commentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, source_task_id)
				VALUES ($1, $2, $3, $4, $5, $6) RETURNING id
			`, issueID, testWorkspaceID, authorType, authorID, tc.name, sourceTaskID).Scan(&commentID); err != nil {
				t.Fatalf("insert selected comment: %v", err)
			}

			body := map[string]any{"content_base": tc.name}
			if tc.explicitAgentID != "" {
				body["agent_id"] = tc.explicitAgentID
			}
			requestID := uuid.New().String()
			w := httptest.NewRecorder()
			req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/branch", body), "commentId", commentID)
			req.Header.Set("Idempotency-Key", requestID)
			testHandler.CreateCommentBranch(w, req)
			if w.Code != tc.wantStatus {
				t.Fatalf("create branch = %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}

			if tc.wantStatus != http.StatusCreated {
				var count int
				if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND branch_request_id = $2`, issueID, requestID).Scan(&count); err != nil {
					t.Fatalf("count rejected branch tasks: %v", err)
				}
				if count != 0 {
					t.Fatalf("rejected branch created %d task(s), want 0", count)
				}
				return
			}

			var taskID string
			if err := testPool.QueryRow(ctx, `SELECT id FROM agent_task_queue WHERE issue_id = $1 AND branch_request_id = $2`, issueID, requestID).Scan(&taskID); err != nil {
				t.Fatalf("load branch task id: %v", err)
			}
			task, err := testHandler.Queries.GetAgentTask(ctx, pgtype.UUID{Bytes: uuid.MustParse(taskID), Valid: true})
			if err != nil {
				t.Fatalf("load branch task: %v", err)
			}
			if got := uuidToString(task.AgentID); got != tc.wantAgentID {
				t.Fatalf("branch target = %s, want %s", got, tc.wantAgentID)
			}
			if task.IsLeaderTask || task.SquadID.Valid {
				t.Fatalf("branch inherited squad role: is_leader_task=%v squad_id=%s", task.IsLeaderTask, uuidToString(task.SquadID))
			}
			if !task.ForceFreshSession || task.ParentTaskID.Valid || task.RerunOfTaskID.Valid || task.RetryOfTaskID.Valid {
				t.Fatalf("branch is not fresh: fresh=%v parent=%s rerun=%s retry=%s", task.ForceFreshSession, uuidToString(task.ParentTaskID), uuidToString(task.RerunOfTaskID), uuidToString(task.RetryOfTaskID))
			}
			if task.BranchSourceTaskID.Valid != tc.wantSourceTaskSet {
				t.Fatalf("branch source validity = %v, want %v", task.BranchSourceTaskID.Valid, tc.wantSourceTaskSet)
			}
			if tc.wantSourceTaskSet && uuidToString(task.BranchSourceTaskID) != sourceTaskIDText {
				t.Fatalf("branch source = %s, want %s", uuidToString(task.BranchSourceTaskID), sourceTaskIDText)
			}
			if !validCommentBranchSnapshot(task.BranchContext, task) {
				var snapshot commentBranchSnapshot
				_ = json.Unmarshal(task.BranchContext, &snapshot)
				t.Fatalf("created branch snapshot is invalid: %#v", snapshot)
			}
		})
	}
}

func TestCommentBranchSnapshotContainsOnlyRootToSelectedPath(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
		VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-tree-runtime-%d", suffix), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-tree-agent-%d", suffix), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	issueID := insertWorkflowTestIssue(t, "branch tree matrix", int(suffix%1_000_000)+9_200_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}

	insertComment := func(content string, parent any) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, parent_id)
			VALUES ($1, $2, 'member', $3, $4, $5) RETURNING id
		`, issueID, testWorkspaceID, testUserID, content, parent).Scan(&id); err != nil {
			t.Fatalf("insert %s: %v", content, err)
		}
		return id
	}
	rootID := insertComment("root", nil)
	middleID := insertComment("middle", rootID)
	selectedID := insertComment("selected", middleID)
	siblingID := insertComment("sibling", middleID)
	childID := insertComment("child", selectedID)

	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	for _, tc := range []struct {
		name, selectedID, content string
		wantPath                  []string
	}{
		{name: "root", selectedID: rootID, content: "root", wantPath: []string{rootID}},
		{name: "nested leaf", selectedID: childID, content: "child", wantPath: []string{rootID, middleID, selectedID, childID}},
		{name: "nested non-leaf", selectedID: selectedID, content: "selected", wantPath: []string{rootID, middleID, selectedID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			requestID := uuid.New().String()
			w := httptest.NewRecorder()
			req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+tc.selectedID+"/branch", map[string]any{"content_base": tc.content}), "commentId", tc.selectedID)
			req.Header.Set("Idempotency-Key", requestID)
			testHandler.CreateCommentBranch(w, req)
			if w.Code != http.StatusCreated {
				t.Fatalf("create branch = %d: %s", w.Code, w.Body.String())
			}
			var raw []byte
			if err := testPool.QueryRow(ctx, `SELECT branch_context FROM agent_task_queue WHERE issue_id = $1 AND branch_request_id = $2`, issueID, requestID).Scan(&raw); err != nil {
				t.Fatalf("load branch snapshot: %v", err)
			}
			var snapshot commentBranchSnapshot
			if err := json.Unmarshal(raw, &snapshot); err != nil {
				t.Fatalf("decode snapshot: %v", err)
			}
			if len(snapshot.Comments) != len(tc.wantPath) {
				t.Fatalf("snapshot length = %d, want %d: %#v", len(snapshot.Comments), len(tc.wantPath), snapshot.Comments)
			}
			for i, wantID := range tc.wantPath {
				if snapshot.Comments[i].ID != wantID {
					t.Fatalf("snapshot[%d] = %s, want %s", i, snapshot.Comments[i].ID, wantID)
				}
			}
			for _, comment := range snapshot.Comments {
				if comment.ID == siblingID || (tc.selectedID == selectedID && comment.ID == childID) {
					t.Fatalf("snapshot included node outside ancestry: %#v", snapshot.Comments)
				}
			}
		})
	}

	t.Run("ancestry depth limit", func(t *testing.T) {
		parentID := insertComment("deep-root", nil)
		for i := 0; i < maxCommentBranchAncestryDepth; i++ {
			parentID = insertComment(fmt.Sprintf("deep-%d", i), parentID)
		}
		requestID := uuid.New().String()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+parentID+"/branch", map[string]any{
			"content_base": fmt.Sprintf("deep-%d", maxCommentBranchAncestryDepth-1),
		}), "commentId", parentID)
		req.Header.Set("Idempotency-Key", requestID)
		testHandler.CreateCommentBranch(w, req)
		if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"branch_context_too_large"`) {
			t.Fatalf("deep branch = %d: %s, want 413 branch_context_too_large", w.Code, w.Body.String())
		}
	})

	t.Run("snapshot byte limit", func(t *testing.T) {
		if _, err := testPool.Exec(ctx, `UPDATE issue SET description = $2 WHERE id = $1`, issueID, strings.Repeat("x", maxCommentBranchSnapshotBytes)); err != nil {
			t.Fatalf("inflate issue description: %v", err)
		}
		requestID := uuid.New().String()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+rootID+"/branch", map[string]any{
			"content_base": "root",
		}), "commentId", rootID)
		req.Header.Set("Idempotency-Key", requestID)
		testHandler.CreateCommentBranch(w, req)
		if w.Code != http.StatusRequestEntityTooLarge || !strings.Contains(w.Body.String(), `"code":"branch_context_too_large"`) {
			t.Fatalf("oversized branch = %d: %s, want 413 branch_context_too_large", w.Code, w.Body.String())
		}
		var count int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND branch_request_id = $2`, issueID, requestID).Scan(&count); err != nil {
			t.Fatalf("count oversized branch tasks: %v", err)
		}
		if count != 0 {
			t.Fatalf("oversized branch persisted %d task(s), want 0", count)
		}
	})
}

func TestCommentBranchQueueIsolation(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
		VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3)
		RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-queue-runtime-%d", suffix), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	insertAgent := func(label string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
			VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
		`, testWorkspaceID, fmt.Sprintf("branch-queue-%s-%d", label, suffix), runtimeID, testUserID).Scan(&id); err != nil {
			t.Fatalf("insert %s agent: %v", label, err)
		}
		return id
	}
	firstAgentID := insertAgent("first")
	secondAgentID := insertAgent("second")
	issueID := insertWorkflowTestIssue(t, "branch queue isolation", int(suffix%1_000_000)+9_400_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, firstAgentID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'branch while target busy') RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	var activeTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, priority, started_at)
		VALUES ($1, $2, $3, 'running', 0, now()) RETURNING id
	`, firstAgentID, runtimeID, issueID).Scan(&activeTaskID); err != nil {
		t.Fatalf("insert active task: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = ANY($1::uuid[])`, []string{firstAgentID, secondAgentID})
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	create := func(agentID string) (string, string) {
		t.Helper()
		body := map[string]any{"content_base": "branch while target busy"}
		if agentID != "" {
			body["agent_id"] = agentID
		}
		requestID := uuid.New().String()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/branch", body), "commentId", commentID)
		req.Header.Set("Idempotency-Key", requestID)
		testHandler.CreateCommentBranch(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("create branch = %d: %s", w.Code, w.Body.String())
		}
		var taskID, status string
		if err := testPool.QueryRow(ctx, `SELECT id, status FROM agent_task_queue WHERE issue_id = $1 AND branch_request_id = $2`, issueID, requestID).Scan(&taskID, &status); err != nil {
			t.Fatalf("load branch: %v", err)
		}
		return taskID, status
	}

	firstBranchID, firstStatus := create("")
	if firstStatus != "deferred" {
		t.Fatalf("same-agent branch status = %q, want deferred", firstStatus)
	}
	secondBranchID, secondBranchStatus := create("")
	if secondBranchStatus != "deferred" {
		t.Fatalf("second same-agent branch status = %q, want deferred", secondBranchStatus)
	}
	_, secondStatus := create(secondAgentID)
	if secondStatus != "queued" {
		t.Fatalf("different-agent branch status = %q, want queued", secondStatus)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, activeTaskID); err != nil {
		t.Fatalf("finish ordinary task: %v", err)
	}
	promoted, err := testHandler.TaskService.PromoteDeferredCommentBranch(
		ctx,
		pgtype.UUID{Bytes: uuid.MustParse(issueID), Valid: true},
		pgtype.UUID{Bytes: uuid.MustParse(firstAgentID), Valid: true},
	)
	if err != nil || promoted == nil || uuidToString(promoted.ID) != firstBranchID {
		t.Fatalf("promote same-agent branch = (%#v, %v), want %s", promoted, err, firstBranchID)
	}
	if _, err := testPool.Exec(ctx, `UPDATE agent_task_queue SET status = 'completed', completed_at = now() WHERE id = $1`, firstBranchID); err != nil {
		t.Fatalf("finish first branch: %v", err)
	}
	promoted, err = testHandler.TaskService.PromoteDeferredCommentBranch(
		ctx,
		pgtype.UUID{Bytes: uuid.MustParse(issueID), Valid: true},
		pgtype.UUID{Bytes: uuid.MustParse(firstAgentID), Valid: true},
	)
	if err != nil || promoted == nil || uuidToString(promoted.ID) != secondBranchID {
		t.Fatalf("promote second branch FIFO = (%#v, %v), want %s", promoted, err, secondBranchID)
	}
}

// The original branch bug created a second ordinary run when completion
// reconciliation replayed the selected member comment as undelivered. A branch
// consumes its frozen branch point even though that body bypasses the ordinary
// claim receipt. Its result is a new top-level comment whose source_task_id
// keeps the exact branch-point relation without visually nesting the result.
func TestCommentBranchCompletionDoesNotReplayBranchPoint(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
		VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-completion-runtime-%d", suffix), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-completion-agent-%d", suffix), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	issueID := insertWorkflowTestIssue(t, "branch completion does not replay", int(suffix%1_000_000)+9_600_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	var rootCommentID, commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'thread root') RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&rootCommentID); err != nil {
		t.Fatalf("insert thread root: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, parent_id)
		VALUES ($1, $2, 'member', $3, 'selected branch point', $4) RETURNING id
	`, issueID, testWorkspaceID, testUserID, rootCommentID).Scan(&commentID); err != nil {
		t.Fatalf("insert selected reply: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	requestID := uuid.New().String()
	createW := httptest.NewRecorder()
	createReq := withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/branch", map[string]any{"content_base": "selected branch point"}), "commentId", commentID)
	createReq.Header.Set("Idempotency-Key", requestID)
	testHandler.CreateCommentBranch(createW, createReq)
	if createW.Code != http.StatusCreated {
		t.Fatalf("create branch = %d: %s", createW.Code, createW.Body.String())
	}
	var taskID string
	if err := testPool.QueryRow(ctx, `
		UPDATE agent_task_queue
		SET status = 'running', dispatched_at = now(), started_at = now()
		WHERE issue_id = $1 AND branch_request_id = $2
		RETURNING id
	`, issueID, requestID).Scan(&taskID); err != nil {
		t.Fatalf("start branch task: %v", err)
	}
	rerunW := httptest.NewRecorder()
	rerunReq := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/rerun", map[string]any{
		"task_id": taskID,
	}), "id", issueID)
	testHandler.RerunIssue(rerunW, rerunReq)
	if rerunW.Code != http.StatusConflict || !strings.Contains(rerunW.Body.String(), `"code":"comment_branch_rerun_unsupported"`) {
		t.Fatalf("rerun branch = %d: %s, want actionable 409", rerunW.Code, rerunW.Body.String())
	}
	nestedW := httptest.NewRecorder()
	nestedReq := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "misplaced branch result", "parent_id": commentID,
	}), "id", issueID)
	nestedReq.Header.Set("X-Agent-ID", agentID)
	nestedReq.Header.Set("X-Task-ID", taskID)
	testHandler.CreateComment(nestedW, nestedReq)
	if nestedW.Code != http.StatusConflict || !strings.Contains(nestedW.Body.String(), "top-level result comment") {
		t.Fatalf("nested branch result = %d: %s, want actionable 409", nestedW.Code, nestedW.Body.String())
	}

	resultW := httptest.NewRecorder()
	resultReq := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "branch result",
	}), "id", issueID)
	resultReq.Header.Set("X-Agent-ID", agentID)
	resultReq.Header.Set("X-Task-ID", taskID)
	testHandler.CreateComment(resultW, resultReq)
	if resultW.Code != http.StatusCreated {
		t.Fatalf("create top-level branch result = %d: %s", resultW.Code, resultW.Body.String())
	}

	var resultCommentID, resultBranchPointID string
	var resultHasParent bool
	if err := testPool.QueryRow(ctx, `
		SELECT c.id, c.parent_id IS NOT NULL, q.branch_point_comment_id
		FROM comment c
		JOIN agent_task_queue q ON q.id = c.source_task_id
		WHERE c.issue_id = $1 AND c.source_task_id = $2
	`, issueID, taskID).Scan(&resultCommentID, &resultHasParent, &resultBranchPointID); err != nil {
		t.Fatalf("load branch result relation: %v", err)
	}
	if resultHasParent {
		t.Fatalf("branch result %s has parent_id, want top-level comment", resultCommentID)
	}
	if resultBranchPointID != commentID {
		t.Fatalf("branch result source = %s, want selected reply %s", resultBranchPointID, commentID)
	}

	// A normal comment posted after the branch starts must not leak into the
	// frozen branch prompt. Keep mainline scheduling semantics: a running task
	// may have one queued successor, so this becomes ordinary queued work now;
	// branch completion must neither consume nor enqueue it again.
	laterW := httptest.NewRecorder()
	laterReq := withURLParam(newRequest(http.MethodPost, "/api/issues/"+issueID+"/comments", map[string]any{
		"content": "ordinary work after branch start",
	}), "id", issueID)
	testHandler.CreateComment(laterW, laterReq)
	if laterW.Code != http.StatusCreated {
		t.Fatalf("create later ordinary comment = %d: %s", laterW.Code, laterW.Body.String())
	}
	var laterCommentID string
	if err := testPool.QueryRow(ctx, `
		SELECT id FROM comment
		WHERE issue_id = $1 AND content = 'ordinary work after branch start'
	`, issueID).Scan(&laterCommentID); err != nil {
		t.Fatalf("load later ordinary comment: %v", err)
	}
	var plannedCommentIDs []string
	var frozenContext []byte
	if err := testPool.QueryRow(ctx, `
		SELECT coalesced_comment_ids, branch_context
		FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&plannedCommentIDs, &frozenContext); err != nil {
		t.Fatalf("load branch plan: %v", err)
	}
	if slices.Contains(plannedCommentIDs, laterCommentID) {
		t.Fatalf("branch planned comments = %v, must not absorb later comment %s", plannedCommentIDs, laterCommentID)
	}
	var frozenSnapshot commentBranchSnapshot
	if err := json.Unmarshal(frozenContext, &frozenSnapshot); err != nil {
		t.Fatalf("decode frozen branch context: %v", err)
	}
	for _, frozenComment := range frozenSnapshot.Comments {
		if frozenComment.ID == laterCommentID {
			t.Fatal("later ordinary comment leaked into frozen branch snapshot")
		}
	}
	var queuedBeforeCompletion int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
		  AND trigger_comment_id = $3 AND branch_context IS NULL
	`, issueID, agentID, laterCommentID).Scan(&queuedBeforeCompletion); err != nil {
		t.Fatalf("count ordinary queued successor: %v", err)
	}
	if queuedBeforeCompletion != 1 {
		t.Fatalf("ordinary queued successors before branch completion = %d, want 1", queuedBeforeCompletion)
	}

	if w := completeTaskViaHandler(t, taskID, "done"); w.Code != http.StatusOK {
		t.Fatalf("complete branch = %d: %s", w.Code, w.Body.String())
	}
	var pending int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2
		  AND status IN ('queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
	`, issueID, agentID).Scan(&pending); err != nil {
		t.Fatalf("count follow-up tasks: %v", err)
	}
	if pending != 1 {
		t.Fatalf("branch completion created %d follow-up task(s), want exactly the later ordinary comment", pending)
	}
	var followUpTriggerID string
	var followUpHasBranchContext bool
	if err := testPool.QueryRow(ctx, `
		SELECT trigger_comment_id, branch_context IS NOT NULL
		FROM agent_task_queue
		WHERE issue_id = $1 AND agent_id = $2 AND status = 'queued'
	`, issueID, agentID).Scan(&followUpTriggerID, &followUpHasBranchContext); err != nil {
		t.Fatalf("load ordinary follow-up: %v", err)
	}
	if followUpTriggerID != laterCommentID || followUpHasBranchContext {
		t.Fatalf("follow-up trigger=%s branch=%v, want ordinary later comment %s", followUpTriggerID, followUpHasBranchContext, laterCommentID)
	}
}

func TestCommentBranchSourceDeletionPreservesFrozenRun(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
		VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-delete-runtime-%d", suffix), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-delete-agent-%d", suffix), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	var issueIDs []string
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = ANY($1::uuid[])`, issueIDs)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	for i, tc := range []struct {
		status     string
		wantDelete int
		wantStatus string
	}{
		{status: "queued", wantDelete: http.StatusNoContent, wantStatus: "queued"},
		{status: "deferred", wantDelete: http.StatusNoContent, wantStatus: "deferred"},
		{status: "dispatched", wantDelete: http.StatusNoContent, wantStatus: "dispatched"},
		{status: "running", wantDelete: http.StatusNoContent, wantStatus: "running"},
		{status: "waiting_local_directory", wantDelete: http.StatusNoContent, wantStatus: "waiting_local_directory"},
	} {
		t.Run(tc.status, func(t *testing.T) {
			issueID := insertWorkflowTestIssue(t, "branch delete "+tc.status, int(suffix%1_000_000)+9_950_000+i)
			issueIDs = append(issueIDs, issueID)
			if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
				t.Fatalf("assign issue: %v", err)
			}
			var rootCommentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
				VALUES ($1, $2, 'member', $3, $4) RETURNING id
			`, issueID, testWorkspaceID, testUserID, "root "+tc.status).Scan(&rootCommentID); err != nil {
				t.Fatalf("insert root comment: %v", err)
			}
			var commentID string
			if err := testPool.QueryRow(ctx, `
				INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, parent_id)
				VALUES ($1, $2, 'member', $3, $4, $5) RETURNING id
			`, issueID, testWorkspaceID, testUserID, tc.status, rootCommentID).Scan(&commentID); err != nil {
				t.Fatalf("insert selected descendant: %v", err)
			}
			requestID := uuid.New().String()
			createW := httptest.NewRecorder()
			createReq := withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/branch", map[string]any{"content_base": tc.status}), "commentId", commentID)
			createReq.Header.Set("Idempotency-Key", requestID)
			testHandler.CreateCommentBranch(createW, createReq)
			if createW.Code != http.StatusCreated {
				t.Fatalf("create branch = %d: %s", createW.Code, createW.Body.String())
			}
			editW := httptest.NewRecorder()
			editReq := withURLParam(newRequest(http.MethodPut, "/api/comments/"+commentID, map[string]any{
				"content":        tc.status + " edited",
				"content_base":   tc.status,
				"attachment_ids": []string{},
			}), "commentId", commentID)
			testHandler.UpdateComment(editW, editReq)
			if editW.Code != http.StatusOK {
				t.Fatalf("edit branch source = %d: %s", editW.Code, editW.Body.String())
			}
			var taskID string
			if err := testPool.QueryRow(ctx, `
				UPDATE agent_task_queue
				SET status = $3,
					dispatched_at = CASE WHEN $3 IN ('dispatched', 'running', 'waiting_local_directory') THEN now() ELSE NULL END,
					started_at = CASE WHEN $3 IN ('running', 'waiting_local_directory') THEN now() ELSE NULL END,
					wait_reason = CASE WHEN $3 = 'waiting_local_directory' THEN 'local directory required' ELSE NULL END
				WHERE issue_id = $1 AND branch_request_id = $2 RETURNING id
			`, issueID, requestID, tc.status).Scan(&taskID); err != nil {
				t.Fatalf("set branch status: %v", err)
			}
			var frozenContext []byte
			if err := testPool.QueryRow(ctx, `SELECT branch_context FROM agent_task_queue WHERE id = $1`, taskID).Scan(&frozenContext); err != nil {
				t.Fatalf("load frozen branch after edit: %v", err)
			}
			var snapshot commentBranchSnapshot
			if err := json.Unmarshal(frozenContext, &snapshot); err != nil {
				t.Fatalf("decode frozen branch after edit: %v", err)
			}
			if got := snapshot.Comments[len(snapshot.Comments)-1].Content; got != tc.status {
				t.Fatalf("frozen branch content after edit = %q, want %q", got, tc.status)
			}

			deleteW := httptest.NewRecorder()
			deleteReq := withURLParam(newRequest(http.MethodDelete, "/api/comments/"+rootCommentID, nil), "commentId", rootCommentID)
			testHandler.DeleteComment(deleteW, deleteReq)
			if deleteW.Code != tc.wantDelete {
				t.Fatalf("delete in %s = %d, want %d: %s", tc.status, deleteW.Code, tc.wantDelete, deleteW.Body.String())
			}
			var gotStatus string
			if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&gotStatus); err != nil {
				t.Fatalf("reload branch: %v", err)
			}
			if gotStatus != tc.wantStatus {
				t.Fatalf("branch status after delete = %q, want %q", gotStatus, tc.wantStatus)
			}
			var commentCount int
			if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM comment WHERE id = ANY($1::uuid[])`, []string{rootCommentID, commentID}).Scan(&commentCount); err != nil {
				t.Fatalf("count comment: %v", err)
			}
			if commentCount != 0 {
				t.Fatalf("comment count = %d, want deleted source subtree", commentCount)
			}
		})
	}
}

func TestCommentBranchContentBaselineAndIdempotency(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
		VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-idempotency-runtime-%d", suffix), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-idempotency-agent-%d", suffix), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	issueID := insertWorkflowTestIssue(t, "branch baseline and idempotency", int(suffix%1_000_000)+9_700_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	insertComment := func(content string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
			VALUES ($1, $2, 'member', $3, $4) RETURNING id
		`, issueID, testWorkspaceID, testUserID, content).Scan(&id); err != nil {
			t.Fatalf("insert comment: %v", err)
		}
		return id
	}
	firstCommentID := insertComment("first")
	secondCommentID := insertComment("second")
	thirdCommentID := insertComment("third")
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	branch := func(commentID, requestID string, body map[string]any) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/branch", body), "commentId", commentID)
		req.Header.Set("Idempotency-Key", requestID)
		testHandler.CreateCommentBranch(w, req)
		return w
	}
	if w := branch(firstCommentID, "not-a-uuid", map[string]any{"content_base": "first"}); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid idempotency key = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := branch(firstCommentID, uuid.New().String(), map[string]any{}); w.Code != http.StatusBadRequest {
		t.Fatalf("missing content baseline = %d, want 400: %s", w.Code, w.Body.String())
	}
	if w := branch(firstCommentID, uuid.New().String(), map[string]any{"content_base": "stale"}); w.Code != http.StatusConflict {
		t.Fatalf("stale content baseline = %d, want 409: %s", w.Code, w.Body.String())
	}

	// PR1 established that aggregate issue revision is not a text-edit gate.
	// An unrelated issue mutation must not reject a branch whose selected
	// comment baseline is still exact.
	if _, err := testPool.Exec(ctx, `UPDATE issue SET title = title || ' updated', revision = revision + 1 WHERE id = $1`, issueID); err != nil {
		t.Fatalf("mutate unrelated issue field: %v", err)
	}
	requestID := uuid.New().String()
	body := map[string]any{"content_base": "first"}
	if w := branch(firstCommentID, requestID, body); w.Code != http.StatusCreated {
		t.Fatalf("first idempotent create = %d: %s", w.Code, w.Body.String())
	}
	if w := branch(firstCommentID, requestID, body); w.Code != http.StatusOK {
		t.Fatalf("exact replay = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w := branch(firstCommentID, requestID, map[string]any{"content_base": "first", "agent_id": agentID}); w.Code != http.StatusConflict {
		t.Fatalf("automatic request replayed as explicit = %d, want 409: %s", w.Code, w.Body.String())
	}
	if w := branch(firstCommentID, requestID, map[string]any{"content_base": "first", "agent_id": uuid.New().String()}); w.Code != http.StatusConflict {
		t.Fatalf("same-key different agent = %d, want 409: %s", w.Code, w.Body.String())
	}
	explicitRequestID := uuid.New().String()
	if w := branch(thirdCommentID, explicitRequestID, map[string]any{"content_base": "third", "agent_id": agentID}); w.Code != http.StatusCreated {
		t.Fatalf("explicit idempotent create = %d: %s", w.Code, w.Body.String())
	}
	if w := branch(thirdCommentID, explicitRequestID, map[string]any{"content_base": "third"}); w.Code != http.StatusConflict {
		t.Fatalf("explicit request replayed as automatic = %d, want 409: %s", w.Code, w.Body.String())
	}
	if _, err := testPool.Exec(ctx, `UPDATE comment SET content = 'first edited', revision = revision + 1 WHERE id = $1`, firstCommentID); err != nil {
		t.Fatalf("edit branch source after creation: %v", err)
	}
	if w := branch(firstCommentID, requestID, body); w.Code != http.StatusOK {
		t.Fatalf("replay after source edit = %d, want 200: %s", w.Code, w.Body.String())
	}
	if w := branch(firstCommentID, requestID, map[string]any{"content_base": "first edited"}); w.Code != http.StatusConflict {
		t.Fatalf("same-key different content baseline = %d, want 409: %s", w.Code, w.Body.String())
	}
	if w := branch(secondCommentID, requestID, map[string]any{"content_base": "second"}); w.Code != http.StatusConflict {
		t.Fatalf("cross-comment key reuse = %d, want 409: %s", w.Code, w.Body.String())
	}

	concurrentRequestID := uuid.New().String()
	start := make(chan struct{})
	codes := make(chan int, 2)
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			codes <- branch(secondCommentID, concurrentRequestID, map[string]any{"content_base": "second"}).Code
		}()
	}
	close(start)
	wg.Wait()
	close(codes)
	created, replayed := 0, 0
	for code := range codes {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusOK:
			replayed++
		default:
			t.Fatalf("concurrent replay returned %d, want 201/200", code)
		}
	}
	if created != 1 || replayed != 1 {
		t.Fatalf("concurrent outcomes = created:%d replayed:%d, want 1/1", created, replayed)
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1 AND branch_request_id = $2`, issueID, concurrentRequestID).Scan(&count); err != nil {
		t.Fatalf("count concurrent branches: %v", err)
	}
	if count != 1 {
		t.Fatalf("concurrent same-key create persisted %d rows, want 1", count)
	}
}

func TestCommentBranchTargetChangeDuringInsertReturnsConflict(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	var runtimeID, agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
		VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-target-change-runtime-%d", suffix), testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-target-change-agent-%d", suffix), runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	issueID := insertWorkflowTestIssue(t, "branch target changes during insert", int(suffix%1_000_000)+9_800_000)
	if _, err := testPool.Exec(ctx, `UPDATE issue SET assignee_type = 'agent', assignee_id = $2 WHERE id = $1`, issueID, agentID); err != nil {
		t.Fatalf("assign issue: %v", err)
	}
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'branch me') RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	h := *testHandler
	h.TxStarter = failCommentBranchInsertTxStarter{inner: testHandler.TxStarter}
	w := httptest.NewRecorder()
	req := withURLParam(newRequest(http.MethodPost, "/api/comments/"+commentID+"/branch", map[string]any{
		"content_base": "branch me",
	}), "commentId", commentID)
	req.Header.Set("Idempotency-Key", uuid.New().String())
	h.CreateCommentBranch(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("target change during insert = %d, want 409: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"code":"branch_target_changed"`) {
		t.Fatalf("target change response = %s, want branch_target_changed", w.Body.String())
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count rolled-back branch tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("target change persisted %d branch tasks, want 0", count)
	}
}

func TestCreateDeferredCommentBranchTaskRejectsStaleAgentRuntime(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()

	insertRuntime := func(label string) string {
		t.Helper()
		var id string
		if err := testPool.QueryRow(ctx, `
			INSERT INTO agent_runtime (workspace_id, name, runtime_mode, provider, status, device_info, metadata, visibility, owner_id)
			VALUES ($1, $2, 'local', 'codex', 'online', 'test', '{"capabilities":["comment-branch-v1"]}'::jsonb, 'private', $3)
			RETURNING id
		`, testWorkspaceID, fmt.Sprintf("branch-runtime-fence-%s-%d", label, suffix), testUserID).Scan(&id); err != nil {
			t.Fatalf("insert %s runtime: %v", label, err)
		}
		return id
	}
	staleRuntimeID := insertRuntime("stale")
	currentRuntimeID := insertRuntime("current")
	var agentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (workspace_id, name, description, runtime_mode, runtime_config, runtime_id, visibility, permission_mode, max_concurrent_tasks, owner_id)
		VALUES ($1, $2, '', 'local', '{}'::jsonb, $3, 'private', 'private', 1, $4) RETURNING id
	`, testWorkspaceID, fmt.Sprintf("branch-runtime-fence-agent-%d", suffix), staleRuntimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}
	issueID := insertWorkflowTestIssue(t, "branch runtime relation fence", int(suffix%1_000_000)+9_850_000)
	var commentID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'branch me') RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_task_queue WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent WHERE id = $1`, agentID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM agent_runtime WHERE id = ANY($1::uuid[])`, []string{staleRuntimeID, currentRuntimeID})
	})

	if _, err := testPool.Exec(ctx, `UPDATE agent SET runtime_id = $2 WHERE id = $1`, agentID, currentRuntimeID); err != nil {
		t.Fatalf("move agent to current runtime: %v", err)
	}
	_, err := testHandler.Queries.CreateDeferredCommentBranchTask(ctx, db.CreateDeferredCommentBranchTaskParams{
		AgentID:              parseUUID(agentID),
		RuntimeID:            parseUUID(staleRuntimeID),
		IssueID:              parseUUID(issueID),
		TriggerCommentID:     parseUUID(commentID),
		OriginatorUserID:     parseUUID(testUserID),
		AccountableUserID:    parseUUID(testUserID),
		OriginatorSource:     pgtype.Text{String: "direct_human", Valid: true},
		TriggerEvidenceKind:  pgtype.Text{String: "comment_branch", Valid: true},
		TriggerEvidenceRefID: parseUUID(commentID),
		BranchPointCommentID: parseUUID(commentID),
		BranchContext:        []byte(`{"version":1}`),
		BranchRequestID:      pgtype.UUID{Bytes: uuid.New(), Valid: true},
	})
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("create branch against stale runtime error = %v, want pgx.ErrNoRows", err)
	}
	var count int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM agent_task_queue WHERE issue_id = $1`, issueID).Scan(&count); err != nil {
		t.Fatalf("count stale-runtime tasks: %v", err)
	}
	if count != 0 {
		t.Fatalf("stale runtime fence persisted %d task(s), want 0", count)
	}
}

func TestCommentBranchMembershipGateDoesNotRevealCommentExistence(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	suffix := time.Now().UnixNano()
	issueID := insertWorkflowTestIssue(t, "branch membership gate", int(suffix%1_000_000)+9_900_000)
	var commentID, outsiderID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'private branch point') RETURNING id
	`, issueID, testWorkspaceID, testUserID).Scan(&commentID); err != nil {
		t.Fatalf("insert comment: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Comment Branch Outsider', $1) RETURNING id
	`, fmt.Sprintf("comment-branch-outsider-%d@multica.test", suffix)).Scan(&outsiderID); err != nil {
		t.Fatalf("insert outsider: %v", err)
	}
	t.Cleanup(func() {
		cleanup := context.Background()
		_, _ = testPool.Exec(cleanup, `DELETE FROM comment WHERE issue_id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(cleanup, `DELETE FROM "user" WHERE id = $1`, outsiderID)
	})

	branchAsOutsider := func(targetCommentID string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		req := withURLParam(newRequestAs(outsiderID, http.MethodPost, "/api/comments/"+targetCommentID+"/branch", map[string]any{
			"content_base": "private branch point",
		}), "commentId", targetCommentID)
		req.Header.Set("Idempotency-Key", uuid.New().String())
		testHandler.CreateCommentBranch(w, req)
		return w
	}
	known := branchAsOutsider(commentID)
	unknown := branchAsOutsider(uuid.New().String())
	if known.Code != http.StatusNotFound || unknown.Code != http.StatusNotFound {
		t.Fatalf("membership gate codes = known:%d unknown:%d, want 404/404", known.Code, unknown.Code)
	}
	if known.Body.String() != unknown.Body.String() {
		t.Fatalf("membership gate reveals existence: known=%q unknown=%q", known.Body.String(), unknown.Body.String())
	}
}
