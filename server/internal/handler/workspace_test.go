package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/runtimepool"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func createRuntimePoolMembershipTestUser(t *testing.T, fixture *runtimeCapabilityFixture) (string, string) {
	t.Helper()
	email := fmt.Sprintf("runtime-pool-member-%d@example.test", time.Now().UnixNano())
	var userID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO "user" (name, email) VALUES ('Runtime Pool Member', $1) RETURNING id
	`, email).Scan(&userID); err != nil {
		t.Fatalf("create membership test user: %v", err)
	}
	return userID, email
}

func TestRuntimePoolWakeAfterMemberAccessImprovement(t *testing.T) {
	t.Run("member added", func(t *testing.T) {
		fixture := newRuntimeCapabilityFixture(t)
		_, email := createRuntimePoolMembershipTestUser(t, fixture)
		req := newRequestAs(fixture.ownerID, http.MethodPost, "/api/workspaces/"+fixture.workspaceID+"/members", CreateMemberRequest{
			Email: email,
			Role:  "member",
		})
		req = withURLParam(req, "id", fixture.workspaceID)
		response := httptest.NewRecorder()

		fixture.handler.CreateMember(response, req)
		if response.Code != http.StatusCreated {
			t.Fatalf("CreateMember status = %d, want %d: %s", response.Code, http.StatusCreated, response.Body.String())
		}
		if len(fixture.wake.requests) != 1 {
			t.Fatalf("Workspace wakes = %d, want 1", len(fixture.wake.requests))
		}
	})

	tests := []struct {
		name      string
		fromRole  string
		toRole    string
		wantWakes int
	}{
		{name: "member to admin restores private Runtime access", fromRole: "member", toRole: "admin", wantWakes: 1},
		{name: "same role", fromRole: "member", toRole: "member", wantWakes: 0},
		{name: "admin to owner has equivalent Runtime access", fromRole: "admin", toRole: "owner", wantWakes: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newRuntimeCapabilityFixture(t)
			userID, _ := createRuntimePoolMembershipTestUser(t, fixture)
			var memberID string
			if err := fixture.tx.QueryRow(fixture.ctx, `
				INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, $3) RETURNING id
			`, fixture.workspaceID, userID, test.fromRole).Scan(&memberID); err != nil {
				t.Fatalf("create target member: %v", err)
			}
			req := newRequestAs(fixture.ownerID, http.MethodPatch, "/api/workspaces/"+fixture.workspaceID+"/members/"+memberID, UpdateMemberRequest{Role: test.toRole})
			req = withURLParams(req, "id", fixture.workspaceID, "memberId", memberID)
			response := httptest.NewRecorder()

			fixture.handler.UpdateMember(response, req)
			if response.Code != http.StatusOK {
				t.Fatalf("UpdateMember status = %d, want %d: %s", response.Code, http.StatusOK, response.Body.String())
			}
			if len(fixture.wake.requests) != test.wantWakes {
				t.Fatalf("Workspace wakes = %d, want %d", len(fixture.wake.requests), test.wantWakes)
			}
		})
	}
}

// Break caught: UpdateMember reads the target before it mutates the role. A
// concurrent role change can therefore turn a seemingly neutral write into an
// access reduction. The stale request must fail closed instead of bypassing
// the Member-locked downgrade path.
func TestRuntimeMemberRoleSnapshotDriftFailsClosed(t *testing.T) {
	for _, test := range []struct {
		name           string
		initialRole    string
		concurrentRole string
		desiredRole    string
		blockedQuery   string
	}{
		{
			name:           "ordinary update becomes downgrade",
			initialRole:    "member",
			concurrentRole: "admin",
			desiredRole:    "member",
			blockedQuery:   "UpdateMemberRole",
		},
		{
			name:           "safe downgrade snapshot changes under Member lock",
			initialRole:    "admin",
			concurrentRole: "owner",
			desiredRole:    "member",
			blockedQuery:   "LockPoolPlacementMember",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			suffix := fmt.Sprintf("%d", time.Now().UnixNano())
			fixture := setupRevocationFixture(t, "handler-tests-role-drift-"+suffix, "daemon-role-drift-"+suffix)
			ctx := context.Background()
			if _, err := testPool.Exec(ctx, `UPDATE member SET role = $1 WHERE id = $2`, test.initialRole, fixture.MemberID); err != nil {
				t.Fatalf("set initial role: %v", err)
			}

			blocker, err := testPool.Begin(ctx)
			if err != nil {
				t.Fatalf("begin concurrent role update: %v", err)
			}
			defer blocker.Rollback(ctx)
			if _, err := blocker.Exec(ctx, `UPDATE member SET role = $1 WHERE id = $2`, test.concurrentRole, fixture.MemberID); err != nil {
				t.Fatalf("stage concurrent role update: %v", err)
			}
			var blockerPID int32
			if err := blocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
				t.Fatalf("read concurrent updater PID: %v", err)
			}

			responseDone := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				response := httptest.NewRecorder()
				req := newRequestAs(testUserID, http.MethodPatch,
					"/api/workspaces/"+fixture.WorkspaceID+"/members/"+fixture.MemberID,
					UpdateMemberRequest{Role: test.desiredRole})
				req = withURLParams(req, "id", fixture.WorkspaceID, "memberId", fixture.MemberID)
				testHandler.UpdateMember(response, req)
				responseDone <- response
			}()

			deadline := time.Now().Add(5 * time.Second)
			blocked := false
			for time.Now().Before(deadline) {
				if err := testPool.QueryRow(ctx, `
					SELECT EXISTS (
						SELECT 1 FROM pg_stat_activity AS activity
						WHERE $1 = ANY(pg_blocking_pids(activity.pid))
						  AND activity.wait_event_type = 'Lock'
						  AND activity.query LIKE '%' || $2 || '%'
					)
				`, blockerPID, test.blockedQuery).Scan(&blocked); err != nil {
					t.Fatalf("inspect blocked role update: %v", err)
				}
				if blocked {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !blocked {
				t.Fatalf("UpdateMember did not reach %s lock barrier", test.blockedQuery)
			}
			if err := blocker.Commit(ctx); err != nil {
				t.Fatalf("commit concurrent role update: %v", err)
			}

			select {
			case response := <-responseDone:
				if response.Code != http.StatusConflict {
					t.Fatalf("UpdateMember status = %d, want 409 after snapshot drift: %s", response.Code, response.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("UpdateMember did not finish after releasing Member lock")
			}
			var finalRole string
			if err := testPool.QueryRow(ctx, `SELECT role FROM member WHERE id = $1`, fixture.MemberID).Scan(&finalRole); err != nil {
				t.Fatalf("read final role: %v", err)
			}
			if finalRole != test.concurrentRole {
				t.Fatalf("final role = %q, want concurrent role %q preserved", finalRole, test.concurrentRole)
			}
		})
	}
}

func TestRuntimeMemberRevokeCancelsWithout409(t *testing.T) {
	fixture := newRuntimeCapabilityFixture(t)
	targetUserID := fixture.createMember("member")
	targetMember, err := fixture.handler.Queries.GetMemberByUserAndWorkspace(fixture.ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(targetUserID),
		WorkspaceID: parseUUID(fixture.workspaceID),
	})
	if err != nil {
		t.Fatalf("get target member: %v", err)
	}
	runtimeID := fixture.createRuntime("custom", "runtime-pool-member-revoke", fixture.ownerID, "public", []string{"custom/v1"}, "")
	taskID := fixture.createPoolTask(runtimeID, targetUserID, "custom/v1", "queued", runtimepool.SessionAffinityNone)
	nonterminalTaskIDs := map[string]string{"queued": taskID}
	for _, status := range []string{"waiting_runtime", "deferred", "dispatched", "running", "waiting_local_directory"} {
		var assignedRuntime any = runtimeID
		if status == "waiting_runtime" || status == "deferred" {
			assignedRuntime = nil
		}
		var dependentTaskID string
		if err := fixture.tx.QueryRow(fixture.ctx, `
			INSERT INTO agent_task_queue (
				agent_id, issue_id, runtime_id, status, runtime_binding_mode,
				runtime_requirements, placement_workspace_id, runtime_requester_user_id,
				session_affinity_state, session_affinity_runtime_id, wait_reason, fire_at
			)
			SELECT agent_id, NULL, $2, $3, 'pool', runtime_requirements, placement_workspace_id,
			       runtime_requester_user_id, 'none', NULL,
			       CASE WHEN $3 IN ('waiting_runtime', 'deferred') THEN 'no_eligible_runtime' END,
			       CASE WHEN $3 = 'deferred' THEN now() + interval '1 hour' END
			FROM agent_task_queue WHERE id = $1
			RETURNING id
		`, taskID, assignedRuntime, status).Scan(&dependentTaskID); err != nil {
			t.Fatalf("create %s requester Pool task: %v", status, err)
		}
		nonterminalTaskIDs[status] = dependentTaskID
	}

	baseTask := fixture.getRuntimePoolTask(taskID)
	var chatSessionID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id)
		VALUES ($1, $2, $3, 'member revoke unresolved', $4)
		RETURNING id
	`, fixture.workspaceID, uuidToString(baseTask.AgentID), targetUserID, runtimeID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create requester Chat Session: %v", err)
	}
	var unresolvedTaskID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id, wait_reason
		)
		SELECT agent_id, $2, NULL, 'waiting_runtime', 'pool', runtime_requirements,
		       placement_workspace_id, runtime_requester_user_id,
		       'unresolved', NULL, 'chat_predecessor_pending'
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, taskID, chatSessionID).Scan(&unresolvedTaskID); err != nil {
		t.Fatalf("create unresolved requester Pool task: %v", err)
	}
	nonterminalTaskIDs["unresolved"] = unresolvedTaskID

	var completedTaskID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO agent_task_queue (
			agent_id, issue_id, runtime_id, status, completed_at, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id
		)
		SELECT agent_id, NULL, runtime_id, 'completed', now(), 'pool', runtime_requirements,
		       placement_workspace_id, runtime_requester_user_id, 'none', NULL
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, taskID).Scan(&completedTaskID); err != nil {
		t.Fatalf("create terminal requester Pool task: %v", err)
	}

	response := httptest.NewRecorder()
	req := newRequestAs(fixture.ownerID, http.MethodDelete, "/api/workspaces/"+fixture.workspaceID+"/members/"+uuidToString(targetMember.ID), nil)
	req = withURLParams(req, "id", fixture.workspaceID, "memberId", uuidToString(targetMember.ID))
	fixture.handler.DeleteMember(response, req)

	if response.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember status = %d, want %d without 409: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	for status, dependentTaskID := range nonterminalTaskIDs {
		cancelled, taskErr := fixture.handler.Queries.GetAgentTask(fixture.ctx, parseUUID(dependentTaskID))
		if taskErr != nil {
			t.Fatalf("get %s requester Pool task: %v", status, taskErr)
		}
		if cancelled.Status != "cancelled" {
			t.Fatalf("%s requester Pool task status = %q, want cancelled", status, cancelled.Status)
		}
		if status == "unresolved" {
			if cancelled.SessionAffinityState != runtimepool.SessionAffinityNone || cancelled.SessionAffinityRuntimeID.Valid || cancelled.WaitReason.Valid {
				t.Fatalf("unresolved cancellation affinity = (%q, %v, %v), want none/NULL/NULL", cancelled.SessionAffinityState, cancelled.SessionAffinityRuntimeID, cancelled.WaitReason)
			}
		}
	}
	completed := fixture.getRuntimePoolTask(completedTaskID)
	if completed.Status != "completed" {
		t.Fatalf("terminal requester Pool task status = %q, want completed", completed.Status)
	}
	if len(fixture.wake.requests) != 1 {
		t.Fatalf("Workspace wakes = %d, want 1 after committed revocation", len(fixture.wake.requests))
	}
}

