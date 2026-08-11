package service

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/pooltestdb"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestPoolRetryRoutingSnapshot(t *testing.T) {
	agentID := pgtype.UUID{Bytes: [16]byte{1}, Valid: true}
	workspaceID := pgtype.UUID{Bytes: [16]byte{2}, Valid: true}
	requesterID := pgtype.UUID{Bytes: [16]byte{3}, Valid: true}
	runtimeID := pgtype.UUID{Bytes: [16]byte{4}, Valid: true}
	retryID := pgtype.UUID{Bytes: [16]byte{5}, Valid: true}
	agent := db.Agent{ID: agentID, WorkspaceID: workspaceID, OwnerID: requesterID, RuntimeBindingMode: runtimepool.BindingPool}
	member := db.Member{WorkspaceID: workspaceID, UserID: requesterID}

	tests := []struct {
		name        string
		placement   PoolPlacement
		deferred    bool
		wantStatus  string
		wantState   string
		wantRuntime pgtype.UUID
	}{
		{name: "assigned parent pins retry", placement: PoolPlacement{State: runtimepool.SessionAffinityPinned, RuntimeID: runtimeID}, wantStatus: runtimepool.StatusWaitingRuntime, wantState: runtimepool.SessionAffinityPinned, wantRuntime: runtimeID},
		{name: "unassigned parent starts without affinity", placement: PoolPlacement{State: runtimepool.SessionAffinityNone}, wantStatus: runtimepool.StatusWaitingRuntime, wantState: runtimepool.SessionAffinityNone},
		{name: "backoff preserves deferred", placement: PoolPlacement{State: runtimepool.SessionAffinityPinned, RuntimeID: runtimeID}, deferred: true, wantStatus: "deferred", wantState: runtimepool.SessionAffinityPinned, wantRuntime: runtimeID},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			snapshot, err := newPoolRoutingSnapshot(
				agent,
				member,
				requesterID,
				requesterID,
				PoolPlacementRequest{RetryOfTaskID: retryID},
				tt.placement,
				[]byte(`{"capabilities_all":[]}`),
				tt.deferred,
			)
			if err != nil {
				t.Fatalf("newPoolRoutingSnapshot: %v", err)
			}
			if snapshot.Status != tt.wantStatus || snapshot.SessionAffinityState != tt.wantState || snapshot.SessionAffinityRuntimeID != tt.wantRuntime {
				t.Fatalf("retry routing = %+v, want status=%q state=%q runtime=%v", snapshot, tt.wantStatus, tt.wantState, tt.wantRuntime)
			}
			if snapshot.RuntimeTriggerUserID != requesterID {
				t.Fatalf("runtime_trigger_user_id = %v, want %v", snapshot.RuntimeTriggerUserID, requesterID)
			}
		})
	}
}

func TestPoolTriggerUserRequiresCurrentInvocationActor(t *testing.T) {
	userID := pgtype.UUID{Bytes: [16]byte{9}, Valid: true}
	service := &TaskService{}
	if got := service.poolTriggerUserForIssue(context.Background(), pgtype.UUID{}, pgtype.UUID{}, userID); got != userID {
		t.Fatalf("explicit actor = %v, want %v", got, userID)
	}
	if got := service.poolTriggerUserForIssue(context.Background(), pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}); got.Valid {
		t.Fatalf("missing actor produced personal trigger %v", got)
	}
}