func TestRuntimeMemberRoleDowngradeCancelsOnlyNewlyUnauthorized(t *testing.T) {
	fixture := newRuntimeCapabilityFixture(t)
	targetUserID := fixture.createMember("admin")
	targetMember, err := fixture.handler.Queries.GetMemberByUserAndWorkspace(fixture.ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(targetUserID),
		WorkspaceID: parseUUID(fixture.workspaceID),
	})
	if err != nil {
		t.Fatalf("get target member: %v", err)
	}
	privateOtherRuntime := fixture.createRuntime("custom", "runtime-pool-downgrade-private", fixture.ownerID, "private", []string{"custom/v1"}, "")
	publicOtherRuntime := fixture.createRuntime("custom", "runtime-pool-downgrade-public", fixture.ownerID, "public", []string{"custom/v1"}, "")
	ownedRuntime := fixture.createRuntime("custom", "runtime-pool-downgrade-owned", targetUserID, "private", []string{"custom/v1"}, "")
	privateTaskID := fixture.createPoolTask(privateOtherRuntime, targetUserID, "custom/v1", "running", runtimepool.SessionAffinityNone)
	publicTaskID := fixture.createPoolTask(publicOtherRuntime, targetUserID, "custom/v1", "queued", runtimepool.SessionAffinityNone)
	ownedTaskID := fixture.createPoolTask(ownedRuntime, targetUserID, "custom/v1", "queued", runtimepool.SessionAffinityNone)

	response := httptest.NewRecorder()
	req := newRequestAs(fixture.ownerID, http.MethodPatch, "/api/workspaces/"+fixture.workspaceID+"/members/"+uuidToString(targetMember.ID), UpdateMemberRequest{Role: "member"})
	req = withURLParams(req, "id", fixture.workspaceID, "memberId", uuidToString(targetMember.ID))
	fixture.handler.UpdateMember(response, req)

	if response.Code != http.StatusOK {
		t.Fatalf("UpdateMember status = %d, want %d without 409: %s", response.Code, http.StatusOK, response.Body.String())
	}
	assertTaskStatus := func(taskID, want string) {
		t.Helper()
		task, taskErr := fixture.handler.Queries.GetAgentTask(fixture.ctx, parseUUID(taskID))
		if taskErr != nil {
			t.Fatalf("get Task %s: %v", taskID, taskErr)
		}
		if task.Status != want {
			t.Fatalf("Task %s status = %q, want %q", taskID, task.Status, want)
		}
	}
	assertTaskStatus(privateTaskID, "cancelled")
	assertTaskStatus(publicTaskID, "queued")
	assertTaskStatus(ownedTaskID, "queued")
	updatedMember, err := fixture.handler.Queries.GetMember(fixture.ctx, targetMember.ID)
	if err != nil {
		t.Fatalf("get downgraded member: %v", err)
	}
	if updatedMember.Role != "member" {
		t.Fatalf("member role = %q, want member", updatedMember.Role)
	}
	if len(fixture.wake.requests) != 1 {
		t.Fatalf("Workspace wakes = %d, want 1 after one committed cancellation", len(fixture.wake.requests))
	}
}