func TestPoolRetrySQLAndTransactionShape(t *testing.T) {
	rawSQL, err := os.ReadFile("../../pkg/db/queries/agent.sql")
	if err != nil {
		t.Fatalf("read agent.sql: %v", err)
	}
	fixed := namedAgentQuery(t, string(rawSQL), "CreateRetryTask")
	if strings.Contains(fixed, "runtime_binding_mode") || strings.Contains(fixed, "waiting_runtime") {
		t.Fatal("fixed CreateRetryTask must keep its legacy routing shape")
	}
	lockSource := namedAgentQuery(t, string(rawSQL), "LockPoolRetrySourceTask")
	if !strings.Contains(lockSource, "FOR UPDATE") {
		t.Fatal("Pool retry source must be locked after Member and Agent")
	}
	pool := namedAgentQuery(t, string(rawSQL), "CreatePoolRetryTask")
	for _, fragment := range []string{
		"NULL::uuid", "runtime_binding_mode", "runtime_requirements",
		"placement_workspace_id", "runtime_requester_user_id",
		"runtime_trigger_user_id",
		"session_affinity_state", "session_affinity_runtime_id",
		"retry_of_task_id", "fire_at",
	} {
		if !strings.Contains(pool, fragment) {
			t.Fatalf("CreatePoolRetryTask missing %q", fragment)
		}
	}

	rawGo, err := os.ReadFile("task.go")
	if err != nil {
		t.Fatalf("read task.go: %v", err)
	}
	failStart := strings.Index(string(rawGo), "func (s *TaskService) FailTask")
	failEnd := strings.Index(string(rawGo)[failStart:], "var retryableReasons")
	if failStart < 0 || failEnd < 0 {
		t.Fatal("locate FailTask source")
	}
	failBlock := string(rawGo)[failStart : failStart+failEnd]
	last := -1
	for _, marker := range []string{
		"lockPoolTaskCreateMember",
		"lockChatSessionForTaskWrite",
		"lockPoolTaskCreateAgent",
		"FailAgentTask",
		"createPoolRetryTaskLocked",
	} {
		at := strings.Index(failBlock, marker)
		if at < 0 || at <= last {
			t.Fatalf("FailTask Pool retry lock/create order missing %q", marker)
		}
		last = at
	}
	if strings.Contains(failBlock, "createPoolTask(ctx") {
		t.Fatal("FailTask must not open a nested Pool factory transaction")
	}
}

func TestPoolManualRerunPlacement(t *testing.T) {
	sourceID := pgtype.UUID{Bytes: [16]byte{6}, Valid: true}
	for _, fresh := range []bool{false, true} {
		placement := poolManualRerunPlacement(sourceID, fresh)
		if placement.RerunOfTaskID != sourceID || !placement.ForceFreshSession || placement.ExplicitFreshSession != fresh {
			t.Fatalf("poolManualRerunPlacement(fresh=%v) = %+v", fresh, placement)
		}
	}
}

func TestPoolManualRerunAtomicSourceShape(t *testing.T) {
	rawSQL, err := os.ReadFile("../../pkg/db/queries/agent.sql")
	if err != nil {
		t.Fatalf("read agent.sql: %v", err)
	}
	lockSource := namedAgentQuery(t, string(rawSQL), "LockPoolRerunSourceTask")
	if !strings.Contains(lockSource, "FOR UPDATE") {
		t.Fatal("Pool manual rerun source must be locked before placement")
	}

	rawGo, err := os.ReadFile("task.go")
	if err != nil {
		t.Fatalf("read task.go: %v", err)
	}
	start := strings.Index(string(rawGo), "func (s *TaskService) rerunIssue")
	end := strings.Index(string(rawGo)[start:], "func (s *TaskService) promoteNewestSurvivingComment")
	if start < 0 || end < 0 {
		t.Fatal("locate rerunIssue source")
	}
	block := string(rawGo)[start : start+end]
	last := -1
	for _, marker := range []string{
		"targetAgent.RuntimeBindingMode == runtimepool.BindingPool",
		"s.runInTx",
		"lockPoolTaskCreateMember",
		"lockPoolTaskCreateAgent",
		"CancelAgentTasksByIssueAndAgent",
		"createPoolTaskAfterLocks",
		"broadcastPoolRerunCancelledTasks",
		"AssignPoolWorkspace",
		"s.enqueueRerunTask",
	} {
		at := strings.Index(block, marker)
		if at < 0 || at <= last {
			t.Fatalf("Pool rerun atomic/post-commit order missing %q", marker)
		}
		last = at
	}
	if strings.Contains(block[:strings.Index(block, "s.enqueueRerunTask")], "s.createPoolTask(ctx") {
		t.Fatal("Pool rerun must not open a nested factory transaction")
	}
}

func TestTask9EntryGatesUseAgentRoutability(t *testing.T) {
	tests := []struct {
		file      string
		function  string
		wantCount int
	}{
		{file: "issue.go", function: "isAgentAssigneeReadyWithQueries", wantCount: 1},
		{file: "issue_trigger.go", function: "WillEnqueueRun", wantCount: 1},
		{file: "../handler/issue.go", function: "assigneeFallbackAgent", wantCount: 1},
		{file: "../handler/issue.go", function: "isAgentAssigneeReady", wantCount: 1},
		{file: "../handler/comment.go", function: "routeReplyToParentAuthor", wantCount: 1},
		{file: "../handler/comment.go", function: "routeConversationContinuationToAgent", wantCount: 1},
		{file: "../handler/comment.go", function: "routeAssignedSquadLeaderFallback", wantCount: 1},
		{file: "../handler/comment.go", function: "resolveMentionedAgentCommentTriggers", wantCount: 2},
		{file: "../handler/issue_child_done.go", function: "triggerChildDoneAgent", wantCount: 1},
		{file: "../handler/issue_child_done.go", function: "triggerChildDoneSquad", wantCount: 1},
		{file: "task.go", function: "EnqueueDeferredAssigneeFallback", wantCount: 1},
	}
	for _, tt := range tests {
		t.Run(tt.file+"/"+tt.function, func(t *testing.T) {
			raw, err := os.ReadFile(tt.file)
			if err != nil {
				t.Fatalf("read %s: %v", tt.file, err)
			}
			fn := namedGoFunction(t, string(raw), tt.function)
			if got := strings.Count(fn, "IsAgentRoutable(agent)"); got != tt.wantCount {
				t.Fatalf("IsAgentRoutable count = %d, want %d\n%s", got, tt.wantCount, fn)
			}
			if strings.Contains(fn, "!agent.RuntimeID.Valid") {
				t.Fatal("Task9 entry still rejects Pool Agents by RuntimeID")
			}
		})
	}

	raw, err := os.ReadFile("issue.go")
	if err != nil {
		t.Fatalf("read issue.go: %v", err)
	}
	if nativeSquad := namedGoFunction(t, string(raw), "isSquadLeaderReady"); !strings.Contains(nativeSquad, "AgentReadiness") || strings.Contains(nativeSquad, "IsAgentRoutable") {
		t.Fatal("Task10 native Squad readiness gate changed in Task9")
	}
}

func TestPoolDeferredAssigneeFallbackSQLShape(t *testing.T) {
	raw, err := os.ReadFile("../../pkg/db/queries/agent.sql")
	if err != nil {
		t.Fatalf("read agent.sql: %v", err)
	}
	fixed := namedAgentQuery(t, string(raw), "CreateDeferredAgentTask")
	if strings.Contains(fixed, "runtime_binding_mode") || strings.Contains(fixed, "waiting_runtime") {
		t.Fatal("fixed deferred fallback routing changed")
	}
	pool := namedAgentQuery(t, string(raw), "CreatePoolDeferredAgentTask")
	for _, fragment := range []string{
		"escalation_for_task_id", "squad_id", "fire_at", "'pool'",
		"runtime_requirements", "placement_workspace_id",
		"runtime_requester_user_id", "runtime_trigger_user_id", "session_affinity_state",
		"session_affinity_runtime_id", "wait_reason",
	} {
		if !strings.Contains(pool, fragment) {
			t.Fatalf("CreatePoolDeferredAgentTask missing %q", fragment)
		}
	}
}

func namedGoFunction(t *testing.T, raw, name string) string {
	t.Helper()
	start := -1
	searchAt := 0
	for searchAt < len(raw) {
		relative := strings.Index(raw[searchAt:], "func ")
		if relative < 0 {
			break
		}
		candidate := searchAt + relative
		headerEnd := strings.Index(raw[candidate:], "{")
		if headerEnd < 0 {
			break
		}
		header := raw[candidate : candidate+headerEnd]
		if strings.Contains(header, " "+name+"(") || strings.HasPrefix(header, "func "+name+"(") {
			start = candidate
			break
		}
		searchAt = candidate + len("func ")
	}
	if start < 0 {
		t.Fatalf("function %s missing", name)
	}
	rest := raw[start:]
	if next := strings.Index(rest[1:], "\nfunc "); next >= 0 {
		rest = rest[:next+1]
	}
	return rest
}

func TestPoolEntryAgentRoutable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		agent db.Agent
		want  bool
	}{
		{
			name:  "pool unbound",
			agent: db.Agent{RuntimeBindingMode: "pool"},
			want:  true,
		},
		{
			name: "fixed bound",
			agent: db.Agent{
				RuntimeBindingMode: "fixed",
				RuntimeID: pgtype.UUID{
					Bytes: [16]byte{1},
					Valid: true,
				},
			},
			want: true,
		},
		{
			name:  "fixed unbound",
			agent: db.Agent{RuntimeBindingMode: "fixed"},
			want:  false,
		},
		{
			name:  "unknown binding",
			agent: db.Agent{},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := IsAgentRoutable(tt.agent); got != tt.want {
				t.Fatalf("IsAgentRoutable() = %v, want %v", got, tt.want)
			}
		})
	}
}