func TestRuntimeMemberRevokeAdvancesOneAuthorizedChatTail(t *testing.T) {
	fixture := newRuntimeCapabilityFixture(t)
	var waitingEvents []events.Event
	fixture.handler.TaskService.Bus.Subscribe(protocol.EventTaskWaitingRuntime, func(event events.Event) {
		waitingEvents = append(waitingEvents, event)
	})
	targetUserID := fixture.createMember("member")
	otherUserID := fixture.createMember("member")
	targetMember, err := fixture.handler.Queries.GetMemberByUserAndWorkspace(fixture.ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(targetUserID),
		WorkspaceID: parseUUID(fixture.workspaceID),
	})
	if err != nil {
		t.Fatalf("get target member: %v", err)
	}
	runtimeID := fixture.createRuntime("custom", "runtime-pool-member-revoke-chat", fixture.ownerID, "public", []string{"custom/v1"}, "")
	headTaskID := fixture.createPoolTask(runtimeID, targetUserID, "custom/v1", "queued", runtimepool.SessionAffinityPinned)
	headTask := fixture.getRuntimePoolTask(headTaskID)
	var chatSessionID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, session_id)
		VALUES ($1, $2, $3, 'member revoke head', $4, 'session-before-revoke')
		RETURNING id
	`, fixture.workspaceID, uuidToString(headTask.AgentID), targetUserID, runtimeID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create Chat Session: %v", err)
	}
	if _, err := fixture.tx.Exec(fixture.ctx, `
		UPDATE agent_task_queue SET issue_id = NULL, chat_session_id = $2 WHERE id = $1
	`, headTaskID, chatSessionID); err != nil {
		t.Fatalf("bind resolved head to Chat Session: %v", err)
	}

	tailIDs := make([]string, 0, 2)
	for _, priority := range []int{10, 20} {
		var tailID string
		if err := fixture.tx.QueryRow(fixture.ctx, `
			INSERT INTO agent_task_queue (
				agent_id, chat_session_id, runtime_id, status, priority, runtime_binding_mode,
				runtime_requirements, placement_workspace_id, runtime_requester_user_id,
				session_affinity_state, session_affinity_runtime_id, wait_reason
			)
			SELECT agent_id, $2, NULL, 'waiting_runtime', $3, 'pool', runtime_requirements,
			       placement_workspace_id, $4, 'unresolved', NULL, 'chat_predecessor_pending'
			FROM agent_task_queue WHERE id = $1
			RETURNING id
		`, headTaskID, chatSessionID, priority, otherUserID).Scan(&tailID); err != nil {
			t.Fatalf("create unresolved Chat tail: %v", err)
		}
		tailIDs = append(tailIDs, tailID)
	}

	response := httptest.NewRecorder()
	req := newRequestAs(fixture.ownerID, http.MethodDelete, "/api/workspaces/"+fixture.workspaceID+"/members/"+uuidToString(targetMember.ID), nil)
	req = withURLParams(req, "id", fixture.workspaceID, "memberId", uuidToString(targetMember.ID))
	fixture.handler.DeleteMember(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	if head := fixture.getRuntimePoolTask(headTaskID); head.Status != "cancelled" {
		t.Fatalf("resolved head status = %q, want cancelled", head.Status)
	}
	highPriorityTail := fixture.getRuntimePoolTask(tailIDs[1])
	if highPriorityTail.Status != "waiting_runtime" || highPriorityTail.SessionAffinityState != runtimepool.SessionAffinityPinned || uuidToString(highPriorityTail.SessionAffinityRuntimeID) != runtimeID {
		t.Fatalf("high-priority tail = status %q affinity (%q,%q), want waiting_runtime pinned %q", highPriorityTail.Status, highPriorityTail.SessionAffinityState, uuidToString(highPriorityTail.SessionAffinityRuntimeID), runtimeID)
	}
	lowPriorityTail := fixture.getRuntimePoolTask(tailIDs[0])
	if lowPriorityTail.SessionAffinityState != runtimepool.SessionAffinityUnresolved || !lowPriorityTail.WaitReason.Valid || lowPriorityTail.WaitReason.String != "chat_predecessor_pending" {
		t.Fatalf("lower tail affinity = (%q,%v), want unresolved/chat_predecessor_pending", lowPriorityTail.SessionAffinityState, lowPriorityTail.WaitReason)
	}
	if len(fixture.wake.requests) != 1 {
		t.Fatalf("Workspace allocator calls = %d, want exactly 1 from committed head cancellation", len(fixture.wake.requests))
	}
	if len(waitingEvents) != 1 || waitingEvents[0].TaskID != tailIDs[1] {
		t.Fatalf("waiting_runtime events = %+v, want promoted tail %s exactly once", waitingEvents, tailIDs[1])
	}
}

func TestRuntimeMemberRevokePromotedDeferredChatTailStaysSilent(t *testing.T) {
	fixture := newRuntimeCapabilityFixture(t)
	var waitingEvents []events.Event
	fixture.handler.TaskService.Bus.Subscribe(protocol.EventTaskWaitingRuntime, func(event events.Event) {
		waitingEvents = append(waitingEvents, event)
	})
	targetUserID := fixture.createMember("member")
	otherUserID := fixture.createMember("member")
	targetMember, err := fixture.handler.Queries.GetMemberByUserAndWorkspace(fixture.ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(targetUserID),
		WorkspaceID: parseUUID(fixture.workspaceID),
	})
	if err != nil {
		t.Fatalf("get target member: %v", err)
	}
	runtimeID := fixture.createRuntime("custom", "runtime-pool-member-revoke-deferred-chat", fixture.ownerID, "public", []string{"custom/v1"}, "")
	headTaskID := fixture.createPoolTask(runtimeID, targetUserID, "custom/v1", "queued", runtimepool.SessionAffinityPinned)
	headTask := fixture.getRuntimePoolTask(headTaskID)
	var chatSessionID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, session_id)
		VALUES ($1, $2, $3, 'member revoke deferred head', $4, 'session-before-deferred-revoke')
		RETURNING id
	`, fixture.workspaceID, uuidToString(headTask.AgentID), targetUserID, runtimeID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create Chat Session: %v", err)
	}
	if _, err := fixture.tx.Exec(fixture.ctx, `
		UPDATE agent_task_queue SET issue_id = NULL, chat_session_id = $2 WHERE id = $1
	`, headTaskID, chatSessionID); err != nil {
		t.Fatalf("bind resolved head to Chat Session: %v", err)
	}
	var deferredTailID string
	if err := fixture.tx.QueryRow(fixture.ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, fire_at, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id, wait_reason
		)
		SELECT agent_id, $2, NULL, 'deferred', now() + interval '1 hour', 'pool',
		       runtime_requirements, placement_workspace_id, $3,
		       'unresolved', NULL, 'chat_predecessor_pending'
		FROM agent_task_queue WHERE id = $1
		RETURNING id
	`, headTaskID, chatSessionID, otherUserID).Scan(&deferredTailID); err != nil {
		t.Fatalf("create unresolved deferred Chat tail: %v", err)
	}

	response := httptest.NewRecorder()
	req := newRequestAs(fixture.ownerID, http.MethodDelete, "/api/workspaces/"+fixture.workspaceID+"/members/"+uuidToString(targetMember.ID), nil)
	req = withURLParams(req, "id", fixture.workspaceID, "memberId", uuidToString(targetMember.ID))
	fixture.handler.DeleteMember(response, req)
	if response.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember status = %d, want %d: %s", response.Code, http.StatusNoContent, response.Body.String())
	}
	deferredTail := fixture.getRuntimePoolTask(deferredTailID)
	if deferredTail.Status != "deferred" || deferredTail.SessionAffinityState != runtimepool.SessionAffinityPinned || uuidToString(deferredTail.SessionAffinityRuntimeID) != runtimeID {
		t.Fatalf("deferred tail = status %q affinity (%q,%q), want deferred pinned %q", deferredTail.Status, deferredTail.SessionAffinityState, uuidToString(deferredTail.SessionAffinityRuntimeID), runtimeID)
	}
	if len(waitingEvents) != 0 {
		t.Fatalf("deferred tail waiting_runtime events = %+v, want none", waitingEvents)
	}
	if len(fixture.wake.requests) != 1 {
		t.Fatalf("Workspace allocator calls = %d, want only the committed cancellation wake", len(fixture.wake.requests))
	}
}

func TestRuntimeMemberConcurrentRevokeLeavesNoUnauthorizedChatTail(t *testing.T) {
	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	fixture := setupRevocationFixture(t, "handler-tests-revoke-chat-race-"+suffix, "daemon-revoke-chat-race-"+suffix)
	ctx := context.Background()

	var otherUserID, otherMemberID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO "user" (name, email)
		VALUES ('Concurrent Tail Owner', $1)
		RETURNING id
	`, "concurrent-tail-"+suffix+"@example.test").Scan(&otherUserID); err != nil {
		t.Fatalf("create concurrent tail owner: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO member (workspace_id, user_id, role)
		VALUES ($1, $2, 'member')
		RETURNING id
	`, fixture.WorkspaceID, otherUserID).Scan(&otherMemberID); err != nil {
		t.Fatalf("create concurrent tail membership: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, fixture.WorkspaceID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, otherUserID)
	})

	var sharedRuntimeID, sharedAgentID, chatSessionID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_runtime (
			workspace_id, daemon_id, name, runtime_mode, provider, status,
			device_info, metadata, owner_id, visibility, capabilities, last_seen_at
		) VALUES ($1, $2, 'Concurrent shared Runtime', 'local', 'custom', 'online',
		          '', '{}'::jsonb, $3, 'public', ARRAY['custom/v1'], now())
		RETURNING id
	`, fixture.WorkspaceID, "daemon-revoke-shared-"+suffix, testUserID).Scan(&sharedRuntimeID); err != nil {
		t.Fatalf("create shared Runtime: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config, runtime_id,
			visibility, max_concurrent_tasks, owner_id
		) VALUES ($1, 'Concurrent revoke agent', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
		RETURNING id
	`, fixture.WorkspaceID, sharedRuntimeID, testUserID).Scan(&sharedAgentID); err != nil {
		t.Fatalf("create shared Agent: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO chat_session (workspace_id, agent_id, creator_id, title, runtime_id, session_id)
		VALUES ($1, $2, $3, 'Concurrent member revoke', $4, 'session-before-concurrent-revoke')
		RETURNING id
	`, fixture.WorkspaceID, sharedAgentID, fixture.TargetUserID, sharedRuntimeID).Scan(&chatSessionID); err != nil {
		t.Fatalf("create concurrent Chat Session: %v", err)
	}
	var headTaskID, tailTaskID string
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id
		) VALUES (
			$1, $2, $3, 'queued', 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["custom/v1"]}'::jsonb,
			$4, $5, 'pinned', $3
		)
		RETURNING id
	`, sharedAgentID, chatSessionID, sharedRuntimeID, fixture.WorkspaceID, fixture.TargetUserID).Scan(&headTaskID); err != nil {
		t.Fatalf("create concurrent resolved head: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
		INSERT INTO agent_task_queue (
			agent_id, chat_session_id, runtime_id, status, runtime_binding_mode,
			runtime_requirements, placement_workspace_id, runtime_requester_user_id,
			session_affinity_state, session_affinity_runtime_id, wait_reason
		) VALUES (
			$1, $2, NULL, 'waiting_runtime', 'pool',
			'{"schema_version":"multica.runtime-requirements/v1","capabilities_all":["custom/v1"]}'::jsonb,
			$3, $4, 'unresolved', NULL, 'chat_predecessor_pending'
		)
		RETURNING id
	`, sharedAgentID, chatSessionID, fixture.WorkspaceID, otherUserID).Scan(&tailTaskID); err != nil {
		t.Fatalf("create concurrent unresolved tail: %v", err)
	}

	chatBlocker, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin Chat blocker: %v", err)
	}
	defer chatBlocker.Rollback(ctx)
	if _, err := chatBlocker.Exec(ctx, `SELECT id FROM chat_session WHERE id = $1 FOR UPDATE`, chatSessionID); err != nil {
		t.Fatalf("lock Chat Session: %v", err)
	}
	var blockerPID int32
	if err := chatBlocker.QueryRow(ctx, `SELECT pg_backend_pid()`).Scan(&blockerPID); err != nil {
		t.Fatalf("read Chat blocker PID: %v", err)
	}

	type revokeOutcome struct {
		result revocationResult
		err    error
	}
	targetDone := make(chan revokeOutcome, 1)
	go func() {
		result, revokeErr := testHandler.revokeAndRemoveMember(
			context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(fixture.TargetUserID),
			parseUUID(fixture.MemberID), parseUUID(testUserID),
		)
		targetDone <- revokeOutcome{result: result, err: revokeErr}
	}()

	waitForBlockedQuery := func(t *testing.T, blocker int32, queryName string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var waiting bool
			if err := testPool.QueryRow(ctx, `
				SELECT EXISTS (
					SELECT 1 FROM pg_stat_activity AS activity
					WHERE $1 = ANY(pg_blocking_pids(activity.pid))
					  AND activity.wait_event_type = 'Lock'
					  AND activity.query LIKE '%' || $2 || '%'
				)
			`, blocker, queryName).Scan(&waiting); err != nil {
				t.Fatalf("inspect blocked %s: %v", queryName, err)
			}
			if waiting {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s did not reach the deterministic lock barrier", queryName)
	}
	waitForBlockedQuery(t, blockerPID, "LockPoolChatSessionForPlacement")

	otherDone := make(chan revokeOutcome, 1)
	go func() {
		result, revokeErr := testHandler.revokeAndRemoveMember(
			context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(otherUserID),
			parseUUID(otherMemberID), parseUUID(testUserID),
		)
		otherDone <- revokeOutcome{result: result, err: revokeErr}
	}()

	deadline := time.Now().Add(5 * time.Second)
	otherWaitingOnOwnerWrites := false
	for time.Now().Before(deadline) {
		if err := testPool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity AS activity
				WHERE activity.wait_event_type = 'Lock'
				  AND activity.query LIKE '%LockRuntimeOwnerWrites%'
			)
		`).Scan(&otherWaitingOnOwnerWrites); err != nil {
			t.Fatalf("inspect concurrent Runtime owner-write wait: %v", err)
		}
		if otherWaitingOnOwnerWrites {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !otherWaitingOnOwnerWrites {
		t.Fatal("tail-owner revocation did not wait on the first revocation's Runtime owner-write barrier")
	}

	if err := chatBlocker.Commit(ctx); err != nil {
		t.Fatalf("release Chat blocker: %v", err)
	}
	for name, done := range map[string]<-chan revokeOutcome{"head owner": targetDone, "tail owner": otherDone} {
		select {
		case outcome := <-done:
			if outcome.err != nil {
				t.Fatalf("%s revocation: %v", name, outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s revocation did not finish", name)
		}
	}

	for name, taskID := range map[string]string{"head": headTaskID, "tail": tailTaskID} {
		var status string
		if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status); err != nil {
			t.Fatalf("read %s Task: %v", name, err)
		}
		if status != "cancelled" {
			t.Fatalf("%s Task status = %q, want cancelled", name, status)
		}
	}
	var unauthorizedActive int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*)
		FROM agent_task_queue AS task
		WHERE task.placement_workspace_id = $1
		  AND task.runtime_binding_mode = 'pool'
		  AND task.status IN ('waiting_runtime', 'queued', 'deferred', 'dispatched', 'running', 'waiting_local_directory')
		  AND NOT EXISTS (
			SELECT 1 FROM member
			WHERE member.workspace_id = task.placement_workspace_id
			  AND member.user_id = task.runtime_requester_user_id
		  )
	`, fixture.WorkspaceID).Scan(&unauthorizedActive); err != nil {
		t.Fatalf("count unauthorized active Pool Tasks: %v", err)
	}
	if unauthorizedActive != 0 {
		t.Fatalf("unauthorized active Pool Tasks = %d, want 0", unauthorizedActive)
	}
}

func TestCreateWorkspace_RejectsReservedSlug(t *testing.T) {
	// Drive the test off the actual reservedSlugs map so the test can never
	// drift from the source of truth. New entries are covered automatically.
	reserved := make([]string, 0, len(reservedSlugs))
	for slug := range reservedSlugs {
		reserved = append(reserved, slug)
	}
	sort.Strings(reserved) // deterministic test order

	for _, slug := range reserved {
		t.Run(slug, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := newRequest("POST", "/api/workspaces", map[string]any{
				"name": fmt.Sprintf("Test %s", slug),
				"slug": slug,
			})
			testHandler.CreateWorkspace(w, req)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("slug %q: expected 400, got %d: %s", slug, w.Code, w.Body.String())
			}
		})
	}
}

// TestCreateWorkspace_DoesNotMarkOnboarded guards the onboarding
// contract: creating a workspace MUST leave user.onboarded_at NULL so
// the route guard in apps/web/app/[workspaceSlug]/layout.tsx (and the
// desktop App.tsx overlay decision) can redirect the un-onboarded user
// back to /onboarding to finish Step 3. The previous behavior atomically
// set onboarded_at inside CreateWorkspace; this test makes the new
// invariant explicit and regression-protected.
//
// CompleteOnboarding (Step 3 exit) and AcceptInvitation are the only
// remaining handlers that flip onboarded_at.
func TestCreateWorkspace_DoesNotMarkOnboarded(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const slug = "handler-tests-onboarded-null"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	// Ensure the test user starts un-onboarded so the assertion is meaningful.
	_, _ = testPool.Exec(ctx, `UPDATE "user" SET onboarded_at = NULL WHERE id = $1`, testUserID)

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
		_, _ = testPool.Exec(context.Background(), `UPDATE "user" SET onboarded_at = NULL WHERE id = $1`, testUserID)
	})

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Onboarding Invariant Probe",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("CreateWorkspace: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var onboardedAt *string
	if err := testPool.QueryRow(ctx, `SELECT onboarded_at FROM "user" WHERE id = $1`, testUserID).Scan(&onboardedAt); err != nil {
		t.Fatalf("lookup user: %v", err)
	}
	if onboardedAt != nil {
		t.Fatalf("CreateWorkspace marked user as onboarded; expected NULL, got %q. The workspace layout hard gate relies on this staying NULL until Step 3 CompleteOnboarding fires.", *onboardedAt)
	}
}