type poolEntryCommitScheduler struct {
	queries *db.Queries
	calls   []runtimepool.AssignRequest
}

func (s *poolEntryCommitScheduler) AssignWaiting(ctx context.Context, request runtimepool.AssignRequest) (runtimepool.AssignResult, error) {
	s.calls = append(s.calls, request)
	task, err := s.queries.GetAgentTask(ctx, request.FocusTaskID)
	if err != nil {
		return runtimepool.AssignResult{}, fmt.Errorf("load committed focused task: %w", err)
	}
	if task.Status != runtimepool.StatusWaitingRuntime {
		return runtimepool.AssignResult{}, fmt.Errorf("focused task status = %q, want waiting_runtime", task.Status)
	}
	return runtimepool.AssignResult{}, nil
}

func (s *poolEntryCommitScheduler) SweepWaiting(context.Context, int) ([]runtimepool.AssignResult, error) {
	return nil, nil
}

func TestPoolEntryIssuePersistsWaitingSnapshotAndAssignsOnce(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queries := db.New(pool)

	seed := time.Now().UnixNano()
	var userID, workspaceID, agentID, issueID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Pool Entry User', $1)
		RETURNING id
	`, fmt.Sprintf("pool-entry-%d@example.test", seed)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Pool Entry', $1, '', 'PET')
		RETURNING id
	`, fmt.Sprintf("pool-entry-%d", seed)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	const requirements = `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.test/v1"]}`
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks,
			owner_id, instructions, custom_env, custom_args,
			runtime_binding_mode, runtime_requirements
		) VALUES (
			$1, 'Pool Entry Agent', '', 'pool', '{}'::jsonb,
			NULL, 'private', 'private', 1,
			$2, '', '{}'::jsonb, '[]'::jsonb,
			'pool', $3::jsonb
		)
		RETURNING id
	`, workspaceID, userID, requirements).Scan(&agentID); err != nil {
		t.Fatalf("create Pool Agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number, assignee_type, assignee_id
		) VALUES ($1, 'Pool entry issue', 'backlog', 'medium', 'member', $2, 0, 1, 'agent', $3)
		RETURNING id
	`, workspaceID, userID, agentID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}

	scheduler := &poolEntryCommitScheduler{queries: queries}
	service := &TaskService{
		Queries:     queries,
		TxStarter:   pool,
		Bus:         events.New(),
		RuntimePool: scheduler,
	}
	task, err := service.EnqueueTaskForIssue(ctx, db.Issue{
		ID:           issueID,
		WorkspaceID:  workspaceID,
		Priority:     "medium",
		CreatorType:  "member",
		CreatorID:    userID,
		AssigneeType: pgtype.Text{String: "agent", Valid: true},
		AssigneeID:   agentID,
	})
	if err != nil {
		t.Fatalf("EnqueueTaskForIssue: %v", err)
	}

	persisted, err := queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("load persisted task: %v", err)
	}
	if persisted.Status != runtimepool.StatusWaitingRuntime {
		t.Errorf("status = %q, want waiting_runtime", persisted.Status)
	}
	if persisted.RuntimeID.Valid {
		t.Errorf("runtime_id = %v, want NULL", persisted.RuntimeID)
	}
	if persisted.RuntimeTriggerUserID.Valid {
		t.Errorf("runtime_trigger_user_id = %v, want NULL for an entry without an explicit actor", persisted.RuntimeTriggerUserID)
	}
	if persisted.RuntimeBindingMode != runtimepool.BindingPool {
		t.Errorf("runtime_binding_mode = %q, want pool", persisted.RuntimeBindingMode)
	}
	if persisted.PlacementWorkspaceID != workspaceID {
		t.Errorf("placement_workspace_id = %v, want %v", persisted.PlacementWorkspaceID, workspaceID)
	}
	if persisted.RuntimeRequesterUserID != userID {
		t.Errorf("runtime_requester_user_id = %v, want %v", persisted.RuntimeRequesterUserID, userID)
	}
	parsedRequirements, err := runtimepool.ParseRequirements(persisted.RuntimeRequirements)
	if err != nil {
		t.Fatalf("parse persisted runtime_requirements: %v", err)
	}
	canonicalRequirements, err := runtimepool.CanonicalRequirements(parsedRequirements)
	if err != nil {
		t.Fatalf("canonicalize persisted runtime_requirements: %v", err)
	}
	if string(canonicalRequirements) != requirements {
		t.Errorf("runtime_requirements = %s, want %s", canonicalRequirements, requirements)
	}
	if persisted.SessionAffinityState != runtimepool.SessionAffinityNone {
		t.Errorf("session_affinity_state = %q, want none", persisted.SessionAffinityState)
	}
	if persisted.WaitReason.String != "no_eligible_runtime" || !persisted.WaitReason.Valid {
		t.Errorf("wait_reason = %v, want no_eligible_runtime", persisted.WaitReason)
	}
	if len(scheduler.calls) != 1 {
		t.Fatalf("allocator calls = %d, want 1", len(scheduler.calls))
	}
	request := scheduler.calls[0]
	if request.WorkspaceID != workspaceID || request.FocusTaskID != task.ID || request.Limit != runtimepool.AssignmentBatchLimit {
		t.Fatalf("allocator request = %+v", request)
	}
}