// TestCreateWorkspace_DisabledByConfig guards the self-host gate added by
// #3433: when DisableWorkspaceCreation is true on the handler config, every
// caller — even an already-authenticated user — must receive 403 and the
// workspace row must not be written.
func TestCreateWorkspace_DisabledByConfig(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const slug = "handler-tests-disabled-create"
	ctx := context.Background()
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE slug = $1`, slug)
	})

	prev := testHandler.cfg
	testHandler.cfg = Config{
		AllowSignup:              prev.AllowSignup,
		DisableWorkspaceCreation: true,
	}
	t.Cleanup(func() { testHandler.cfg = prev })

	w := httptest.NewRecorder()
	req := newRequest("POST", "/api/workspaces", map[string]any{
		"name": "Disabled Create",
		"slug": slug,
	})
	testHandler.CreateWorkspace(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("CreateWorkspace: expected 403 with flag on, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM workspace WHERE slug = $1`, slug).Scan(&count); err != nil {
		t.Fatalf("count workspaces: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected no workspace row to be written when gate fires, found %d", count)
	}
}

// TestDeleteWorkspace_RequiresOwner exercises the in-handler authorization
// added to DeleteWorkspace by calling the handler directly (bypassing the
// router-level RequireWorkspaceRoleFromURL middleware). Without the handler
// check, a non-owner member request would reach DeleteWorkspace and erase the
// workspace; with it, the handler must return 403 and leave the workspace
// intact.
func TestDeleteWorkspace_RequiresOwner(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-403"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete 403", slug, "DeleteWorkspace handler permission test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'admin')
`, wsID, testUserID); err != nil {
		t.Fatalf("create admin member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("expected 403 from DeleteWorkspace handler for admin (non-owner), got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if !exists {
		t.Fatal("workspace was deleted despite non-owner request — handler-level check did not fire")
	}
}

// TestDeleteWorkspace_OwnerSucceeds is the positive counterpart: an owner
// calling DeleteWorkspace directly must succeed (204) and the workspace must
// be gone. This guards the handler check against being too strict.
func TestDeleteWorkspace_OwnerSucceeds(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-delete-ok"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Delete OK", slug, "DeleteWorkspace handler owner test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO github_pending_check_suite (
	workspace_id, installation_id, repo_owner, repo_name, pr_number,
	suite_id, head_sha, app_id, status, suite_updated_at
)
VALUES ($1, 123456789, 'multica-ai', 'multica', 3366, 987654321, 'abc123', 15368, 'completed', now())
`, wsID); err != nil {
		t.Fatalf("create pending check suite: %v", err)
	}
	var githubPRID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO github_pull_request (
	workspace_id, installation_id, repo_owner, repo_name, pr_number,
	title, state, html_url, pr_created_at, pr_updated_at, head_sha
)
VALUES ($1, 123456789, 'multica-ai', 'multica', 5265,
	'Workspace cleanup snapshot', 'open', 'https://github.com/multica-ai/multica/pull/5265',
	now(), now(), 'head-a')
RETURNING id
`, wsID).Scan(&githubPRID); err != nil {
		t.Fatalf("create github PR snapshot parent: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO github_pull_request_check_run (
	pr_id, head_sha, ordinal, name, status, conclusion, is_status_context
)
VALUES ($1, 'head-a', 0, 'backend', 'completed', 'success', false)
`, githubPRID); err != nil {
		t.Fatalf("create github PR check run: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request_check_run WHERE pr_id = $1`, githubPRID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM github_pull_request WHERE id = $1`, githubPRID)
	})
	var propertyID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue_property (workspace_id, name, type)
VALUES ($1, 'Delete cleanup property', 'text')
RETURNING id
`, wsID).Scan(&propertyID); err != nil {
		t.Fatalf("create issue property: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue_property WHERE id = $1`, propertyID)
	})

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
	workspace_id, name, runtime_mode, provider, status, device_info, metadata, owner_id
)
VALUES ($1, 'Workspace delete runtime', 'cloud', 'delete-test', 'offline', '', '{}'::jsonb, $2)
RETURNING id
`, wsID, testUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("create workspace runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (
	workspace_id, name, runtime_mode, runtime_config, runtime_id, owner_id
)
VALUES ($1, 'Workspace delete agent', 'cloud', '{}'::jsonb, $2, $3)
RETURNING id
`, wsID, runtimeID, testUserID).Scan(&agentID); err != nil {
		t.Fatalf("create workspace agent: %v", err)
	}

	var issueID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id)
VALUES ($1, 'Workspace delete issue', 'member', $2)
RETURNING id
`, wsID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create workspace issue: %v", err)
	}

	var autopilotID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO autopilot (
	workspace_id, title, assignee_id, created_by_type, created_by_id
)
VALUES ($1, 'Workspace delete autopilot', $2, 'member', $3)
RETURNING id
`, wsID, agentID, testUserID).Scan(&autopilotID); err != nil {
		t.Fatalf("create workspace autopilot: %v", err)
	}

	var autopilotRunID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO autopilot_run (autopilot_id, source, status, issue_id)
VALUES ($1, 'manual', 'completed', $2)
RETURNING id
`, autopilotID, issueID).Scan(&autopilotRunID); err != nil {
		t.Fatalf("create workspace autopilot run: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (
	agent_id, runtime_id, issue_id, status, priority, autopilot_run_id, completed_at
)
VALUES ($1, $2, $3, 'completed', 0, $4, now())
RETURNING id
`, agentID, runtimeID, issueID, autopilotRunID).Scan(&taskID); err != nil {
		t.Fatalf("create workspace task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
UPDATE autopilot_run SET task_id = $2 WHERE id = $1
`, autopilotRunID, taskID); err != nil {
		t.Fatalf("link workspace autopilot run to task: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
VALUES ($1, 'delete-test', 'workspace-delete', 10, 5)
`, taskID); err != nil {
		t.Fatalf("create workspace task usage: %v", err)
	}

	var rollupRuntimeID, rollupAgentID string
	if err := testPool.QueryRow(ctx, `SELECT gen_random_uuid(), gen_random_uuid()`).Scan(&rollupRuntimeID, &rollupAgentID); err != nil {
		t.Fatalf("create rollup fixture IDs: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage_hourly (
	bucket_hour, workspace_id, runtime_id, agent_id, provider, model,
	input_tokens, output_tokens, task_count, event_count
)
VALUES (date_trunc('hour', now()), $1, $2, $3, 'delete-test', 'workspace-rollup', 10, 5, 1, 1)
`, wsID, rollupRuntimeID, rollupAgentID); err != nil {
		t.Fatalf("create workspace hourly usage: %v", err)
	}
	if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage_hourly_dirty (
	bucket_hour, workspace_id, runtime_id, agent_id, provider, model
)
VALUES (date_trunc('hour', now()), $1, $2, $3, 'delete-test', 'workspace-rollup')
`, wsID, rollupRuntimeID, rollupAgentID); err != nil {
		t.Fatalf("create workspace dirty usage: %v", err)
	}

	var runtimeProfileID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO runtime_profile (
	workspace_id, display_name, protocol_family, command_name, created_by
)
VALUES ($1, 'Delete cleanup profile', 'codex', 'codex', $2)
RETURNING id
`, wsID, testUserID).Scan(&runtimeProfileID); err != nil {
		t.Fatalf("create runtime profile: %v", err)
	}

	var ruleVersionID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO autopilot_rule_version (
	autopilot_id, workspace_id, published_by_type, published_by_id
)
VALUES (gen_random_uuid(), $1, 'member', $2)
RETURNING id
`, wsID, testUserID).Scan(&ruleVersionID); err != nil {
		t.Fatalf("create autopilot rule version: %v", err)
	}

	const pendingObjectKey = "workspace-delete-pending-object"
	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_media_pending_object (
	storage_key, workspace_id, chat_message_id, storage_url
)
VALUES ($1, $2, gen_random_uuid(), 's3://workspace-delete/pending-object')
`, pendingObjectKey, wsID); err != nil {
		t.Fatalf("create pending channel media object: %v", err)
	}

	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE workspace_id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly WHERE workspace_id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE id = $1`, runtimeProfileID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM autopilot_rule_version WHERE id = $1`, ruleVersionID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_media_pending_object WHERE storage_key = $1`, pendingObjectKey)
	})

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testHandler.DeleteWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 from DeleteWorkspace handler for owner, got %d: %s", w.Code, w.Body.String())
	}

	var exists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM workspace WHERE id = $1)`, wsID).Scan(&exists); err != nil {
		t.Fatalf("verify workspace: %v", err)
	}
	if exists {
		t.Fatal("workspace still exists after owner DELETE")
	}

	var pendingCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM github_pending_check_suite WHERE workspace_id = $1`, wsID).Scan(&pendingCount); err != nil {
		t.Fatalf("verify pending check suites: %v", err)
	}
	if pendingCount != 0 {
		t.Fatalf("pending check suites were not cleaned up for deleted workspace: %d", pendingCount)
	}

	var checkRunCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM github_pull_request_check_run WHERE pr_id = $1`, githubPRID).Scan(&checkRunCount); err != nil {
		t.Fatalf("verify github PR check-run cleanup: %v", err)
	}
	if checkRunCount != 0 {
		t.Fatalf("github PR check runs were not cleaned up for deleted workspace: %d", checkRunCount)
	}

	var propertyCount int
	if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM issue_property WHERE id = $1`, propertyID).Scan(&propertyCount); err != nil {
		t.Fatalf("verify issue property cleanup: %v", err)
	}
	if propertyCount != 0 {
		t.Fatalf("issue properties were not cleaned up for deleted workspace: %d", propertyCount)
	}

	for _, table := range []string{
		"task_usage_hourly_dirty",
		"task_usage_hourly",
		"runtime_profile",
		"autopilot_rule_version",
	} {
		var count int
		if err := testPool.QueryRow(ctx, `SELECT COUNT(*) FROM `+table+` WHERE workspace_id = $1`, wsID).Scan(&count); err != nil {
			t.Fatalf("verify %s cleanup: %v", table, err)
		}
		if count != 0 {
			t.Fatalf("%s rows survived workspace delete: %d", table, count)
		}
	}

	var pendingObjectState string
	if err := testPool.QueryRow(ctx, `
SELECT state
FROM channel_media_pending_object
WHERE storage_key = $1
`, pendingObjectKey).Scan(&pendingObjectState); err != nil {
		t.Fatalf("verify pending channel media object handoff: %v", err)
	}
	if pendingObjectState != "deleting" {
		t.Fatalf("pending channel media object state = %q, want deleting", pendingObjectState)
	}
}