func TestPoolEntryMentionPreservesPayloadAndAssignsOnce(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queries := db.New(pool)

	seed := time.Now().UnixNano()
	var userID, workspaceID, agentID, issueID, triggerCommentID, coalescedCommentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Pool Mention User', $1)
		RETURNING id
	`, fmt.Sprintf("pool-mention-%d@example.test", seed)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Pool Mention', $1, '', 'PMT')
		RETURNING id
	`, fmt.Sprintf("pool-mention-%d", seed)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks,
			owner_id, instructions, custom_env, custom_args,
			runtime_binding_mode, runtime_requirements
		) VALUES (
			$1, 'Pool Mention Agent', '', 'pool', '{}'::jsonb,
			NULL, 'private', 'private', 1,
			$2, '', '{}'::jsonb, '[]'::jsonb,
			'pool', '{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.test/v1"]}'::jsonb
		)
		RETURNING id
	`, workspaceID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create Pool Agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number
		) VALUES ($1, 'Pool mention issue', 'backlog', 'high', 'member', $2, 0, 1)
		RETURNING id
	`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	const triggerContent = "preserve this Pool mention payload"
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, $4)
		RETURNING id
	`, issueID, workspaceID, userID, triggerContent).Scan(&triggerCommentID); err != nil {
		t.Fatalf("create trigger comment: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
		VALUES ($1, $2, 'member', $3, 'coalesced payload')
		RETURNING id
	`, issueID, workspaceID, userID).Scan(&coalescedCommentID); err != nil {
		t.Fatalf("create coalesced comment: %v", err)
	}

	scheduler := &poolEntryCommitScheduler{queries: queries}
	service := &TaskService{
		Queries:     queries,
		TxStarter:   pool,
		Bus:         events.New(),
		RuntimePool: scheduler,
	}
	issue := db.Issue{
		ID:          issueID,
		WorkspaceID: workspaceID,
		Priority:    "high",
		CreatorType: "member",
		CreatorID:   userID,
	}
	task, err := service.enqueueMentionTaskWithCommentPlan(
		ctx,
		issue,
		agentID,
		triggerCommentID,
		[]pgtype.UUID{coalescedCommentID},
		false,
		pgtype.UUID{},
		false,
		"",
		pgtype.UUID{},
		pgtype.UUID{},
	)
	if err != nil {
		t.Fatalf("enqueueMentionTaskWithCommentPlan: %v", err)
	}

	persisted, err := queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("load persisted mention task: %v", err)
	}
	if persisted.Status != runtimepool.StatusWaitingRuntime || persisted.RuntimeID.Valid ||
		persisted.RuntimeBindingMode != runtimepool.BindingPool ||
		persisted.PlacementWorkspaceID != workspaceID || persisted.RuntimeRequesterUserID != userID ||
		persisted.RuntimeTriggerUserID != userID ||
		persisted.SessionAffinityState != runtimepool.SessionAffinityNone ||
		persisted.WaitReason.String != "no_eligible_runtime" {
		t.Errorf("persisted routing snapshot = %+v", persisted)
	}
	if persisted.TriggerCommentID != triggerCommentID {
		t.Errorf("trigger_comment_id = %v, want %v", persisted.TriggerCommentID, triggerCommentID)
	}
	if len(persisted.CoalescedCommentIds) != 1 || persisted.CoalescedCommentIds[0] != coalescedCommentID {
		t.Errorf("coalesced_comment_ids = %v, want [%v]", persisted.CoalescedCommentIds, coalescedCommentID)
	}
	if persisted.TriggerSummary.String != triggerContent || !persisted.TriggerSummary.Valid {
		t.Errorf("trigger_summary = %v, want %q", persisted.TriggerSummary, triggerContent)
	}
	if persisted.OriginatorUserID != userID || persisted.AccountableUserID != userID {
		t.Errorf("originator/accountable = %v/%v, want %v", persisted.OriginatorUserID, persisted.AccountableUserID, userID)
	}
	if persisted.OriginatorSource.String != "direct_human" || !persisted.OriginatorSource.Valid {
		t.Errorf("originator_source = %v, want direct_human", persisted.OriginatorSource)
	}
	if persisted.TriggerEvidenceKind.String != "comment" || persisted.TriggerEvidenceRefID != triggerCommentID {
		t.Errorf("trigger evidence = %v/%v, want comment/%v", persisted.TriggerEvidenceKind, persisted.TriggerEvidenceRefID, triggerCommentID)
	}
	if len(scheduler.calls) != 1 {
		t.Fatalf("allocator calls = %d, want 1", len(scheduler.calls))
	}
	if request := scheduler.calls[0]; request.WorkspaceID != workspaceID || request.FocusTaskID != task.ID {
		t.Fatalf("allocator request = %+v", request)
	}
}

func TestPoolEntryLeaderMentionPreservesSquadAndDelegationLineage(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queries := db.New(pool)

	seed := time.Now().UnixNano()
	var userID, workspaceID, agentID, issueID, parentTaskID, commentID, squadID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Pool Leader User', $1) RETURNING id
	`, fmt.Sprintf("pool-leader-%d@example.test", seed)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Pool Leader', $1, '', 'PLM') RETURNING id
	`, fmt.Sprintf("pool-leader-%d", seed)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks,
			owner_id, instructions, custom_env, custom_args,
			runtime_binding_mode, runtime_requirements
		) VALUES (
			$1, 'Pool Leader Agent', '', 'pool', '{}'::jsonb,
			NULL, 'private', 'private', 1, $2, '', '{}'::jsonb, '[]'::jsonb,
			'pool', '{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.test/v1"]}'::jsonb
		) RETURNING id
	`, workspaceID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create Pool Agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO issue (
			workspace_id, title, status, priority, creator_type, creator_id,
			position, number
		) VALUES ($1, 'Pool leader issue', 'backlog', 'urgent', 'member', $2, 0, 1)
		RETURNING id
	`, workspaceID, userID).Scan(&issueID); err != nil {
		t.Fatalf("create issue: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, status, completed_at, priority,
			originator_user_id, accountable_user_id, originator_source,
			runtime_binding_mode, runtime_requirements, placement_workspace_id,
			runtime_requester_user_id, session_affinity_state
		) VALUES (
			$1, $2, 'completed', now(), 0, $3, $3, 'direct_human',
			'pool', '{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.test/v1"]}'::jsonb,
			$4, $3, 'none'
		) RETURNING id
	`, agentID, issueID, userID, workspaceID).Scan(&parentTaskID); err != nil {
		t.Fatalf("create parent task: %v", err)
	}
	const commentContent = "leader should inherit this parent lineage"
	if err := pool.QueryRow(ctx, `
		INSERT INTO comment (
			issue_id, workspace_id, author_type, author_id, content, source_task_id
		) VALUES ($1, $2, 'agent', $3, $4, $5)
		RETURNING id
	`, issueID, workspaceID, agentID, commentContent, parentTaskID).Scan(&commentID); err != nil {
		t.Fatalf("create agent comment: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO squad (workspace_id, name, description, leader_id, creator_id)
		VALUES ($1, $2, '', $3, $4)
		RETURNING id
	`, workspaceID, fmt.Sprintf("Pool Leader Squad %d", seed), agentID, userID).Scan(&squadID); err != nil {
		t.Fatalf("create squad: %v", err)
	}

	scheduler := &poolEntryCommitScheduler{queries: queries}
	service := &TaskService{
		Queries:     queries,
		TxStarter:   pool,
		Bus:         events.New(),
		RuntimePool: scheduler,
	}
	task, err := service.EnqueueTaskForSquadLeader(ctx, db.Issue{
		ID:          issueID,
		WorkspaceID: workspaceID,
		Priority:    "urgent",
		CreatorType: "member",
		CreatorID:   userID,
	}, agentID, squadID, commentID)
	if err != nil {
		t.Fatalf("EnqueueTaskForSquadLeader: %v", err)
	}
	persisted, err := queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("load persisted leader task: %v", err)
	}
	if persisted.Status != runtimepool.StatusWaitingRuntime || persisted.RuntimeID.Valid ||
		persisted.PlacementWorkspaceID != workspaceID || persisted.RuntimeRequesterUserID != userID {
		t.Errorf("leader routing snapshot = %+v", persisted)
	}
	if !persisted.IsLeaderTask || persisted.SquadID != squadID {
		t.Errorf("leader/squad = %v/%v, want true/%v", persisted.IsLeaderTask, persisted.SquadID, squadID)
	}
	if persisted.TriggerCommentID != commentID || persisted.TriggerSummary.String != commentContent {
		t.Errorf("trigger payload = %v/%v, want %v/%q", persisted.TriggerCommentID, persisted.TriggerSummary, commentID, commentContent)
	}
	if persisted.OriginatorUserID != userID || persisted.AccountableUserID != userID ||
		persisted.OriginatorSource.String != "delegation" ||
		persisted.DelegatedFromTaskID != parentTaskID ||
		persisted.TriggerEvidenceKind.String != "comment" || persisted.TriggerEvidenceRefID != commentID {
		t.Errorf("delegation lineage = originator:%v accountable:%v source:%v parent:%v evidence:%v/%v",
			persisted.OriginatorUserID, persisted.AccountableUserID, persisted.OriginatorSource,
			persisted.DelegatedFromTaskID, persisted.TriggerEvidenceKind, persisted.TriggerEvidenceRefID)
	}
	if persisted.ParentTaskID.Valid {
		t.Errorf("parent_task_id = %v, want retry lineage unset", persisted.ParentTaskID)
	}
	if len(scheduler.calls) != 1 {
		t.Fatalf("allocator calls = %d, want 1", len(scheduler.calls))
	}
}