// Break caught: PAT/JWT Runtime registration can transfer owner_id while a
// member revocation is deriving its Runtime lock set. Both mutations must share
// the target Member barrier so either registration commits first and is then
// revoked, or revocation commits first and registration fails closed.
func TestRuntimeMemberRevokeSerializesRuntimeOwnerRegistration(t *testing.T) {
	type registrationOutcome struct {
		result runtimeRegistrationMutationResult
		err    error
	}
	type revokeOutcome struct {
		result revocationResult
		err    error
	}
	type heartbeatOutcome struct {
		err error
	}
	waitForBlockedBy := func(t *testing.T, blockerPID int32, queryName string) int32 {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			var blockedPID int32
			err := testPool.QueryRow(context.Background(), `
				SELECT activity.pid
				FROM pg_stat_activity AS activity
				WHERE $1 = ANY(pg_blocking_pids(activity.pid))
				  AND activity.wait_event_type = 'Lock'
				  AND activity.query LIKE '%' || $2 || '%'
				ORDER BY activity.pid
				LIMIT 1
			`, blockerPID, queryName).Scan(&blockedPID)
			if err == nil {
				return blockedPID
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				t.Fatalf("inspect blocked %s: %v", queryName, err)
			}
			time.Sleep(10 * time.Millisecond)
		}
		t.Fatalf("%s did not reach the deterministic lock barrier", queryName)
		return 0
	}
	register := func(fixture revocationFixture, ownerID string) registrationOutcome {
		runtime, err := testHandler.Queries.GetAgentRuntime(context.Background(), parseUUID(fixture.RuntimeID))
		if err != nil {
			return registrationOutcome{err: err}
		}
		ownerUUID := pgtype.UUID{}
		if ownerID != "" {
			ownerUUID = parseUUID(ownerID)
		}
		result, err := testHandler.registerRuntimeMutation(context.Background(), runtimeRegistrationMutation{
			WorkspaceID:          runtime.WorkspaceID,
			DaemonID:             runtime.DaemonID,
			Name:                 runtime.Name,
			RuntimeMode:          runtime.RuntimeMode,
			Provider:             runtime.Provider,
			Status:               "online",
			DeviceInfo:           runtime.DeviceInfo,
			Metadata:             runtime.Metadata,
			OwnerID:              ownerUUID,
			PreserveCapabilities: true,
		})
		return registrationOutcome{result: result, err: err}
	}
	heartbeatHandler := func() (*Handler, *runtimeCapabilityWakeRecorder) {
		queries := db.New(testPool)
		wake := &runtimeCapabilityWakeRecorder{}
		taskService := service.NewTaskService(queries, testPool, nil, events.New())
		taskService.RuntimePool = wake
		copy := *testHandler
		copy.Queries = queries
		copy.DB = testPool
		copy.TxStarter = testPool
		copy.TaskService = taskService
		copy.LivenessStore = NewNoopLivenessStore()
		copy.HeartbeatScheduler = NewPassthroughHeartbeatScheduler(queries)
		return &copy, wake
	}
	beginRuntimeBlocker := func(t *testing.T, runtimeID string) (pgx.Tx, int32) {
		t.Helper()
		tx, err := testPool.Begin(context.Background())
		if err != nil {
			t.Fatalf("begin Runtime blocker: %v", err)
		}
		if _, err := tx.Exec(context.Background(), `SELECT id FROM agent_runtime WHERE id = $1 FOR UPDATE`, runtimeID); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("lock Runtime: %v", err)
		}
		var pid int32
		if err := tx.QueryRow(context.Background(), `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
			_ = tx.Rollback(context.Background())
			t.Fatalf("read Runtime blocker PID: %v", err)
		}
		return tx, pid
	}
	assertRevokedRuntime := func(t *testing.T, fixture revocationFixture) {
		t.Helper()
		var memberExists bool
		if err := testPool.QueryRow(context.Background(), `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fixture.MemberID).Scan(&memberExists); err != nil {
			t.Fatalf("check revoked Member: %v", err)
		}
		if memberExists {
			t.Fatal("target Member survived revocation")
		}
		var status string
		if err := testPool.QueryRow(context.Background(), `SELECT status FROM agent_runtime WHERE id = $1`, fixture.RuntimeID).Scan(&status); err != nil {
			t.Fatalf("load Runtime after race: %v", err)
		}
		if status != "offline" {
			t.Fatalf("Runtime status = %q, want offline after revocation", status)
		}
	}

	t.Run("registration commits before revocation", func(t *testing.T) {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		fixture := setupRevocationFixture(t, "handler-tests-owner-register-first-"+suffix, "daemon-owner-register-first-"+suffix)
		if _, err := testPool.Exec(context.Background(), `UPDATE agent_runtime SET owner_id = $1 WHERE id = $2`, testUserID, fixture.RuntimeID); err != nil {
			t.Fatalf("move Runtime away from target: %v", err)
		}
		blocker, blockerPID := beginRuntimeBlocker(t, fixture.RuntimeID)
		defer blocker.Rollback(context.Background())

		registrationDone := make(chan registrationOutcome, 1)
		go func() { registrationDone <- register(fixture, fixture.TargetUserID) }()
		registrationPID := waitForBlockedBy(t, blockerPID, "LockRuntimeForCapabilityRegistration")

		revokeDone := make(chan revokeOutcome, 1)
		go func() {
			result, err := testHandler.revokeAndRemoveMember(
				context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(fixture.TargetUserID),
				parseUUID(fixture.MemberID), parseUUID(testUserID),
			)
			revokeDone <- revokeOutcome{result: result, err: err}
		}()
		waitForBlockedBy(t, registrationPID, "LockRuntimeOwnerWrites")
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release Runtime blocker: %v", err)
		}

		select {
		case outcome := <-registrationDone:
			if outcome.err != nil {
				t.Fatalf("registration-first mutation: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("registration-first mutation did not finish")
		}
		select {
		case outcome := <-revokeDone:
			if outcome.err != nil {
				t.Fatalf("revocation after registration: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("revocation after registration did not finish")
		}
		assertRevokedRuntime(t, fixture)
	})

	t.Run("revocation commits before registration", func(t *testing.T) {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		fixture := setupRevocationFixture(t, "handler-tests-owner-revoke-first-"+suffix, "daemon-owner-revoke-first-"+suffix)
		blocker, blockerPID := beginRuntimeBlocker(t, fixture.RuntimeID)
		defer blocker.Rollback(context.Background())

		revokeDone := make(chan revokeOutcome, 1)
		go func() {
			result, err := testHandler.revokeAndRemoveMember(
				context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(fixture.TargetUserID),
				parseUUID(fixture.MemberID), parseUUID(testUserID),
			)
			revokeDone <- revokeOutcome{result: result, err: err}
		}()
		revokePID := waitForBlockedBy(t, blockerPID, "LockAgentRuntime")

		registrationDone := make(chan registrationOutcome, 1)
		go func() { registrationDone <- register(fixture, fixture.TargetUserID) }()
		waitForBlockedBy(t, revokePID, "LockRuntimeOwnerWrites")
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release Runtime blocker: %v", err)
		}

		select {
		case outcome := <-revokeDone:
			if outcome.err != nil {
				t.Fatalf("revocation-first mutation: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("revocation-first mutation did not finish")
		}
		select {
		case outcome := <-registrationDone:
			if outcome.err == nil {
				t.Fatal("registration unexpectedly succeeded after target Member was revoked")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("registration after revocation did not finish")
		}
		assertRevokedRuntime(t, fixture)
	})

	t.Run("revocation commits before daemon token owner-preserving registration", func(t *testing.T) {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		fixture := setupRevocationFixture(t, "handler-tests-token-revoke-first-"+suffix, "daemon-token-revoke-first-"+suffix)
		blocker, blockerPID := beginRuntimeBlocker(t, fixture.RuntimeID)
		defer blocker.Rollback(context.Background())

		revokeDone := make(chan revokeOutcome, 1)
		go func() {
			result, err := testHandler.revokeAndRemoveMember(
				context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(fixture.TargetUserID),
				parseUUID(fixture.MemberID), parseUUID(testUserID),
			)
			revokeDone <- revokeOutcome{result: result, err: err}
		}()
		revokePID := waitForBlockedBy(t, blockerPID, "LockAgentRuntime")

		registrationDone := make(chan registrationOutcome, 1)
		go func() { registrationDone <- register(fixture, "") }()
		waitForBlockedBy(t, revokePID, "LockRuntimeOwnerWrites")
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release Runtime blocker: %v", err)
		}

		select {
		case outcome := <-revokeDone:
			if outcome.err != nil {
				t.Fatalf("revocation before daemon-token registration: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("revocation before daemon-token registration did not finish")
		}
		select {
		case outcome := <-registrationDone:
			if outcome.err == nil {
				t.Fatal("daemon-token registration revived a removed owner's Runtime")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("daemon-token registration after revocation did not finish")
		}
		assertRevokedRuntime(t, fixture)
	})

	t.Run("heartbeat recovery commits before revocation", func(t *testing.T) {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		fixture := setupRevocationFixture(t, "handler-tests-heartbeat-first-"+suffix, "daemon-heartbeat-first-"+suffix)
		if _, err := testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET status = 'offline', last_seen_at = now() - interval '10 minutes'
			WHERE id = $1
		`, fixture.RuntimeID); err != nil {
			t.Fatalf("make Runtime need synchronous heartbeat recovery: %v", err)
		}
		handler, wake := heartbeatHandler()
		runtime, err := handler.Queries.GetAgentRuntime(context.Background(), parseUUID(fixture.RuntimeID))
		if err != nil {
			t.Fatalf("load heartbeat Runtime: %v", err)
		}
		blocker, blockerPID := beginRuntimeBlocker(t, fixture.RuntimeID)
		defer blocker.Rollback(context.Background())

		heartbeatDone := make(chan heartbeatOutcome, 1)
		go func() { heartbeatDone <- heartbeatOutcome{err: handler.recordHeartbeat(context.Background(), runtime)} }()
		heartbeatPID := waitForBlockedBy(t, blockerPID, "LockAgentRuntime")

		revokeDone := make(chan revokeOutcome, 1)
		go func() {
			result, revokeErr := handler.revokeAndRemoveMember(
				context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(fixture.TargetUserID),
				parseUUID(fixture.MemberID), parseUUID(testUserID),
			)
			revokeDone <- revokeOutcome{result: result, err: revokeErr}
		}()
		waitForBlockedBy(t, heartbeatPID, "LockRuntimeOwnerWrites")
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release Runtime blocker: %v", err)
		}

		select {
		case outcome := <-heartbeatDone:
			if outcome.err != nil {
				t.Fatalf("heartbeat-first recovery: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("heartbeat-first recovery did not finish")
		}
		select {
		case outcome := <-revokeDone:
			if outcome.err != nil {
				t.Fatalf("revocation after heartbeat: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("revocation after heartbeat did not finish")
		}
		assertRevokedRuntime(t, fixture)
		if len(wake.requests) != 1 {
			t.Fatalf("heartbeat-first Workspace wakes = %d, want 1 before the later revocation", len(wake.requests))
		}
	})

	t.Run("revocation commits before authenticated heartbeat recovery", func(t *testing.T) {
		suffix := fmt.Sprintf("%d", time.Now().UnixNano())
		fixture := setupRevocationFixture(t, "handler-tests-heartbeat-revoke-first-"+suffix, "daemon-heartbeat-revoke-first-"+suffix)
		if _, err := testPool.Exec(context.Background(), `
			UPDATE agent_runtime SET status = 'offline', last_seen_at = now() - interval '10 minutes'
			WHERE id = $1
		`, fixture.RuntimeID); err != nil {
			t.Fatalf("make Runtime need synchronous heartbeat recovery: %v", err)
		}
		handler, wake := heartbeatHandler()
		// This snapshot models an HTTP request that passed access checks, or a WS
		// connection that loaded the Runtime, before revocation committed.
		runtime, err := handler.Queries.GetAgentRuntime(context.Background(), parseUUID(fixture.RuntimeID))
		if err != nil {
			t.Fatalf("load pre-revocation heartbeat Runtime: %v", err)
		}
		blocker, blockerPID := beginRuntimeBlocker(t, fixture.RuntimeID)
		defer blocker.Rollback(context.Background())

		revokeDone := make(chan revokeOutcome, 1)
		go func() {
			result, revokeErr := handler.revokeAndRemoveMember(
				context.Background(), parseUUID(fixture.WorkspaceID), parseUUID(fixture.TargetUserID),
				parseUUID(fixture.MemberID), parseUUID(testUserID),
			)
			revokeDone <- revokeOutcome{result: result, err: revokeErr}
		}()
		revokePID := waitForBlockedBy(t, blockerPID, "LockAgentRuntime")

		heartbeatDone := make(chan heartbeatOutcome, 1)
		go func() { heartbeatDone <- heartbeatOutcome{err: handler.recordHeartbeat(context.Background(), runtime)} }()
		waitForBlockedBy(t, revokePID, "LockRuntimeOwnerWrites")
		if err := blocker.Commit(context.Background()); err != nil {
			t.Fatalf("release Runtime blocker: %v", err)
		}

		select {
		case outcome := <-revokeDone:
			if outcome.err != nil {
				t.Fatalf("revocation-first heartbeat race: %v", outcome.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("revocation-first heartbeat race did not finish")
		}
		select {
		case outcome := <-heartbeatDone:
			if outcome.err == nil {
				t.Fatal("authenticated in-flight heartbeat revived a removed owner's Runtime")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("authenticated heartbeat after revocation did not finish")
		}
		assertRevokedRuntime(t, fixture)
		if len(wake.requests) != 0 {
			t.Fatalf("post-revocation heartbeat Workspace wakes = %d, want 0", len(wake.requests))
		}
	})
}

func TestDeleteWorkspace_DirtyTriggersHaveTeardownGuard(t *testing.T) {
	ctx := context.Background()
	for _, triggerName := range []string{
		"trg_atq_dirty_hourly",
		"trg_issue_delete_dirty_hourly",
		"trg_tu_dirty_hourly",
	} {
		var definition string
		if err := testPool.QueryRow(ctx, `
SELECT pg_get_triggerdef(oid)
FROM pg_trigger
WHERE tgname = $1
  AND NOT tgisinternal
`, triggerName).Scan(&definition); err != nil {
			t.Fatalf("read trigger %s: %v", triggerName, err)
		}
		if !strings.Contains(definition, "multica.workspace_teardown") {
			t.Fatalf("trigger %s does not guard workspace teardown: %s", triggerName, definition)
		}
	}
}

func TestWorkspaceTeardownModeDoesNotLeakIntoOrdinaryDeletes(t *testing.T) {
	ctx := context.Background()
	conn, err := testPool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire connection: %v", err)
	}
	defer conn.Release()

	tx, err := conn.Begin(ctx)
	if err != nil {
		t.Fatalf("begin teardown marker transaction: %v", err)
	}
	if _, err := tx.Exec(ctx, `SELECT set_config('multica.workspace_teardown', 'on', true)`); err != nil {
		_ = tx.Rollback(ctx)
		t.Fatalf("set transaction-local teardown mode: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit teardown marker transaction: %v", err)
	}

	var teardownMode string
	if err := conn.QueryRow(ctx, `SELECT current_setting('multica.workspace_teardown', true)`).Scan(&teardownMode); err != nil {
		t.Fatalf("read teardown mode after commit: %v", err)
	}
	if teardownMode != "" {
		t.Fatalf("teardown mode leaked after commit: %q", teardownMode)
	}

	var runtimeID, agentID string
	if err := conn.QueryRow(ctx, `
SELECT runtime.id, agent.id
FROM agent_runtime AS runtime
JOIN agent ON agent.runtime_id = runtime.id
WHERE runtime.workspace_id = $1
LIMIT 1
`, testWorkspaceID).Scan(&runtimeID, &agentID); err != nil {
		t.Fatalf("load ordinary delete fixture agent: %v", err)
	}

	var issueID string
	if err := conn.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_id, creator_type, number)
VALUES (
	$1, 'ordinary delete after workspace teardown', $2, 'member',
	(SELECT COALESCE(MAX(number), 0) + 1 FROM issue WHERE workspace_id = $1)
)
RETURNING id
`, testWorkspaceID, testUserID).Scan(&issueID); err != nil {
		t.Fatalf("create ordinary delete issue: %v", err)
	}
	var taskID string
	if err := conn.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, issue_id, status, completed_at)
VALUES ($1, $2, $3, 'completed', now())
RETURNING id
`, agentID, runtimeID, issueID).Scan(&taskID); err != nil {
		t.Fatalf("create ordinary delete task: %v", err)
	}

	const provider = "workspace-teardown-ordinary-delete"
	_, _ = conn.Exec(ctx, `DELETE FROM task_usage_hourly_dirty WHERE provider = $1`, provider)
	_, _ = conn.Exec(ctx, `DELETE FROM task_usage_hourly WHERE provider = $1`, provider)
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE provider = $1`, provider)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly WHERE provider = $1`, provider)
	})

	var usageID string
	if err := conn.QueryRow(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
VALUES ($1, $2, 'task-usage-delete', 10, 5)
RETURNING id
`, taskID, provider).Scan(&usageID); err != nil {
		t.Fatalf("create ordinary task usage: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM task_usage WHERE id = $1`, usageID); err != nil {
		t.Fatalf("delete ordinary task usage: %v", err)
	}

	var dirtyCount int
	if err := conn.QueryRow(ctx, `
SELECT COUNT(*)
FROM task_usage_hourly_dirty
WHERE provider = $1 AND model = 'task-usage-delete'
`, provider).Scan(&dirtyCount); err != nil {
		t.Fatalf("count task-usage delete dirty keys: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("ordinary task_usage DELETE dirty keys = %d, want 1", dirtyCount)
	}

	if _, err := conn.Exec(ctx, `
INSERT INTO task_usage (task_id, provider, model, input_tokens, output_tokens)
VALUES ($1, $2, 'issue-delete', 10, 5)
`, taskID, provider); err != nil {
		t.Fatalf("create ordinary issue-delete usage: %v", err)
	}
	if _, err := conn.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID); err != nil {
		t.Fatalf("delete ordinary issue: %v", err)
	}
	if err := conn.QueryRow(ctx, `
SELECT COUNT(*)
FROM task_usage_hourly_dirty
WHERE provider = $1 AND model = 'issue-delete'
`, provider).Scan(&dirtyCount); err != nil {
		t.Fatalf("count issue delete dirty keys: %v", err)
	}
	if dirtyCount != 1 {
		t.Fatalf("ordinary issue DELETE dirty keys = %d, want 1", dirtyCount)
	}
}

func TestDeleteWorkspace_PreservesOtherWorkspaceData(t *testing.T) {
	ctx := context.Background()
	const targetSlug = "handler-tests-delete-tenant-target"
	const neighborSlug = "handler-tests-delete-tenant-neighbor"
	const targetMediaKey = "workspace-delete-tenant-target-media"
	const neighborMediaKey = "workspace-delete-tenant-neighbor-media"

	_, _ = testPool.Exec(ctx, `DELETE FROM channel_media_pending_object WHERE storage_key IN ($1, $2)`, targetMediaKey, neighborMediaKey)
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug IN ($1, $2)`, targetSlug, neighborSlug)

	var targetWorkspaceID, neighborWorkspaceID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug)
VALUES ('Workspace delete tenant target', $1)
RETURNING id
`, targetSlug).Scan(&targetWorkspaceID); err != nil {
		t.Fatalf("create target workspace: %v", err)
	}
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug)
VALUES ('Workspace delete tenant neighbor', $1)
RETURNING id
`, neighborSlug).Scan(&neighborWorkspaceID); err != nil {
		t.Fatalf("create neighbor workspace: %v", err)
	}
	t.Cleanup(func() {
		for _, workspaceID := range []string{targetWorkspaceID, neighborWorkspaceID} {
			_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, workspaceID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM task_usage_hourly_dirty WHERE workspace_id = $1`, workspaceID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM runtime_profile WHERE workspace_id = $1`, workspaceID)
			_, _ = testPool.Exec(context.Background(), `DELETE FROM channel_media_pending_object WHERE workspace_id = $1`, workspaceID)
		}
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, targetWorkspaceID, testUserID); err != nil {
		t.Fatalf("create target owner: %v", err)
	}

	type tenantFixture struct {
		workspaceID string
		mediaKey    string
		issueID     string
	}
	fixtures := []*tenantFixture{
		{workspaceID: targetWorkspaceID, mediaKey: targetMediaKey},
		{workspaceID: neighborWorkspaceID, mediaKey: neighborMediaKey},
	}
	for _, fixture := range fixtures {
		if err := testPool.QueryRow(ctx, `
INSERT INTO issue (workspace_id, title, creator_type, creator_id)
VALUES ($1, 'Workspace delete tenant isolation', 'member', $2)
RETURNING id
`, fixture.workspaceID, testUserID).Scan(&fixture.issueID); err != nil {
			t.Fatalf("create issue for workspace %s: %v", fixture.workspaceID, err)
		}
		if _, err := testPool.Exec(ctx, `
INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content)
VALUES ($1, $2, 'member', $3, 'Workspace delete tenant isolation')
`, fixture.issueID, fixture.workspaceID, testUserID); err != nil {
			t.Fatalf("create comment for workspace %s: %v", fixture.workspaceID, err)
		}
		if _, err := testPool.Exec(ctx, `
INSERT INTO inbox_item (
	workspace_id, recipient_type, recipient_id, type, issue_id, title
)
VALUES ($1, 'member', $2, 'workspace-delete-test', $3, 'Workspace delete tenant isolation')
`, fixture.workspaceID, testUserID, fixture.issueID); err != nil {
			t.Fatalf("create inbox item for workspace %s: %v", fixture.workspaceID, err)
		}
		if _, err := testPool.Exec(ctx, `
INSERT INTO runtime_profile (
	workspace_id, display_name, protocol_family, command_name, created_by
)
VALUES ($1, 'Workspace delete tenant isolation', 'codex', 'codex', $2)
`, fixture.workspaceID, testUserID); err != nil {
			t.Fatalf("create runtime profile for workspace %s: %v", fixture.workspaceID, err)
		}
		if _, err := testPool.Exec(ctx, `
INSERT INTO task_usage_hourly_dirty (
	bucket_hour, workspace_id, runtime_id, agent_id, provider, model
)
VALUES (date_trunc('hour', now()), $1, gen_random_uuid(), gen_random_uuid(), 'workspace-delete-tenant', 'isolation')
`, fixture.workspaceID); err != nil {
			t.Fatalf("create dirty usage for workspace %s: %v", fixture.workspaceID, err)
		}
		if _, err := testPool.Exec(ctx, `
INSERT INTO channel_media_pending_object (
	storage_key, workspace_id, chat_message_id, storage_url
)
VALUES ($1, $2, gen_random_uuid(), 's3://workspace-delete/tenant-isolation')
`, fixture.mediaKey, fixture.workspaceID); err != nil {
			t.Fatalf("create media ledger for workspace %s: %v", fixture.workspaceID, err)
		}
	}

	recorder := httptest.NewRecorder()
	request := newRequest(http.MethodDelete, "/api/workspaces/"+targetWorkspaceID, nil)
	request = withURLParam(request, "id", targetWorkspaceID)
	testHandler.DeleteWorkspace(recorder, request)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("DeleteWorkspace returned %d: %s", recorder.Code, recorder.Body.String())
	}

	for table, predicate := range map[string]string{
		"workspace":                    "id",
		"issue":                        "workspace_id",
		"comment":                      "workspace_id",
		"inbox_item":                   "workspace_id",
		"runtime_profile":              "workspace_id",
		"task_usage_hourly_dirty":      "workspace_id",
		"channel_media_pending_object": "workspace_id",
	} {
		var count int
		if err := testPool.QueryRow(ctx, `
SELECT COUNT(*) FROM `+table+` WHERE `+predicate+` = $1
`, neighborWorkspaceID).Scan(&count); err != nil {
			t.Fatalf("count neighbor %s rows: %v", table, err)
		}
		if count != 1 {
			t.Fatalf("neighbor %s rows = %d, want 1", table, count)
		}
	}

	var neighborMediaState string
	if err := testPool.QueryRow(ctx, `
SELECT state
FROM channel_media_pending_object
WHERE storage_key = $1
`, neighborMediaKey).Scan(&neighborMediaState); err != nil {
		t.Fatalf("read neighbor media state: %v", err)
	}
	if neighborMediaState != "pending" {
		t.Fatalf("neighbor media state = %q, want pending", neighborMediaState)
	}
}

// TestUpdateWorkspace_AvatarURL covers the avatar_url field added to
// UpdateWorkspaceRequest: a PATCH with avatar_url is persisted and surfaced
// back on the response, and partial updates leave other fields untouched.
// Route-level authorization (owner/admin) is enforced by middleware in
// router.go; the handler test calls UpdateWorkspace directly to verify the
// payload wiring.
func TestUpdateWorkspace_AvatarURL(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-avatar-url"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Avatar URL", slug, "UpdateWorkspace avatar_url test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	const avatarURL = "https://cdn.example.com/workspaces/abc/logo.png"

	w := httptest.NewRecorder()
	req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
		"avatar_url": avatarURL,
	})
	req = withURLParam(req, "id", wsID)
	testHandler.UpdateWorkspace(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from UpdateWorkspace, got %d: %s", w.Code, w.Body.String())
	}

	var resp WorkspaceResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.AvatarURL == nil || *resp.AvatarURL != avatarURL {
		t.Fatalf("expected avatar_url %q in response, got %v", avatarURL, resp.AvatarURL)
	}
	if resp.Name != "Handler Test Avatar URL" {
		t.Fatalf("name should be unchanged by avatar-only update, got %q", resp.Name)
	}

	var dbAvatar *string
	if err := testPool.QueryRow(ctx, `SELECT avatar_url FROM workspace WHERE id = $1`, wsID).Scan(&dbAvatar); err != nil {
		t.Fatalf("read avatar_url back: %v", err)
	}
	if dbAvatar == nil || *dbAvatar != avatarURL {
		t.Fatalf("expected avatar_url %q persisted, got %v", avatarURL, dbAvatar)
	}

	// A follow-up update that doesn't include avatar_url must leave it alone.
	w2 := httptest.NewRecorder()
	req2 := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
		"description": "new description",
	})
	req2 = withURLParam(req2, "id", wsID)
	testHandler.UpdateWorkspace(w2, req2)

	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200 from second UpdateWorkspace, got %d: %s", w2.Code, w2.Body.String())
	}

	var resp2 WorkspaceResponse
	if err := json.Unmarshal(w2.Body.Bytes(), &resp2); err != nil {
		t.Fatalf("decode second response: %v", err)
	}
	if resp2.AvatarURL == nil || *resp2.AvatarURL != avatarURL {
		t.Fatalf("avatar_url should be preserved by partial update, got %v", resp2.AvatarURL)
	}
}

func TestUpdateWorkspace_ReposValidation(t *testing.T) {
	ctx := context.Background()

	const slug = "handler-tests-repos-validation"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description)
VALUES ($1, $2, $3)
RETURNING id
`, "Handler Test Repos Validation", slug, "UpdateWorkspace repos validation test").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
	})

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role)
VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create owner member: %v", err)
	}

	t.Run("rejects invalid repo URLs without persisting", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
			"repos": []map[string]any{
				{"url": "not-a-url"},
			},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 from invalid repos update, got %d: %s", w.Code, w.Body.String())
		}

		var raw []byte
		if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, wsID).Scan(&raw); err != nil {
			t.Fatalf("read repos: %v", err)
		}
		if string(raw) != "[]" {
			t.Fatalf("invalid repos update should not persist, got %s", raw)
		}
	})

	t.Run("normalizes valid repos", func(t *testing.T) {
		w := httptest.NewRecorder()
		req := newRequest("PATCH", "/api/workspaces/"+wsID, map[string]any{
			"repos": []map[string]any{
				{
					"url":         "  https://github.com/multica-ai/multica.git  ",
					"description": "  main monorepo  ",
				},
				{
					"url": "https://github.com/multica-ai/multica.git",
				},
				{
					"url": "git@github.com:multica-ai/multica-cloud.git",
				},
			},
		})
		req = withURLParam(req, "id", wsID)
		testHandler.UpdateWorkspace(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("expected 200 from valid repos update, got %d: %s", w.Code, w.Body.String())
		}

		var raw []byte
		if err := testPool.QueryRow(ctx, `SELECT repos FROM workspace WHERE id = $1`, wsID).Scan(&raw); err != nil {
			t.Fatalf("read repos: %v", err)
		}
		var repos []workspaceRepoRef
		if err := json.Unmarshal(raw, &repos); err != nil {
			t.Fatalf("decode repos: %v", err)
		}
		if len(repos) != 2 {
			t.Fatalf("expected duplicate URL to be deduped, got %d repos: %s", len(repos), raw)
		}
		if repos[0].URL != "https://github.com/multica-ai/multica.git" || repos[0].Description != "main monorepo" {
			t.Fatalf("first repo not normalized: %+v", repos[0])
		}
		if repos[1].URL != "git@github.com:multica-ai/multica-cloud.git" {
			t.Fatalf("second repo not preserved: %+v", repos[1])
		}
	})
}