func TestPoolDeferredChannelIssuePersistsInCallerTransactionWithoutAllocator(t *testing.T) {
	fixture := newPoolAffinityFixture(t)
	const requirements = `{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.media/v1"]}`
	if _, err := fixture.tx.Exec(fixture.ctx, `
		UPDATE agent SET runtime_requirements = $2::jsonb WHERE id = $1
	`, fixture.agentID, requirements); err != nil {
		t.Fatalf("set Pool requirements: %v", err)
	}
	scheduler := &poolEntryCommitScheduler{queries: db.New(fixture.tx)}
	fixture.service.RuntimePool = scheduler
	fireAt := time.Now().UTC().Add(10 * time.Minute).Truncate(time.Microsecond)

	task, err := fixture.service.createDeferredChannelIssueTaskWithQueries(
		fixture.ctx,
		db.New(fixture.tx),
		db.Issue{
			ID:           fixture.issueID,
			WorkspaceID:  fixture.workspaceID,
			Priority:     "high",
			CreatorType:  "member",
			CreatorID:    fixture.userID,
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   fixture.agentID,
		},
		fireAt,
	)
	if err != nil {
		t.Fatalf("createDeferredChannelIssueTaskWithQueries: %v", err)
	}
	persisted, err := db.New(fixture.tx).GetAgentTask(fixture.ctx, task.ID)
	if err != nil {
		t.Fatalf("load deferred Pool task: %v", err)
	}
	if persisted.Status != "deferred" || persisted.RuntimeID.Valid ||
		persisted.RuntimeBindingMode != runtimepool.BindingPool ||
		persisted.PlacementWorkspaceID != fixture.workspaceID ||
		persisted.RuntimeRequesterUserID != fixture.userID ||
		persisted.SessionAffinityState != runtimepool.SessionAffinityNone ||
		persisted.WaitReason.String != "no_eligible_runtime" {
		t.Errorf("deferred routing snapshot = %+v", persisted)
	}
	if !persisted.FireAt.Valid || !persisted.FireAt.Time.Equal(fireAt) {
		t.Errorf("fire_at = %v, want %v", persisted.FireAt, fireAt)
	}
	if len(scheduler.calls) != 0 {
		t.Fatalf("allocator calls = %d, want 0 before deferred promotion", len(scheduler.calls))
	}
}