// revocationFixture is a minimal (workspace, member-to-revoke, runtime,
// agent, queued-task, daemon-token) bundle used to drive the revocation
// tests. The "requester" is always testUserID (owner of the workspace) so
// `newRequest` passes the existing fixtures' auth context unchanged.
type revocationFixture struct {
	WorkspaceID  string
	TargetUserID string
	MemberID     string
	RuntimeID    string
	AgentID      string
	TaskID       string
	DaemonID     string
	TokenHash    string
}

func setupRevocationFixture(t *testing.T, slug, daemonID string) revocationFixture {
	t.Helper()
	ctx := context.Background()

	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Revocation "+slug, slug, "revocation test", "REV").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	// Requester (= testUserID) is always an owner so DeleteMember authorization
	// passes. Two owners total so LeaveWorkspace doesn't trip the "must keep
	// at least one owner" guard.
	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create requester member: %v", err)
	}

	targetEmail := fmt.Sprintf("revocation-%s@multica.ai", slug)
	var targetUserID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Revocation Target "+slug, targetEmail).Scan(&targetUserID); err != nil {
		t.Fatalf("create target user: %v", err)
	}

	// Cleanup ordering: workspace first (cascade clears agent_runtime,
	// agent, member, daemon_token), then user (whose deletion would
	// otherwise be blocked by agent.owner_id / agent_runtime.owner_id FKs).
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner') RETURNING id
`, wsID, targetUserID).Scan(&memberID); err != nil {
		t.Fatalf("create target member: %v", err)
	}

	var runtimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status,
    device_info, metadata, owner_id, last_seen_at
)
VALUES ($1, $2, 'Target Runtime', 'local', 'multica_daemon', 'online', '', '{}'::jsonb, $3, now())
RETURNING id
`, wsID, daemonID, targetUserID).Scan(&runtimeID); err != nil {
		t.Fatalf("insert runtime: %v", err)
	}

	var agentID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent (
    workspace_id, name, description, runtime_mode, runtime_config,
    runtime_id, visibility, max_concurrent_tasks, owner_id
)
VALUES ($1, 'Target Agent', '', 'local', '{}'::jsonb, $2, 'workspace', 1, $3)
RETURNING id
`, wsID, runtimeID, targetUserID).Scan(&agentID); err != nil {
		t.Fatalf("insert agent: %v", err)
	}

	var taskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
VALUES ($1, $2, 'queued', 0)
RETURNING id
`, agentID, runtimeID).Scan(&taskID); err != nil {
		t.Fatalf("insert task: %v", err)
	}

	// daemon_token row — paired with the runtime's daemon_id so the
	// revocation should sweep its hash up via DeleteDaemonTokensByWorkspaceAndDaemons.
	rawToken := "mdt_test_" + slug
	sum := sha256.Sum256([]byte(rawToken))
	tokenHash := hex.EncodeToString(sum[:])
	if _, err := testPool.Exec(ctx, `
INSERT INTO daemon_token (token_hash, workspace_id, daemon_id, expires_at)
VALUES ($1, $2, $3, now() + interval '1 day')
`, tokenHash, wsID, daemonID); err != nil {
		t.Fatalf("insert daemon_token: %v", err)
	}

	return revocationFixture{
		WorkspaceID:  wsID,
		TargetUserID: targetUserID,
		MemberID:     memberID,
		RuntimeID:    runtimeID,
		AgentID:      agentID,
		TaskID:       taskID,
		DaemonID:     daemonID,
		TokenHash:    tokenHash,
	}
}

func assertRevoked(t *testing.T, fx revocationFixture) {
	t.Helper()
	ctx := context.Background()

	var memberExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, fx.MemberID).Scan(&memberExists); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberExists {
		t.Fatal("member row was not deleted")
	}

	var runtimeStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, fx.RuntimeID).Scan(&runtimeStatus); err != nil {
		t.Fatalf("query runtime: %v", err)
	}
	if runtimeStatus != "offline" {
		t.Fatalf("expected runtime offline, got %q", runtimeStatus)
	}

	var archivedAt *string
	if err := testPool.QueryRow(ctx, `SELECT archived_at::text FROM agent WHERE id = $1`, fx.AgentID).Scan(&archivedAt); err != nil {
		t.Fatalf("query agent: %v", err)
	}
	if archivedAt == nil {
		t.Fatal("agent was not archived")
	}

	var taskStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, fx.TaskID).Scan(&taskStatus); err != nil {
		t.Fatalf("query task: %v", err)
	}
	if taskStatus != "cancelled" {
		t.Fatalf("expected task cancelled, got %q", taskStatus)
	}

	var tokenExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM daemon_token WHERE token_hash = $1)`, fx.TokenHash).Scan(&tokenExists); err != nil {
		t.Fatalf("query daemon_token: %v", err)
	}
	if tokenExists {
		t.Fatal("daemon_token row was not deleted")
	}
}

// TestDeleteMember_RevokesTargetRuntimes verifies that when an admin removes
// another member from a workspace, every runtime owned by the removed member
// has its agents archived, its in-flight tasks cancelled, its row flipped
// offline, and its daemon_token rows deleted — all atomically with the member
// row deletion.
func TestDeleteMember_RevokesTargetRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-kick", "daemon-revoke-kick")

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)
}

// TestDeleteMember_PrunesChannelUserBindings verifies the application-layer
// replacement for the channel_user_binding member-FK cascade (MUL-3515 §4):
// removing a member prunes that member's channel bindings, in the same tx as
// the member-row delete, while leaving a remaining member's binding intact.
func TestDeleteMember_PrunesChannelUserBindings(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-binding", "daemon-revoke-binding")
	ctx := context.Background()

	const appID = "cli_revoke_binding"
	const removedOpenID = "ou_revoke_binding_removed"
	const keepOpenID = "ou_revoke_binding_keep"

	// channel_* rows have no FK to workspace (MUL-3515 §4), so the fixture's
	// workspace-delete cleanup never reaches them; clear by deterministic key
	// both before (in case a prior run was killed mid-test) and after.
	cleanChannel := func() {
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_user_binding WHERE channel_user_id = ANY($1)`,
			[]string{removedOpenID, keepOpenID})
		_, _ = testPool.Exec(context.Background(),
			`DELETE FROM channel_installation WHERE channel_type = 'feishu' AND config->>'app_id' = $1`, appID)
	}
	cleanChannel()
	t.Cleanup(cleanChannel)

	var installID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO channel_installation (workspace_id, agent_id, channel_type, config, installer_user_id)