func TestPoolQuickCreatePersistsTypedWaitingTaskAndAssignsOnce(t *testing.T) {
	pool := pooltestdb.Open(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	queries := db.New(pool)

	seed := time.Now().UnixNano()
	var userID, workspaceID, agentID pgtype.UUID
	var projectID, squadID, parentIssueID, attachmentID pgtype.UUID
	if err := pool.QueryRow(ctx, `
		INSERT INTO "user" (name, email) VALUES ('Pool Quick Create User', $1) RETURNING id
	`, fmt.Sprintf("pool-qc-%d@example.test", seed)).Scan(&userID); err != nil {
		t.Fatalf("create user: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO workspace (name, slug, description, issue_prefix)
		VALUES ('Pool Quick Create', $1, '', 'PQC') RETURNING id
	`, fmt.Sprintf("pool-qc-%d", seed)).Scan(&workspaceID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	if _, err := pool.Exec(ctx, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, workspaceID, userID); err != nil {
		t.Fatalf("create member: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, description, runtime_mode, runtime_config,
			runtime_id, visibility, permission_mode, max_concurrent_tasks,
			owner_id, instructions, custom_env, custom_args,
			runtime_binding_mode, runtime_requirements
		) VALUES (
			$1, 'Pool Quick Create Agent', '', 'pool', '{}'::jsonb,
			NULL, 'private', 'private', 1, $2, '', '{}'::jsonb, '[]'::jsonb,
			'pool', '{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["multica.quick-create/v1"]}'::jsonb
		) RETURNING id
	`, workspaceID, userID).Scan(&agentID); err != nil {
		t.Fatalf("create Pool Agent: %v", err)
	}
	if err := pool.QueryRow(ctx, `
		SELECT gen_random_uuid(), gen_random_uuid(), gen_random_uuid(), gen_random_uuid()
	`).Scan(&projectID, &squadID, &parentIssueID, &attachmentID); err != nil {
		t.Fatalf("create context UUIDs: %v", err)
	}

	scheduler := &poolEntryCommitScheduler{queries: queries}
	service := &TaskService{
		Queries:     queries,
		TxStarter:   pool,
		Bus:         events.New(),
		RuntimePool: scheduler,
	}
	task, err := service.EnqueueQuickCreateTask(
		ctx,
		workspaceID,
		userID,
		agentID,
		squadID,
		"Create the typed Pool issue",
		"urgent",
		"2026-08-20",
		projectID,
		parentIssueID,
		[]pgtype.UUID{attachmentID},
	)
	if err != nil {
		t.Fatalf("EnqueueQuickCreateTask: %v", err)
	}
	persisted, err := queries.GetAgentTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("load Quick Create task: %v", err)
	}
	if persisted.Status != runtimepool.StatusWaitingRuntime || persisted.RuntimeID.Valid ||
		persisted.RuntimeBindingMode != runtimepool.BindingPool ||
		persisted.PlacementWorkspaceID != workspaceID || persisted.RuntimeRequesterUserID != userID ||
		persisted.SessionAffinityState != runtimepool.SessionAffinityNone ||
		persisted.WaitReason.String != "no_eligible_runtime" {
		t.Errorf("Quick Create routing snapshot = %+v", persisted)
	}
	quick, recognized, err := runtimepool.ParseQuickCreateContext(persisted.Context)
	if err != nil || !recognized {
		t.Fatalf("ParseQuickCreateContext = recognized:%v error:%v", recognized, err)
	}
	if quick.SchemaVersion != runtimepool.QuickCreateContextSchemaV1 ||
		quick.Prompt != "Create the typed Pool issue" || quick.RequesterID != util.UUIDToString(userID) ||
		quick.WorkspaceID != util.UUIDToString(workspaceID) || quick.Priority != "urgent" ||
		quick.DueDate != "2026-08-20" || quick.ProjectID != util.UUIDToString(projectID) ||
		quick.SquadID != util.UUIDToString(squadID) || quick.ParentIssueID != util.UUIDToString(parentIssueID) ||
		len(quick.AttachmentIDs) != 1 || quick.AttachmentIDs[0] != util.UUIDToString(attachmentID) {
		t.Errorf("Quick Create context = %+v", quick)
	}
	if len(scheduler.calls) != 1 {
		t.Fatalf("allocator calls = %d, want 1", len(scheduler.calls))
	}
}