VALUES ($1, $2, 'feishu', jsonb_build_object('app_id', $3::text), $4)
RETURNING id
`, fx.WorkspaceID, fx.AgentID, appID, testUserID).Scan(&installID); err != nil {
		t.Fatalf("insert channel_installation: %v", err)
	}

	// Binding for the member being removed — must be pruned.
	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', $4)
`, fx.WorkspaceID, fx.TargetUserID, installID, removedOpenID); err != nil {
		t.Fatalf("insert removed-member binding: %v", err)
	}

	// Binding for the requester (an owner who stays) — must survive, proving
	// the prune is scoped to the removed user, not the whole workspace.
	if _, err := testPool.Exec(ctx, `
INSERT INTO channel_user_binding (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
VALUES ($1, $2, $3, 'feishu', $4)
`, fx.WorkspaceID, testUserID, installID, keepOpenID); err != nil {
		t.Fatalf("insert remaining-member binding: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var removedExists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_user_binding WHERE channel_user_id = $1)`, removedOpenID).Scan(&removedExists); err != nil {
		t.Fatalf("query removed-member binding: %v", err)
	}
	if removedExists {
		t.Fatal("removed member's channel_user_binding was not pruned")
	}

	var keepExists bool
	if err := testPool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM channel_user_binding WHERE channel_user_id = $1)`, keepOpenID).Scan(&keepExists); err != nil {
		t.Fatalf("query remaining-member binding: %v", err)
	}
	if !keepExists {
		t.Fatal("remaining member's channel_user_binding was wrongly pruned")
	}
}

// TestLeaveWorkspace_RevokesOwnRuntimes is the self-removal counterpart: when
// a member leaves a workspace voluntarily, their own runtimes are revoked
// with the same atomic write set as DeleteMember.
func TestLeaveWorkspace_RevokesOwnRuntimes(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-leave", "daemon-revoke-leave")

	// Re-target the request from the leaving member's perspective: the
	// leaver is the request actor, not the workspace owner.
	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/leave", nil)
	req.Header.Set("X-User-ID", fx.TargetUserID)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParam(req, "id", fx.WorkspaceID)
	testHandler.LeaveWorkspace(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("LeaveWorkspace: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)
}

// TestDeleteMember_CancelsTasksFromAgentReassignment covers a subtle
// case: an agent's runtime_id can be changed via UpdateAgent, but
// agent_task_queue.runtime_id keeps the value from when the task was
// queued. So after a leaving member is removed, an agent currently bound
// to their runtime gets archived — but tasks that agent queued under a
// PRIOR runtime (still owned by another active member) keep their old
// runtime_id and would not be caught by a runtime-only sweep. Because
// ClaimAgentTask does not gate on agent.archived_at, those orphaned
// queued tasks would remain claimable.
func TestDeleteMember_CancelsTasksFromAgentReassignment(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-reassign", "daemon-revoke-reassign")
	ctx := context.Background()

	// Create a SECOND runtime in the workspace owned by the requester
	// (not the leaving member). The agent originally lived here.
	var otherRuntimeID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_runtime (
    workspace_id, daemon_id, name, runtime_mode, provider, status,
    device_info, metadata, owner_id, last_seen_at
)
VALUES ($1, $2, 'Other Runtime', 'local', 'multica_daemon', 'online', '', '{}'::jsonb, $3, now())
RETURNING id
`, fx.WorkspaceID, "daemon-revoke-reassign-other", testUserID).Scan(&otherRuntimeID); err != nil {
		t.Fatalf("insert other runtime: %v", err)
	}

	// Queue a task on the agent while it was still pinned to the OTHER
	// runtime (simulating a task created before the agent was reassigned
	// to the leaving member's runtime).
	var orphanTaskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority)
VALUES ($1, $2, 'queued', 0)
RETURNING id
`, fx.AgentID, otherRuntimeID).Scan(&orphanTaskID); err != nil {
		t.Fatalf("insert orphan task: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)

	// The orphan task — same agent, different runtime — must also be
	// cancelled. Without the by-agent leg in CancelAgentTasksByRuntimeOrAgent
	// this stays 'queued' and would be picked up by the other runtime.
	var orphanStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_task_queue WHERE id = $1`, orphanTaskID).Scan(&orphanStatus); err != nil {
		t.Fatalf("query orphan task: %v", err)
	}
	if orphanStatus != "cancelled" {
		t.Fatalf("expected orphan task cancelled (archived agent leftover on other runtime), got %q", orphanStatus)
	}

	// And the OTHER runtime — owned by an active member — must still be
	// online: revocation is scoped to the leaving member's owned runtimes.
	var otherStatus string
	if err := testPool.QueryRow(ctx, `SELECT status FROM agent_runtime WHERE id = $1`, otherRuntimeID).Scan(&otherStatus); err != nil {
		t.Fatalf("query other runtime: %v", err)
	}
	if otherStatus != "online" {
		t.Fatalf("expected other-member runtime to stay online, got %q", otherStatus)
	}
}

// TestDeleteMember_CancelsDeferredTasks covers the second caller of
// CancelAgentTasksByRuntimeOrAgent. Member revocation must cancel a scheduled
// fallback just like queued/running work; otherwise it could become claimable
// after its owner and runtime access have been removed.
func TestDeleteMember_CancelsDeferredTasks(t *testing.T) {
	fx := setupRevocationFixture(t, "handler-tests-revoke-deferred", "daemon-revoke-deferred")
	ctx := context.Background()

	var deferredTaskID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, fire_at)
VALUES ($1, $2, 'deferred', 0, now() + interval '1 hour')
RETURNING id
`, fx.AgentID, fx.RuntimeID).Scan(&deferredTaskID); err != nil {
		t.Fatalf("insert deferred task: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+fx.WorkspaceID+"/members/"+fx.MemberID, nil)
	req.Header.Set("X-Workspace-ID", fx.WorkspaceID)
	req = withURLParams(req, "id", fx.WorkspaceID, "memberId", fx.MemberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	assertRevoked(t, fx)

	var status string
	var completedAt *time.Time
	if err := testPool.QueryRow(ctx,
		`SELECT status, completed_at FROM agent_task_queue WHERE id = $1`,
		deferredTaskID,
	).Scan(&status, &completedAt); err != nil {
		t.Fatalf("query deferred task: %v", err)
	}
	if status != "cancelled" || completedAt == nil {
		t.Fatalf("deferred task = (%q, %v), want cancelled with completed_at", status, completedAt)
	}
}

// TestDeleteMember_NoRuntimes_DeletesMember covers the empty-revocation
// path: a member with no owned runtimes should still have their member row
// deleted by the same atomic transaction, with no spurious archive/cancel
// writes.
func TestDeleteMember_NoRuntimes_DeletesMember(t *testing.T) {
	ctx := context.Background()
	const slug = "handler-tests-revoke-no-runtimes"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)

	var wsID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO workspace (name, slug, description, issue_prefix)
VALUES ($1, $2, $3, $4)
RETURNING id
`, "Revocation no runtimes", slug, "revocation no-runtimes test", "REV").Scan(&wsID); err != nil {
		t.Fatalf("create workspace: %v", err)
	}

	if _, err := testPool.Exec(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
`, wsID, testUserID); err != nil {
		t.Fatalf("create requester member: %v", err)
	}

	var targetUserID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO "user" (name, email) VALUES ($1, $2) RETURNING id
`, "Revocation No Runtimes Target", "revocation-no-runtimes@multica.ai").Scan(&targetUserID); err != nil {
		t.Fatalf("create target user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = testPool.Exec(context.Background(), `DELETE FROM workspace WHERE id = $1`, wsID)
		_, _ = testPool.Exec(context.Background(), `DELETE FROM "user" WHERE id = $1`, targetUserID)
	})

	var memberID string
	if err := testPool.QueryRow(ctx, `
INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'admin') RETURNING id
`, wsID, targetUserID).Scan(&memberID); err != nil {
		t.Fatalf("create target member: %v", err)
	}

	w := httptest.NewRecorder()
	req := newRequest("DELETE", "/api/workspaces/"+wsID+"/members/"+memberID, nil)
	req.Header.Set("X-Workspace-ID", wsID)
	req = withURLParams(req, "id", wsID, "memberId", memberID)
	testHandler.DeleteMember(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("DeleteMember: expected 204, got %d: %s", w.Code, w.Body.String())
	}

	var memberExists bool
	if err := testPool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM member WHERE id = $1)`, memberID).Scan(&memberExists); err != nil {
		t.Fatalf("query member: %v", err)
	}
	if memberExists {
		t.Fatal("member row was not deleted")
	}
}
