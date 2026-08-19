package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/multica-ai/multica/server/pkg/remotemcp"
)

func TestLogClaimEndpointSlowIncludesPayloadFields(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelInfo})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	logClaimEndpointSlow("runtime-1", "claimed", time.Now().Add(-600*time.Millisecond), 10, 20, 30, 4096, 2, 8, 3072)

	got := logs.String()
	for _, want := range []string{
		"msg=\"claim_endpoint slow\"",
		"runtime_id=runtime-1",
		"payload_bytes=4096",
		"agent_skill_count=2",
		"builtin_skill_count=8",
		"skill_payload_bytes=3072",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("slow claim log missing %q in %s", want, got)
		}
	}
}

// slowProbeLocalSkillListStore wraps a LocalSkillListStore but blocks inside
// HasPending until the provided context is cancelled. PopPending delegates
// to the underlying store. Used to verify that a stalled probe cannot wedge
// the heartbeat — the bound context must cut it short — while the ack-safe
// PopPending path is never reached because HasPending returns an error, not
// true.
type slowProbeLocalSkillListStore struct{ LocalSkillListStore }

func (s slowProbeLocalSkillListStore) HasPending(ctx context.Context, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

type slowProbeLocalSkillImportStore struct{ LocalSkillImportStore }

func (s slowProbeLocalSkillImportStore) HasPending(ctx context.Context, _ string) (bool, error) {
	<-ctx.Done()
	return false, ctx.Err()
}

// popRecordingLocalSkillListStore counts PopPending calls so a test can assert
// that the handler never reaches the ack-unsafe side-effecting claim path
// when HasPending reports an empty queue.
type popRecordingLocalSkillListStore struct {
	LocalSkillListStore
	popCalls int
}

func (s *popRecordingLocalSkillListStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillListRequest, error) {
	s.popCalls++
	return s.LocalSkillListStore.PopPending(ctx, runtimeID)
}

type popRecordingLocalSkillImportStore struct {
	LocalSkillImportStore
	popCalls int
}

func (s *popRecordingLocalSkillImportStore) PopPending(ctx context.Context, runtimeID string) (*RuntimeLocalSkillImportRequest, error) {
	s.popCalls++
	return s.LocalSkillImportStore.PopPending(ctx, runtimeID)
}

func setHandlerTestWorkspaceRepos(t *testing.T, repos []map[string]string) {
	t.Helper()
	data, err := json.Marshal(repos)
	if err != nil {
		t.Fatalf("marshal repos: %v", err)
	}
	dbfx.Exec(t, `UPDATE workspace SET repos = $1 WHERE id = $2`, data, testWorkspaceID)
	t.Cleanup(func() {
		dbfx.Exec(t, `UPDATE workspace SET repos = $1 WHERE id = $2`, []byte("[]"), testWorkspaceID)
	})
}

// newDaemonTokenRequest creates an HTTP request with daemon token context set
// (simulating DaemonAuth middleware for mdt_ tokens).
func newDaemonTokenRequest(method, path string, body any, workspaceID, daemonID string) *http.Request {
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req := httptest.NewRequest(method, path, &buf)
	req.Header.Set("Content-Type", "application/json")
	// No X-User-ID — daemon tokens don't set it.
	ctx := middleware.WithDaemonContext(req.Context(), workspaceID, daemonID)
	return req.WithContext(ctx)
}

func TestRemoteMCPDaemonTokenForClaim(t *testing.T) {
	runtime := db.AgentRuntime{
		WorkspaceID: parseUUID(testWorkspaceID),
		DaemonID:    strToText("daemon-remote-mcp"),
	}
	raw, params, err := remoteMCPDaemonTokenForClaim(AgentTaskResponse{
		RemoteMCPConnections: []remotemcp.Connection{{ContributionKey: "mobbin"}},
	}, runtime)
	if err != nil {
		t.Fatalf("remoteMCPDaemonTokenForClaim: %v", err)
	}
	if !strings.HasPrefix(raw, "mdt_") {
		t.Fatalf("raw token has unexpected prefix")
	}
	if len(params) != 1 || params[0].TokenHash != auth.HashToken(raw) {
		t.Fatalf("daemon token params do not contain the generated token hash")
	}
	if params[0].WorkspaceID != runtime.WorkspaceID || params[0].DaemonID != "daemon-remote-mcp" {
		t.Fatalf("daemon token scope = (%v, %q)", params[0].WorkspaceID, params[0].DaemonID)
	}
	remaining := time.Until(params[0].ExpiresAt.Time)
	if remaining < 23*time.Hour || remaining > 24*time.Hour {
		t.Fatalf("daemon token lifetime = %s, want about 24h", remaining)
	}
}

func TestListDaemonWorkspaces_UserScopedAndConditional(t *testing.T) {
	w := testutil.Call(t, testHandler.ListDaemonWorkspaces, newRequest(http.MethodGet, "/api/daemon/workspaces", nil)).Want(http.StatusOK)

	var workspaces []DaemonWorkspaceResponse
	w.JSON(&workspaces)
	if len(workspaces) == 0 {
		t.Fatal("expected at least one daemon workspace")
	}
	if workspaces[0].ID == "" || workspaces[0].Name == "" {
		t.Fatalf("expected minimal id/name projection, got %+v", workspaces[0])
	}
	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("expected ETag")
	}

	req := newRequest(http.MethodGet, "/api/daemon/workspaces", nil)
	req.Header.Set("If-None-Match", etag)
	w = testutil.Call(t, testHandler.ListDaemonWorkspaces, req).Want(http.StatusNotModified)
	if w.Body.Len() != 0 {
		t.Fatalf("expected empty 304 body, got %q", w.Body.String())
	}
}

func TestListDaemonWorkspaces_DaemonTokenIsWorkspaceScoped(t *testing.T) {
	w := testutil.Call(t, testHandler.ListDaemonWorkspaces, newDaemonTokenRequest(http.MethodGet, "/api/daemon/workspaces", nil, testWorkspaceID, "daemon-test")).Want(http.StatusOK)

	var workspaces []DaemonWorkspaceResponse
	w.JSON(&workspaces)
	if len(workspaces) != 1 || workspaces[0].ID != testWorkspaceID {
		t.Fatalf("daemon-token workspaces = %+v, want only %s", workspaces, testWorkspaceID)
	}
}

func createClaimReclaimRuntime(t *testing.T, ctx context.Context, name string) string {
	t.Helper()

	runtimeID := dbfx.Runtime(t, name, testutil.Cols{
		"device_info": "claim reclaim fixture",
	})

	return runtimeID
}

func createClaimReclaimAgentAndIssue(t *testing.T, ctx context.Context, runtimeID, name string) (string, string) {
	t.Helper()

	agentID := dbfx.Agent(t, name, runtimeID)

	var issueID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES (
			$1, $2, 'in_progress', 'none', $3, 'member',
			(SELECT COALESCE(MAX(number), 82649) + 1 FROM issue WHERE workspace_id = $1),
			0
		)
		RETURNING id
	`, testWorkspaceID, name+" issue", testUserID).Scan(&issueID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM issue WHERE id = $1`, issueID) })

	return agentID, issueID
}

func createDispatchedClaimFixtureTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID, dispatchedAge string, started bool) string {
	t.Helper()

	var taskID string
	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, status, priority, dispatched_at, started_at
		)
		VALUES ($1, $2, $3, 'dispatched', 0, now() - ($4::interval), CASE WHEN $5::boolean THEN now() ELSE NULL END)
		RETURNING id
	`, agentID, runtimeID, issueID, dispatchedAge, started).Scan(&taskID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	return taskID
}

func setTaskPrepareLeaseForTest(t *testing.T, ctx context.Context, taskID, expiresIn string) {
	t.Helper()
	dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET prepare_lease_expires_at = now() + ($2::interval)
		WHERE id = $1
	`, taskID, expiresIn)
}

func claimTaskByRuntimeForTest(t *testing.T, runtimeID string) (*struct {
	ID string `json:"id"`
}, string) {
	t.Helper()

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "claim-reclaim-review")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID string `json:"id"`
		} `json:"task"`
	}
	w.JSON(&resp)
	return resp.Task, w.Body.String()
}

// claimChatIntroForTest claims the next task for a runtime and returns the
// claimed task id plus its chat_intro flag (false when the field is absent, as
// it is omitempty).
func claimChatIntroForTest(t *testing.T, runtimeID string) (string, bool, string) {
	t.Helper()

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "chat-intro-review")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID        string `json:"id"`
			ChatIntro bool   `json:"chat_intro"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimable task, got none: %s", w.Body.String())
	}
	return resp.Task.ID, resp.Task.ChatIntro, w.Body.String()
}

// TestClaimTaskByRuntime_ChatIntroGateClearsAfterUserReplies pins the MUL-4259
// fix: an is_agent_intro session drives the self-introduction prompt only on
// its first, message-less turn. Once the creator has replied, later turns on
// the same (still is_agent_intro) session must claim with chat_intro=false so
// the agent answers instead of repeating its introduction.
func TestClaimTaskByRuntime_ChatIntroGateClearsAfterUserReplies(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Chat intro gate runtime")
	agentID, _ := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Chat intro gate agent")
	// Allow more than one in-flight task so the second claim isn't blocked by
	// the first (which stays 'dispatched') on the concurrency limit.
	dbfx.Exec(t, `UPDATE agent SET max_concurrent_tasks = 5 WHERE id = $1`, agentID)

	sessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":          "👋 intro",
		"runtime_id":     runtimeID,
		"is_agent_intro": true,
	})

	seedQueuedChatTask := func() string {
		taskID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":      runtimeID,
			"chat_session_id": sessionID,
		})
		return taskID
	}

	// First turn: no user message yet → the server-driven intro run.
	introTaskID := seedQueuedChatTask()
	claimedID, chatIntro, body := claimChatIntroForTest(t, runtimeID)
	if claimedID != introTaskID {
		t.Fatalf("claimed task id = %s, want intro task %s: %s", claimedID, introTaskID, body)
	}
	if !chatIntro {
		t.Fatalf("expected chat_intro=true for the message-less intro turn: %s", body)
	}

	// The intro run finishes. Chat tasks serialize on chat_session_id, so the
	// next turn only becomes claimable once this one leaves an in-flight state.
	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'completed' WHERE id = $1`, introTaskID)

	// The creator replies: persist a user message on the same session.
	dbfx.Exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content)
		VALUES ($1, 'user', 'thanks! can you help me with X?')
	`, sessionID)

	// Second turn on the same is_agent_intro session must NOT re-introduce.
	followupTaskID := seedQueuedChatTask()
	claimedID, chatIntro, body = claimChatIntroForTest(t, runtimeID)
	if claimedID != followupTaskID {
		t.Fatalf("claimed task id = %s, want follow-up task %s: %s", claimedID, followupTaskID, body)
	}
	if chatIntro {
		t.Fatalf("expected chat_intro=false once the creator has replied: %s", body)
	}
}

func TestClaimTaskByRuntime_ReclaimsStaleDispatchedTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Stale dispatch reclaim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Stale dispatch reclaim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	task, body := claimTaskByRuntimeForTest(t, runtimeID)
	if task == nil {
		t.Fatalf("expected stale dispatched task %s to be reclaimed, got nil response: %s", taskID, body)
	}
	if task.ID != taskID {
		t.Fatalf("reclaimed task id = %s, want %s", task.ID, taskID)
	}

	var refreshed bool
	dbfx.QueryRow(t, `
		SELECT dispatched_at > now() - interval '15 seconds'
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&refreshed)
	if !refreshed {
		t.Fatal("expected reclaimed task to refresh dispatched_at")
	}
}

func TestClaimTaskByRuntime_DoesNotReclaimFreshDispatchedTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Fresh dispatch reclaim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Fresh dispatch reclaim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "75 seconds", false)

	task, body := claimTaskByRuntimeForTest(t, runtimeID)
	if task != nil {
		t.Fatalf("expected fresh dispatched task %s not to be reclaimed, got %s in %s", taskID, task.ID, body)
	}

	var stillFresh bool
	dbfx.QueryRow(t, `
		SELECT dispatched_at < now() - interval '70 seconds'
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&stillFresh)
	if !stillFresh {
		t.Fatal("expected fresh dispatched task to keep its original dispatched_at")
	}
}

func TestClaimTaskByRuntime_DoesNotReclaimActivePrepareLease(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Active prepare lease runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Active prepare lease agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "240 seconds", false)
	setTaskPrepareLeaseForTest(t, ctx, taskID, "30 seconds")

	task, body := claimTaskByRuntimeForTest(t, runtimeID)
	if task != nil {
		t.Fatalf("expected actively leased dispatched task %s not to be reclaimed, got %s in %s", taskID, task.ID, body)
	}

	var leaseActive bool
	dbfx.QueryRow(t, `
		SELECT prepare_lease_expires_at > now()
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&leaseActive)
	if !leaseActive {
		t.Fatal("expected prepare lease to remain active")
	}
}

func TestClaimTaskByRuntime_ReclaimsExpiredPrepareLease(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Expired prepare lease runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Expired prepare lease agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "240 seconds", false)
	setTaskPrepareLeaseForTest(t, ctx, taskID, "-5 seconds")

	task, body := claimTaskByRuntimeForTest(t, runtimeID)
	if task == nil {
		t.Fatalf("expected expired leased task %s to be reclaimed, got nil response: %s", taskID, body)
	}
	if task.ID != taskID {
		t.Fatalf("reclaimed task id = %s, want %s", task.ID, taskID)
	}

	var leaseRefreshed bool
	dbfx.QueryRow(t, `
		SELECT prepare_lease_expires_at > now()
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&leaseRefreshed)
	if !leaseRefreshed {
		t.Fatal("expected reclaimed task to refresh prepare lease")
	}
}

func TestExtendTaskPrepareLease(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Extend prepare lease runtime")
	otherRuntimeID := createClaimReclaimRuntime(t, ctx, "Other prepare lease runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Extend prepare lease agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+otherRuntimeID+"/tasks/"+taskID+"/prepare-lease", nil,
		testWorkspaceID, "extend-prepare-lease")
	req = withURLParams(req, "runtimeId", otherRuntimeID, "taskId", taskID)
	testutil.Call(t, testHandler.ExtendTaskPrepareLease, req).Want(http.StatusNotFound)

	req = newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/prepare-lease", nil,
		testWorkspaceID, "extend-prepare-lease")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	testutil.Call(t, testHandler.ExtendTaskPrepareLease, req).Want(http.StatusOK)

	var leaseActive bool
	dbfx.QueryRow(t, `
		SELECT prepare_lease_expires_at > now()
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&leaseActive)
	if !leaseActive {
		t.Fatal("expected prepare lease to be extended")
	}
}

func TestClaimTaskByRuntime_DoesNotReclaimAlreadyStartedTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Started dispatch reclaim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Started dispatch reclaim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", true)

	task, body := claimTaskByRuntimeForTest(t, runtimeID)
	if task != nil {
		t.Fatalf("expected started dispatched task %s not to be reclaimed, got %s in %s", taskID, task.ID, body)
	}

	var startedAtValid bool
	dbfx.QueryRow(t, `
		SELECT started_at IS NOT NULL
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&startedAtValid)
	if !startedAtValid {
		t.Fatal("expected started dispatched task to keep started_at")
	}
}

func TestClaimTaskByRuntime_DoesNotReclaimDifferentRuntimeTask(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	claimingRuntimeID := createClaimReclaimRuntime(t, ctx, "Claiming dispatch reclaim runtime")
	owningRuntimeID := createClaimReclaimRuntime(t, ctx, "Owning dispatch reclaim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, owningRuntimeID, "Different runtime reclaim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, owningRuntimeID, issueID, "120 seconds", false)

	task, body := claimTaskByRuntimeForTest(t, claimingRuntimeID)
	if task != nil {
		t.Fatalf("expected other-runtime task %s not to be reclaimed, got %s in %s", taskID, task.ID, body)
	}

	var runtimeID string
	dbfx.QueryRow(t, `
		SELECT runtime_id::text
		FROM agent_task_queue
		WHERE id = $1
	`, taskID).Scan(&runtimeID)
	if runtimeID != owningRuntimeID {
		t.Fatalf("task runtime_id = %s, want %s", runtimeID, owningRuntimeID)
	}
}

func TestClaimTaskByRuntime_SkillBundleRefsAndResolve(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Skill refs runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Skill refs agent")

	skillID := dbfx.Insert(t, "skill", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"name":         "deploy-skill-ref-test",
		"description":  "Deploy safely",
		"content":      "main skill content",
		"config":       testutil.Raw("'{}'::jsonb"),
		"created_by":   testUserID,
	})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_skill WHERE skill_id = $1`, skillID)
		testPool.Exec(ctx, `DELETE FROM skill_file WHERE skill_id = $1`, skillID)
		testPool.Exec(ctx, `DELETE FROM skill WHERE id = $1`, skillID)
	})
	dbfx.Exec(t, `INSERT INTO skill_file (skill_id, path, content) VALUES ($1, 'rules.md', 'rules content')`, skillID)
	dbfx.Exec(t, `INSERT INTO agent_skill (agent_id, skill_id) VALUES ($1, $2)`, agentID, skillID)

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "skill-refs-daemon")
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilitySkillBundlesV1)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var claimResp struct {
		Task *AgentTaskResponse `json:"task"`
	}
	w.JSON(&claimResp)
	if claimResp.Task == nil || claimResp.Task.Agent == nil {
		t.Fatalf("missing task agent in response: %s", w.Body.String())
	}
	if len(claimResp.Task.Agent.Skills) != 0 {
		t.Fatalf("slim claim returned full skills: %+v", claimResp.Task.Agent.Skills)
	}
	var ref service.AgentSkillRefData
	for _, candidate := range claimResp.Task.Agent.SkillRefs {
		if candidate.ID == skillID {
			ref = candidate
			break
		}
	}
	if ref.ID == "" || ref.Hash == "" || ref.FileCount != 1 || len(ref.Files) != 1 || ref.Files[0].SHA256 == "" {
		t.Fatalf("workspace skill ref missing manifest: %+v", ref)
	}

	staleHashBody := resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{ID: ref.ID, Source: ref.Source, Hash: "sha256:stale"}}}
	req = newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve", staleHashBody, testWorkspaceID, "skill-refs-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	w = testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusOK)
	var staleHashResp struct {
		Bundles []service.AgentSkillData `json:"bundles"`
	}
	w.JSON(&staleHashResp)
	if len(staleHashResp.Bundles) != 1 || staleHashResp.Bundles[0].Hash != ref.Hash {
		t.Fatalf("stale hash resolve did not return current bundle: %+v", staleHashResp.Bundles)
	}

	invalidRefBody := resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{ID: ref.ID, Source: ref.Source}}}
	req = newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve", invalidRefBody, testWorkspaceID, "skill-refs-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	w = testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusBadRequest)

	resolveBody := resolveSkillBundlesRequest{Skills: []resolveSkillBundleRef{{ID: ref.ID, Source: ref.Source, Hash: ref.Hash}}}
	req = newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve", resolveBody, testWorkspaceID, "skill-refs-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	w = testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusOK)
	var resolveResp struct {
		Bundles []service.AgentSkillData `json:"bundles"`
	}
	w.JSON(&resolveResp)
	if len(resolveResp.Bundles) != 1 || resolveResp.Bundles[0].Content != "main skill content" || len(resolveResp.Bundles[0].Files) != 1 || resolveResp.Bundles[0].Files[0].Content != "rules content" {
		t.Fatalf("unexpected resolved bundles: %+v", resolveResp.Bundles)
	}

	dbfx.Exec(t, `UPDATE agent_task_queue SET status = 'running', started_at = now() WHERE id = $1`, taskID)
	req = newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/"+taskID+"/skill-bundles/resolve", resolveBody, testWorkspaceID, "skill-refs-daemon")
	req = withURLParams(req, "runtimeId", runtimeID, "taskId", taskID)
	w = testutil.Call(t, testHandler.ResolveTaskSkillBundles, req).Want(http.StatusConflict)
}

// TestClaimTaskByRuntime_PopulatesWorkspaceContext verifies the claim
// response carries workspace.context so the daemon can inject the
// workspace-level system prompt into every agent brief. Regression coverage
// for MUL-2542: before this fix the field was never plumbed through, so
// even workspaces that had set a context got an empty brief.
func TestClaimTaskByRuntime_PopulatesWorkspaceContext(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	const wsContext = "All comments must be in English. Prefer concise PR descriptions."
	var prior string
	dbfx.QueryRow(t, `SELECT COALESCE(context, '') FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prior)
	dbfx.Exec(t, `UPDATE workspace SET context = $1 WHERE id = $2`, wsContext, testWorkspaceID)
	t.Cleanup(func() {
		if prior == "" {
			testPool.Exec(ctx, `UPDATE workspace SET context = NULL WHERE id = $1`, testWorkspaceID)
		} else {
			testPool.Exec(ctx, `UPDATE workspace SET context = $1 WHERE id = $2`, prior, testWorkspaceID)
		}
	})

	runtimeID := createClaimReclaimRuntime(t, ctx, "Workspace context claim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Workspace context claim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "workspace-context-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID               string `json:"id"`
			WorkspaceContext string `json:"workspace_context"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected dispatched task %s to be claimed, got nil response: %s", taskID, w.Body.String())
	}
	if resp.Task.ID != taskID {
		t.Fatalf("claimed task id = %s, want %s", resp.Task.ID, taskID)
	}
	if resp.Task.WorkspaceContext != wsContext {
		t.Errorf("workspace_context = %q, want %q", resp.Task.WorkspaceContext, wsContext)
	}
}

// TestClaimTaskByRuntime_WorkspaceContextEmptyWhenUnset verifies the field
// is omitted (empty string after JSON decode) when the workspace owner has
// not set a context. Important because the daemon's brief skips the heading
// only on empty input — a stray "context: null" coming back as the string
// "null" would render as a bogus paragraph.
func TestClaimTaskByRuntime_WorkspaceContextEmptyWhenUnset(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	var prior string
	dbfx.QueryRow(t, `SELECT COALESCE(context, '') FROM workspace WHERE id = $1`, testWorkspaceID).Scan(&prior)
	dbfx.Exec(t, `UPDATE workspace SET context = NULL WHERE id = $1`, testWorkspaceID)
	t.Cleanup(func() {
		if prior == "" {
			testPool.Exec(ctx, `UPDATE workspace SET context = NULL WHERE id = $1`, testWorkspaceID)
		} else {
			testPool.Exec(ctx, `UPDATE workspace SET context = $1 WHERE id = $2`, prior, testWorkspaceID)
		}
	})

	runtimeID := createClaimReclaimRuntime(t, ctx, "Workspace context empty claim runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Workspace context empty claim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "workspace-context-empty-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ID               string `json:"id"`
			WorkspaceContext string `json:"workspace_context"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected dispatched task %s to be claimed, got nil: %s", taskID, w.Body.String())
	}
	if resp.Task.WorkspaceContext != "" {
		t.Errorf("workspace_context = %q, want empty string when workspace.context is NULL", resp.Task.WorkspaceContext)
	}
}

func TestClaimTaskByRuntime_MissingRuntimeOwnerCancelsAndRejects(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	runtimeID := dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": testWorkspaceID,
		"daemon_id":    nil,
		"name":         "Missing owner claim runtime",
		"runtime_mode": "cloud",
		"provider":     "handler_test_runtime",
		"status":       "online",
		"device_info":  "claim missing owner fixture",
		"metadata":     testutil.Raw("'{}'::jsonb"),
		"last_seen_at": testutil.Raw("now()"),
		"visibility":   "private",
	})

	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Missing owner claim agent")
	taskID := createDispatchedClaimFixtureTask(t, ctx, agentID, runtimeID, issueID, "120 seconds", false)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil,
		testWorkspaceID, "missing-owner-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusInternalServerError)
	if !strings.Contains(w.Body.String(), "runtime owner required to mint task token") {
		t.Fatalf("ClaimTaskByRuntime body = %q, want runtime owner error", w.Body.String())
	}

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("task status = %q, want cancelled", status)
	}
}

func TestDaemonRegister_WithDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-mdt",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	}, testWorkspaceID, "test-daemon-mdt")
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	runtimes, ok := resp["runtimes"].([]any)
	if !ok || len(runtimes) == 0 {
		t.Fatalf("DaemonRegister: expected runtimes in response, got %v", resp)
	}
	if _, ok := resp["repos_version"].(string); !ok {
		t.Fatalf("DaemonRegister: expected repos_version in response, got %v", resp)
	}

	// Clean up: deregister the runtime.
	rt := runtimes[0].(map[string]any)
	runtimeID := rt["id"].(string)
	testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
}

func TestDaemonRegister_RecordsRuntimeProfileRegistrationFailure(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	profileID := insertRuntimeProfileFixture(t, ctx, "Missing Custom Codex", "codex", "missing-codex")

	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-profile-failure",
		"device_name":  "test-device",
		"failed_profiles": []map[string]any{
			{
				"profile_id":   profileID,
				"command_name": "missing-codex",
				"reason":       "command not found on PATH: missing-codex",
			},
		},
	}, testWorkspaceID, "test-daemon-profile-failure")
	req.Header.Set("X-Client-Capabilities", protocol.DaemonCapabilityLocalWorktreeV1)
	testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE profile_id = $1`, profileID)
	})

	var status string
	var metadata []byte
	dbfx.QueryRow(t, `
		SELECT status, metadata FROM agent_runtime
		WHERE workspace_id = $1 AND daemon_id = $2 AND profile_id = $3
	`, testWorkspaceID, "test-daemon-profile-failure", profileID).Scan(&status, &metadata)
	if status != "offline" {
		t.Fatalf("status = %q, want offline", status)
	}
	var meta map[string]any
	if err := json.Unmarshal(metadata, &meta); err != nil {
		t.Fatalf("unmarshal metadata: %v", err)
	}
	if meta["runtime_profile_registration_error"] != true {
		t.Fatalf("registration error metadata missing: %#v", meta)
	}
	if meta["runtime_profile_failure_reason"] != "command not found on PATH: missing-codex" {
		t.Fatalf("failure reason metadata = %#v", meta["runtime_profile_failure_reason"])
	}
	// This row is written AFTER the successful runtimes of the same request, so
	// its last_seen_at is the newest for the daemon until the first heartbeat —
	// and the worktree gates read the newest row. A failed profile must not make
	// a capable daemon look incapable for that window, nor (on the UI side) make
	// this server look too old to record capabilities at all.
	caps, _ := meta["capabilities"].([]any)
	found := false
	for _, c := range caps {
		if c == protocol.DaemonCapabilityLocalWorktreeV1 {
			found = true
		}
	}
	if !found {
		t.Fatalf("advertised capabilities missing from failure row: %#v", meta["capabilities"])
	}
}

func TestDaemonRegister_WithDaemonToken_WorkspaceMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	w := httptest.NewRecorder()
	// Daemon token is for a different workspace than the request body.
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-mdt",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	}, "00000000-0000-0000-0000-000000000000", "test-daemon-mdt")

	testHandler.DaemonRegister(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("DaemonRegister with mismatched workspace: expected 404, got %d: %s", w.Code, w.Body.String())
	}
}

func TestDaemonHeartbeat_WithDaemonToken_CrossWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// First, register a runtime using PAT (existing flow).
	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    "test-daemon-heartbeat",
		"device_name":  "test-device",
		"runtimes": []map[string]any{
			{"name": "test-runtime-hb", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)
	var regResp map[string]any
	json.NewDecoder(w.Body).Decode(&regResp)
	runtimes := regResp["runtimes"].([]any)
	runtimeID := runtimes[0].(map[string]any)["id"].(string)
	defer testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)

	// Try heartbeat with a daemon token from a DIFFERENT workspace — should fail.
	req = newDaemonTokenRequest("POST", "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, "00000000-0000-0000-0000-000000000000", "attacker-daemon")
	w = testutil.Call(t, testHandler.DaemonHeartbeat, req).Want(http.StatusNotFound)
}

// TestHandleDaemonWSHeartbeat_RuntimeGoneReturnsAckNotError pins the fix for
// issue #2391: when GetAgentRuntime returns pgx.ErrNoRows (runtime row was
// deleted server-side), the WS handler must return a successful ack with
// RuntimeGone=true rather than an error. Returning an error makes the WS hub
// log every beat at Warn — the flood the issue is about.
func TestHandleDaemonWSHeartbeat_RuntimeGoneReturnsAckNotError(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// A well-formed UUID that does NOT exist in agent_runtime. The handler
	// must turn the resulting pgx.ErrNoRows into a RuntimeGone ack.
	missingRuntime := uuid.New().String()
	ack, err := testHandler.HandleDaemonWSHeartbeat(context.Background(),
		daemonws.ClientIdentity{WorkspaceID: testWorkspaceID},
		missingRuntime, false)
	if err != nil {
		t.Fatalf("HandleDaemonWSHeartbeat: unexpected error %v", err)
	}
	if ack == nil {
		t.Fatal("HandleDaemonWSHeartbeat: nil ack for missing runtime")
	}
	if !ack.RuntimeGone {
		t.Fatalf("ack.RuntimeGone = false, want true")
	}
	if ack.Status != protocol.HeartbeatStatusRuntimeGone {
		t.Fatalf("ack.Status = %q, want %q", ack.Status, protocol.HeartbeatStatusRuntimeGone)
	}
	if ack.RuntimeID != missingRuntime {
		t.Fatalf("ack.RuntimeID = %q, want %q", ack.RuntimeID, missingRuntime)
	}
}

func TestHandleDaemonWSHeartbeat_AllowsAnyAuthorizedWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	slug := "handler-ws-heartbeat-" + uuid.New().String()
	workspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "WS Heartbeat Scope",
		"slug":         slug,
		"description":  "Temporary workspace for WS heartbeat tests",
		"issue_prefix": "HWS",
	})

	runtimeID := dbfx.Runtime(t, "WS Heartbeat Runtime", testutil.Cols{
		"workspace_id": workspaceID,
		"device_info":  "WS heartbeat runtime",
	})

	ack, err := testHandler.HandleDaemonWSHeartbeat(ctx,
		daemonws.ClientIdentity{WorkspaceIDs: []string{testWorkspaceID, workspaceID}},
		runtimeID, false)
	if err != nil {
		t.Fatalf("HandleDaemonWSHeartbeat: unexpected error %v", err)
	}
	if ack == nil || ack.RuntimeID != runtimeID {
		t.Fatalf("ack = %+v, want runtime_id %q", ack, runtimeID)
	}
}

// TestDaemonHeartbeat_HTTPRuntimeGoneReturns404 pins the HTTP-path mirror:
// pgx.ErrNoRows on the runtime lookup is the only DB error mapped to 404.
// Anything else (transient pool issue, schema mismatch, ...) must surface
// as 500 so the daemon does not mistake a hiccup for a deletion.
func TestDaemonHeartbeat_HTTPRuntimeGoneReturns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	missingRuntime := uuid.New().String()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": missingRuntime,
	}, testWorkspaceID, "test-daemon")
	w := testutil.Call(t, testHandler.DaemonHeartbeat, req).Want(http.StatusNotFound)
	if !strings.Contains(strings.ToLower(w.Body.String()), "runtime not found") {
		t.Fatalf("expected 'runtime not found' body, got %s", w.Body.String())
	}
}

// TestDaemonHeartbeat_SlowProbeDoesNotWedge pins the invariant that a stalled
// HasPending probe cannot wedge the heartbeat endpoint past the per-probe
// timeout. The probe is the only bounded call; PopPending is ack-safe-
// critical and is intentionally left unbounded. Without the probe bound the
// heartbeat would hang on a slow shared store.
func TestDaemonHeartbeat_SlowProbeDoesNotWedge(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	origList := testHandler.LocalSkillListStore
	origImport := testHandler.LocalSkillImportStore
	testHandler.LocalSkillListStore = slowProbeLocalSkillListStore{origList}
	testHandler.LocalSkillImportStore = slowProbeLocalSkillImportStore{origImport}
	t.Cleanup(func() {
		testHandler.LocalSkillListStore = origList
		testHandler.LocalSkillImportStore = origImport
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, "runtime-local-skills-daemon")

	start := time.Now()
	testHandler.DaemonHeartbeat(w, req)
	elapsed := time.Since(start)

	if w.Code != http.StatusOK {
		t.Fatalf("DaemonHeartbeat with slow probes: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	// Two bounded probes at 1s each + a small fixed slack.
	if elapsed > 3*time.Second {
		t.Fatalf("DaemonHeartbeat took %s; expected fast return despite slow probes", elapsed)
	}
}

// TestDaemonHeartbeat_EmptyQueueSkipsPopPending pins the ack-safety property:
// when HasPending reports no work, the heartbeat must NOT invoke PopPending,
// because PopPending's Redis implementation has non-atomic side effects that
// a client-side cancel cannot cleanly un-run (see GH #1637 review).
func TestDaemonHeartbeat_EmptyQueueSkipsPopPending(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	runtimeID := createRuntimeLocalSkillTestRuntime(t, testUserID)

	origList := testHandler.LocalSkillListStore
	origImport := testHandler.LocalSkillImportStore
	listSpy := &popRecordingLocalSkillListStore{LocalSkillListStore: origList}
	importSpy := &popRecordingLocalSkillImportStore{LocalSkillImportStore: origImport}
	testHandler.LocalSkillListStore = listSpy
	testHandler.LocalSkillImportStore = importSpy
	t.Cleanup(func() {
		testHandler.LocalSkillListStore = origList
		testHandler.LocalSkillImportStore = origImport
	})

	req := newDaemonTokenRequest(http.MethodPost, "/api/daemon/heartbeat", map[string]any{
		"runtime_id": runtimeID,
	}, testWorkspaceID, "runtime-local-skills-daemon")
	testutil.Call(t, testHandler.DaemonHeartbeat, req).Want(http.StatusOK)
	if listSpy.popCalls != 0 {
		t.Fatalf("expected 0 PopPending calls on empty list queue, got %d", listSpy.popCalls)
	}
	if importSpy.popCalls != 0 {
		t.Fatalf("expected 0 PopPending calls on empty import queue, got %d", importSpy.popCalls)
	}
}

func TestGetTaskStatus_WithDaemonToken_CrossWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Create a task in the test workspace.
	var issueID, taskID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type)
		VALUES ($1, 'daemon-auth-test-issue', 'todo', 'medium', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	if err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)

	// Get an agent and runtime from the test workspace.
	var agentID, runtimeID string
	err = testPool.QueryRow(context.Background(), `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)
	if err != nil {
		t.Fatalf("setup: get agent: %v", err)
	}

	err = testPool.QueryRow(context.Background(), `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, 'queued', $3)
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID)
	if err != nil {
		t.Fatalf("setup: create task: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	// Try GetTaskStatus with a daemon token from a DIFFERENT workspace — should fail.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/status", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.GetTaskStatus(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("GetTaskStatus with cross-workspace token: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Same request with the CORRECT workspace should succeed.
	w = httptest.NewRecorder()
	req = newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/status", nil,
		testWorkspaceID, "legit-daemon")
	req = req.WithContext(context.WithValue(
		middleware.WithDaemonContext(req.Context(), testWorkspaceID, "legit-daemon"),
		chi.RouteCtxKey, rctx))

	testHandler.GetTaskStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GetTaskStatus with correct workspace token: expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

// TestGetTaskStatus_TransientDBError_Returns500 verifies that a transient DB
// error from GetAgentTask is reported as 500 rather than 404. The daemon
// uses 404+"task not found" as a hard cancel signal; a transient lookup
// failure must therefore not be smuggled into that body, otherwise a single
// DB hiccup would kill an in-flight agent.
func TestGetTaskStatus_TransientDBError_Returns500(t *testing.T) {
	h := &Handler{}
	h.Queries = db.New(&mockDB{getUserErr: errors.New("connection reset by peer")})

	req := newDaemonTokenRequest("GET", "/api/daemon/tasks/00000000-0000-0000-0000-000000000001/status", nil,
		"00000000-0000-0000-0000-000000000000", "test-daemon")
	req = withURLParam(req, "taskId", "00000000-0000-0000-0000-000000000001")
	w := testutil.Call(t, h.GetTaskStatus, req).Want(http.StatusInternalServerError)
	if strings.Contains(w.Body.String(), "task not found") {
		t.Fatalf("transient DB error must not surface as %q (daemon treats that as a deletion): %s", "task not found", w.Body.String())
	}
}

// TestGetTaskStatus_ErrNoRows_Returns404 verifies that an actually-missing
// task row still returns the 404+"task not found" body the daemon relies on
// to interrupt the running agent.
func TestGetTaskStatus_ErrNoRows_Returns404(t *testing.T) {
	h := &Handler{}
	h.Queries = db.New(&mockDB{getUserErr: pgx.ErrNoRows})

	req := newDaemonTokenRequest("GET", "/api/daemon/tasks/00000000-0000-0000-0000-000000000001/status", nil,
		"00000000-0000-0000-0000-000000000000", "test-daemon")
	req = withURLParam(req, "taskId", "00000000-0000-0000-0000-000000000001")
	w := testutil.Call(t, h.GetTaskStatus, req).Want(http.StatusNotFound)
	if !strings.Contains(w.Body.String(), "task not found") {
		t.Fatalf("ErrNoRows: expected body to contain %q, got %s", "task not found", w.Body.String())
	}
}

func TestGetIssueGCCheck_WithDaemonToken_CrossWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Create an issue in the test workspace. The daemon GC endpoint returns
	// only status + updated_at, so a "done" issue exercises the typical path.
	var issueID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type)
		VALUES ($1, 'gc-check-auth-test-issue', 'done', 'medium', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	if err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)

	// Cross-workspace daemon token must be rejected with 404 — same status
	// code as "issue not found" so there is no UUID enumeration oracle.
	req := newDaemonTokenRequest("GET", "/api/daemon/issues/"+issueID+"/gc-check", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	req = withURLParam(req, "issueId", issueID)
	w := testutil.Call(t, testHandler.GetIssueGCCheck, req).Want(http.StatusNotFound)

	// Same-workspace daemon token succeeds and returns status + updated_at.
	req = newDaemonTokenRequest("GET", "/api/daemon/issues/"+issueID+"/gc-check", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "issueId", issueID)
	w = testutil.Call(t, testHandler.GetIssueGCCheck, req).Want(http.StatusOK)

	var resp struct {
		Status    string `json:"status"`
		UpdatedAt string `json:"updated_at"`
	}
	w.JSON(&resp)
	if resp.Status != "done" {
		t.Fatalf("expected status %q, got %q", "done", resp.Status)
	}
	if resp.UpdatedAt == "" {
		t.Fatal("expected updated_at to be set")
	}
}

func TestBatchIssueGCCheck_WithDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var issueID string
	err := testPool.QueryRow(context.Background(), `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type)
		VALUES ($1, 'batch-gc-check-auth-test-issue', 'done', 'medium', $2, 'member')
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	if err != nil {
		t.Fatalf("setup: create issue: %v", err)
	}
	defer testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID)

	missingID := "00000000-0000-0000-0000-000000000099"
	req := newDaemonTokenRequest("POST", "/api/daemon/workspaces/"+testWorkspaceID+"/issues/gc-check", map[string]any{
		"issue_ids": []string{issueID, missingID},
	}, testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	w := testutil.Call(t, testHandler.BatchIssueGCCheck, req).Want(http.StatusOK)
	var resp struct {
		Issues []struct {
			ID        string `json:"id"`
			Found     bool   `json:"found"`
			Status    string `json:"status"`
			UpdatedAt string `json:"updated_at"`
		} `json:"issues"`
	}
	w.JSON(&resp)
	if len(resp.Issues) != 2 {
		t.Fatalf("issues length = %d, want 2", len(resp.Issues))
	}
	if got := resp.Issues[0]; got.ID != issueID || !got.Found || got.Status != "done" || got.UpdatedAt == "" {
		t.Fatalf("found issue result = %+v", got)
	}
	if got := resp.Issues[1]; got.ID != missingID || got.Found || got.Status != "" || got.UpdatedAt != "" {
		t.Fatalf("missing issue result = %+v", got)
	}

	// A token for another workspace is rejected before any issue lookup.
	req = newDaemonTokenRequest("POST", "/api/daemon/workspaces/"+testWorkspaceID+"/issues/gc-check", map[string]any{
		"issue_ids": []string{issueID},
	}, "00000000-0000-0000-0000-000000000000", "attacker-daemon")
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	w = testutil.Call(t, testHandler.BatchIssueGCCheck, req).Want(http.StatusNotFound)
}

// withURLParams merges the given chi URL parameters into the request context.
// Unlike calling withURLParam twice (which replaces the whole chi.RouteContext
// and loses earlier params), this preserves previously-added params.
func withURLParams(req *http.Request, kv ...string) *http.Request {
	rctx := chi.NewRouteContext()
	if existing, ok := req.Context().Value(chi.RouteCtxKey).(*chi.Context); ok && existing != nil {
		for i, key := range existing.URLParams.Keys {
			rctx.URLParams.Add(key, existing.URLParams.Values[i])
		}
	}
	for i := 0; i+1 < len(kv); i += 2 {
		rctx.URLParams.Add(kv[i], kv[i+1])
	}
	return req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
}

func TestListTaskMessagesByUser_InvalidTaskIDReturnsBadRequest(t *testing.T) {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/tasks/optimistic-optimistic-1778739487737/messages", nil)
	req = withURLParams(req, "taskId", "optimistic-optimistic-1778739487737")

	(&Handler{}).ListTaskMessagesByUser(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid task id, got %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "task_id") {
		t.Fatalf("expected task_id validation error, got %s", w.Body.String())
	}
}

// setupForeignWorkspaceFixture creates an isolated workspace (not reachable
// from testUserID) with its own agent, runtime, issue, and queued task.
// Returns (issueID, taskID). All rows are cleaned up when the test ends.
func setupForeignWorkspaceFixture(t *testing.T) (string, string) {
	t.Helper()

	foreignWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Foreign Workspace",
		"slug":         "foreign-idor-tests",
		"description":  "Cross-tenant IDOR test workspace",
		"issue_prefix": "FOR",
	})

	runtimeID := dbfx.Insert(t, "agent_runtime", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"daemon_id":    nil,
		"name":         "Foreign Runtime",
		"runtime_mode": "cloud",
		"provider":     "foreign_runtime",
		"status":       "online",
		"device_info":  "Foreign runtime",
		"metadata":     testutil.Raw("'{}'::jsonb"),
		"last_seen_at": testutil.Raw("now()"),
	})

	agentID := dbfx.Insert(t, "agent", testutil.Cols{
		"workspace_id":         foreignWorkspaceID,
		"name":                 "Foreign Agent",
		"description":          "",
		"runtime_mode":         "cloud",
		"runtime_config":       testutil.Raw("'{}'::jsonb"),
		"runtime_id":           runtimeID,
		"visibility":           "workspace",
		"max_concurrent_tasks": 1,
	})

	issueID := dbfx.Issue(t, "foreign-workspace-issue", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"priority":     "medium",
		"creator_id":   agentID,
		"creator_type": "agent",
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"issue_id":   issueID,
		"runtime_id": runtimeID,
	})

	return issueID, taskID
}

// TestGetActiveTaskForIssue_CrossWorkspace_Returns404 verifies that a member of
// workspace A cannot discover tasks for an issue in workspace B by passing
// B's issue UUID in the URL while keeping A in X-Workspace-ID.
func TestGetActiveTaskForIssue_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignIssueID, _ := setupForeignWorkspaceFixture(t)

	req := newRequest("GET", "/api/issues/"+foreignIssueID+"/active-task", nil)
	req = withURLParam(req, "id", foreignIssueID)
	testutil.Call(t, testHandler.GetActiveTaskForIssue, req).Want(http.StatusNotFound)
}

// TestCancelTask_CrossWorkspace_Returns404 verifies that a member of workspace
// A cannot cancel a task that lives in workspace B. Critically, the task must
// remain in its original status — no side effect before the access check.
func TestCancelTask_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignIssueID, foreignTaskID := setupForeignWorkspaceFixture(t)

	req := newRequest("POST", "/api/issues/"+foreignIssueID+"/tasks/"+foreignTaskID+"/cancel", nil)
	req = withURLParams(req, "id", foreignIssueID, "taskId", foreignTaskID)
	testutil.Call(t, testHandler.CancelTask, req).Want(http.StatusNotFound)

	// The foreign task must not have been cancelled.
	var status string
	dbfx.QueryRow(t,
		`SELECT status FROM agent_task_queue WHERE id = $1`, foreignTaskID,
	).Scan(&status)
	if status != "queued" {
		t.Fatalf("foreign task status was mutated: expected 'queued', got %q", status)
	}
}

// TestCancelTask_TaskBelongsToDifferentIssue_Returns404 verifies that a task
// UUID belonging to a *different* issue in the *same* accessible workspace
// cannot be cancelled by routing it through another issue's URL. This guards
// against the weaker fix that only validates the issue→workspace binding.
func TestCancelTask_TaskBelongsToDifferentIssue_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	// Issue X — the task's real parent.
	var issueXID, taskID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cancel-crossissue-x', 'todo', 'medium', $2, 'member', 91001, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueXID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueXID) })

	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, 'queued', $3)
		RETURNING id
	`, agentID, issueXID, runtimeID).Scan(&taskID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	// Issue Y — a sibling in the same workspace, used only as the URL cover.
	issueYID := dbfx.Issue(t, "cancel-crossissue-y", testutil.Cols{
		"priority": "medium",
		"number":   91002,
	})

	req := newRequest("POST", "/api/issues/"+issueYID+"/tasks/"+taskID+"/cancel", nil)
	req = withURLParams(req, "id", issueYID, "taskId", taskID)
	testutil.Call(t, testHandler.CancelTask, req).Want(http.StatusNotFound)

	var status string
	dbfx.QueryRow(t,
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status)
	if status != "queued" {
		t.Fatalf("task status was mutated: expected 'queued', got %q", status)
	}
}

// TestCancelTask_SameIssue_Succeeds is the happy-path companion to the two
// negative tests above — same workspace, correct issue→task pairing → 200.
func TestCancelTask_SameIssue_Succeeds(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	var issueID, taskID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'cancel-happy-path', 'todo', 'medium', $2, 'member', 91003, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&issueID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, issueID) })

	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, 'queued', $3)
		RETURNING id
	`, agentID, issueID, runtimeID).Scan(&taskID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, taskID) })

	req := newRequest("POST", "/api/issues/"+issueID+"/tasks/"+taskID+"/cancel", nil)
	req = withURLParams(req, "id", issueID, "taskId", taskID)
	testutil.Call(t, testHandler.CancelTask, req).Want(http.StatusOK)
}

// TestListTasksByIssue_CrossWorkspace_Returns404 verifies that task history
// is not readable across workspaces via a bare issue UUID.
func TestListTasksByIssue_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignIssueID, _ := setupForeignWorkspaceFixture(t)

	req := newRequest("GET", "/api/issues/"+foreignIssueID+"/task-runs", nil)
	req = withURLParam(req, "id", foreignIssueID)
	testutil.Call(t, testHandler.ListTasksByIssue, req).Want(http.StatusNotFound)
}

// TestGetIssueUsage_CrossWorkspace_Returns404 verifies that per-issue token
// usage is not readable across workspaces via a bare issue UUID.
func TestGetIssueUsage_CrossWorkspace_Returns404(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	foreignIssueID, _ := setupForeignWorkspaceFixture(t)

	req := newRequest("GET", "/api/issues/"+foreignIssueID+"/usage", nil)
	req = withURLParam(req, "id", foreignIssueID)
	testutil.Call(t, testHandler.GetIssueUsage, req).Want(http.StatusNotFound)
}

func TestGetDaemonWorkspaceRepos_WithDaemonToken(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": "git@example.com:team/api.git", "description": "API"},
		{"url": "  git@example.com:team/web.git  ", "description": " Web "},
	})

	req := newDaemonTokenRequest("GET", "/api/daemon/workspaces/"+testWorkspaceID+"/repos", nil, testWorkspaceID, "test-daemon-mdt")
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	w := testutil.Call(t, testHandler.GetDaemonWorkspaceRepos, req).Want(http.StatusOK)

	var resp struct {
		WorkspaceID  string              `json:"workspace_id"`
		Repos        []map[string]string `json:"repos"`
		ReposVersion string              `json:"repos_version"`
	}
	w.JSON(&resp)

	if resp.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected workspace_id %s, got %s", testWorkspaceID, resp.WorkspaceID)
	}
	if len(resp.Repos) != 2 {
		t.Fatalf("expected 2 repos, got %d", len(resp.Repos))
	}
	if resp.Repos[1]["url"] != "git@example.com:team/web.git" {
		t.Fatalf("expected trimmed repo URL, got %q", resp.Repos[1]["url"])
	}
	if resp.ReposVersion == "" {
		t.Fatal("expected repos_version to be set")
	}
}

func TestGetDaemonWorkspaceRepos_WithDaemonToken_WorkspaceMismatch(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	req := newDaemonTokenRequest("GET", "/api/daemon/workspaces/"+testWorkspaceID+"/repos", nil, "00000000-0000-0000-0000-000000000000", "test-daemon-mdt")
	req = withURLParam(req, "workspaceId", testWorkspaceID)
	testutil.Call(t, testHandler.GetDaemonWorkspaceRepos, req).Want(http.StatusNotFound)
}

func TestGetDaemonWorkspaceRepos_VersionIgnoresOrderAndDescription(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": "git@example.com:team/api.git", "description": "API"},
		{"url": "git@example.com:team/web.git", "description": "Web"},
	})

	getReposVersion := func() string {
		t.Helper()
		req := newDaemonTokenRequest("GET", "/api/daemon/workspaces/"+testWorkspaceID+"/repos", nil, testWorkspaceID, "test-daemon-mdt")
		req = withURLParam(req, "workspaceId", testWorkspaceID)
		w := testutil.Call(t, testHandler.GetDaemonWorkspaceRepos, req).Want(http.StatusOK)
		var resp struct {
			ReposVersion string `json:"repos_version"`
		}
		w.JSON(&resp)
		return resp.ReposVersion
	}

	version1 := getReposVersion()

	dbfx.Exec(t, `UPDATE workspace SET repos = $1 WHERE id = $2`, []byte(`[{"url":"git@example.com:team/web.git","description":"frontend"},{"url":"git@example.com:team/api.git","description":"backend"}]`), testWorkspaceID)
	version2 := getReposVersion()
	if version1 != version2 {
		t.Fatalf("expected repos_version to ignore order/description changes, got %s vs %s", version1, version2)
	}

	dbfx.Exec(t, `UPDATE workspace SET repos = $1 WHERE id = $2`, []byte(`[{"url":"git@example.com:team/api.git","description":"backend"},{"url":"git@example.com:team/mobile.git","description":"mobile"}]`), testWorkspaceID)
	version3 := getReposVersion()
	if strings.EqualFold(version2, version3) {
		t.Fatalf("expected repos_version to change when URL set changes, got %s", version3)
	}
}

// TestDaemonRegister_MergesLegacyDaemonIDRuntime simulates the migration path
// for an existing user whose runtime was previously keyed on a hostname-derived
// daemon_id (e.g. "MacBook-Pro.local"). After the daemon switches to a stable
// UUID, the registration payload lists the old id under `legacy_daemon_ids`.
// The server must:
//
//   - reassign every agent pointing at the old runtime row to the new row,
//   - reassign every task (agent_task_queue.runtime_id) onto the new row,
//   - delete the stale old row so there's exactly one runtime per machine,
//   - record the legacy daemon_id on the new row for traceability.
//
// This is the acceptance path from MUL-975: hostname drift must no longer
// orphan agents on stale runtime rows.
func TestDaemonRegister_MergesLegacyDaemonIDRuntime(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const legacyDaemonID = "TestMachine.local"
	const newDaemonID = "0192a7a0-9ab3-7c3f-9f1c-4a6fe8c4e801"

	// Seed a legacy runtime row keyed on the hostname-derived id.
	legacyRuntimeID := dbfx.Runtime(t, "legacy-runtime", testutil.Cols{
		"daemon_id":    legacyDaemonID,
		"runtime_mode": "local",
		"provider":     "claude",
		"status":       "offline",
		"device_info":  "TestMachine.local",
		"last_seen_at": testutil.Raw("now() - interval '1 hour'"),
	})

	// An agent bound to the legacy runtime.
	legacyAgentID := dbfx.Insert(t, "agent", testutil.Cols{
		"workspace_id":         testWorkspaceID,
		"name":                 "legacy-agent",
		"runtime_mode":         "local",
		"runtime_config":       testutil.Raw("'{}'::jsonb"),
		"runtime_id":           legacyRuntimeID,
		"visibility":           "workspace",
		"max_concurrent_tasks": 1,
	})

	// An issue + task also bound to the legacy runtime (tasks have ON DELETE
	// CASCADE, so without reassignment deleting the legacy row would silently
	// drop historical tasks).
	var legacyIssueID, legacyTaskID string
	dbfx.QueryRow(t, `
		INSERT INTO issue (workspace_id, title, status, priority, creator_id, creator_type, number, position)
		VALUES ($1, 'legacy-task-owner', 'todo', 'medium', $2, 'member', 97501, 0)
		RETURNING id
	`, testWorkspaceID, testUserID).Scan(&legacyIssueID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM issue WHERE id = $1`, legacyIssueID) })

	dbfx.QueryRow(t, `
		INSERT INTO agent_task_queue (agent_id, issue_id, status, runtime_id)
		VALUES ($1, $2, 'completed', $3)
		RETURNING id
	`, legacyAgentID, legacyIssueID, legacyRuntimeID).Scan(&legacyTaskID)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_task_queue WHERE id = $1`, legacyTaskID)
	})

	// Register under the new stable UUID, declaring the prior hostname-derived
	// id as legacy. The handler should merge the legacy row into the new one.
	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id":      testWorkspaceID,
		"daemon_id":         newDaemonID,
		"legacy_daemon_ids": []string{legacyDaemonID},
		"device_name":       "TestMachine",
		"runtimes": []map[string]any{
			{"name": "test-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	w.JSON(&resp)
	runtimes := resp["runtimes"].([]any)
	newRuntimeID := runtimes[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID)
	})

	if newRuntimeID == legacyRuntimeID {
		t.Fatalf("expected a new runtime row, got the legacy id back")
	}

	// Agent should now point at the new runtime.
	var agentRuntimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, legacyAgentID).Scan(&agentRuntimeID)
	if agentRuntimeID != newRuntimeID {
		t.Fatalf("agent not reassigned: got runtime_id=%s, want %s", agentRuntimeID, newRuntimeID)
	}

	// Task should be reassigned (not dropped).
	var taskRuntimeID string
	dbfx.QueryRow(t, `SELECT runtime_id FROM agent_task_queue WHERE id = $1`, legacyTaskID).Scan(&taskRuntimeID)
	if taskRuntimeID != newRuntimeID {
		t.Fatalf("task not reassigned: got runtime_id=%s, want %s", taskRuntimeID, newRuntimeID)
	}

	// Legacy runtime row must be gone — no more "online + offline" duplicates
	// for the same machine.
	var legacyCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_runtime WHERE id = $1`, legacyRuntimeID).Scan(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("expected legacy runtime row to be deleted, still present")
	}

	// New row should record which legacy id it subsumed, for debug/audit.
	var legacyTrace *string
	dbfx.QueryRow(t, `SELECT legacy_daemon_id FROM agent_runtime WHERE id = $1`, newRuntimeID).Scan(&legacyTrace)
	if legacyTrace == nil || *legacyTrace != legacyDaemonID {
		t.Fatalf("expected legacy_daemon_id=%q, got %v", legacyDaemonID, legacyTrace)
	}
}

// TestDaemonRegister_MergesLegacyDaemonIDRuntime_ReverseDotLocal covers the
// direction missed by the initial implementation: the stored runtime row is
// `host` (no `.local`) but the daemon's current `os.Hostname()` now returns
// `host.local`. The daemon must emit the bare variant as a legacy candidate
// and the server must match it.
func TestDaemonRegister_MergesLegacyDaemonIDRuntime_ReverseDotLocal(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const legacyDaemonID = "ReverseDotLocalHost"        // stored without .local
	const emittedLegacyID = "ReverseDotLocalHost.local" // daemon now reports with .local
	const newDaemonID = "0192a7b0-0011-7ee9-9c21-30a5bcf86aa2"

	legacyRuntimeID := dbfx.Runtime(t, "legacy-runtime-reverse", testutil.Cols{
		"daemon_id":    legacyDaemonID,
		"runtime_mode": "local",
		"provider":     "claude",
		"status":       "offline",
	})

	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id":      testWorkspaceID,
		"daemon_id":         newDaemonID,
		"legacy_daemon_ids": []string{"ReverseDotLocalHost", emittedLegacyID},
		"device_name":       "ReverseDotLocalHost",
		"runtimes": []map[string]any{
			{"name": "reverse-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	newRuntimeID := resp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID)
	})

	var legacyCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_runtime WHERE id = $1`, legacyRuntimeID).Scan(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("expected legacy row to be merged and deleted, still present")
	}
}

// TestDaemonRegister_MergesLegacyDaemonIDRuntime_CaseDrift verifies that
// case-only drift in os.Hostname() output (e.g. `Jiayuans-MacBook-Pro.local`
// vs `jiayuans-macbook-pro.local`) still merges the legacy row. The daemon
// emits the id in its current casing; the server-side lookup uses LOWER() on
// both sides so stored and emitted casings can differ without orphaning.
func TestDaemonRegister_MergesLegacyDaemonIDRuntime_CaseDrift(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const storedDaemonID = "Jiayuans-MacBook-Pro.local"  // DB has original mixed case
	const emittedLegacyID = "jiayuans-macbook-pro.local" // Daemon now reports lowercased
	const newDaemonID = "0192a7b0-0022-7ee9-9c21-30a5bcf86aa3"

	legacyRuntimeID := dbfx.Runtime(t, "legacy-runtime-case", testutil.Cols{
		"daemon_id":    storedDaemonID,
		"runtime_mode": "local",
		"provider":     "claude",
		"status":       "offline",
	})

	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id":      testWorkspaceID,
		"daemon_id":         newDaemonID,
		"legacy_daemon_ids": []string{emittedLegacyID},
		"device_name":       "jiayuans-macbook-pro",
		"runtimes": []map[string]any{
			{"name": "case-drift-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	newRuntimeID := resp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID)
	})

	var legacyCount int
	dbfx.QueryRow(t, `SELECT count(*) FROM agent_runtime WHERE id = $1`, legacyRuntimeID).Scan(&legacyCount)
	if legacyCount != 0 {
		t.Fatalf("expected case-drift legacy row to be merged and deleted, still present")
	}

	var legacyTrace *string
	dbfx.QueryRow(t, `SELECT legacy_daemon_id FROM agent_runtime WHERE id = $1`, newRuntimeID).Scan(&legacyTrace)
	if legacyTrace == nil || *legacyTrace != emittedLegacyID {
		t.Fatalf("expected legacy_daemon_id trace = %q, got %v", emittedLegacyID, legacyTrace)
	}
}

// TestDaemonRegister_MergesAllCaseDuplicateLegacyRuntimes covers the case
// where the DB already holds *two* legacy runtime rows that differ only in
// casing (e.g. `Jiayuans-MacBook-Pro.local` AND `jiayuans-macbook-pro.local`
// coexist under the same workspace+provider because earlier hostname drift
// already minted a duplicate). A single-row lookup would merge only one of
// them and leave the other orphaned; the lookup must return every row whose
// daemon_id case-insensitively matches and the handler must consolidate them
// all. This is the acceptance-standard path: after registration there must
// not be two runtime rows for the same machine.
func TestDaemonRegister_MergesAllCaseDuplicateLegacyRuntimes(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const storedUpperID = "DupHost.local"
	const storedLowerID = "duphost.local"
	const newDaemonID = "0192a7b0-0033-7ee9-9c21-30a5bcf86aa4"

	var legacyUpperID, legacyLowerID string
	dbfx.QueryRow(t, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
		VALUES ($1, $2, 'legacy-upper', 'local', 'claude', 'offline', '', '{}'::jsonb, $3, now() - interval '2 hours')
		RETURNING id
	`, testWorkspaceID, storedUpperID, testUserID).Scan(&legacyUpperID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, legacyUpperID) })

	dbfx.QueryRow(t, `
		INSERT INTO agent_runtime (workspace_id, daemon_id, name, runtime_mode, provider, status, device_info, metadata, owner_id, last_seen_at)
		VALUES ($1, $2, 'legacy-lower', 'local', 'claude', 'offline', '', '{}'::jsonb, $3, now() - interval '1 hour')
		RETURNING id
	`, testWorkspaceID, storedLowerID, testUserID).Scan(&legacyLowerID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, legacyLowerID) })

	// Bind one agent to each legacy row to verify both sides get reassigned.
	var upperAgentID, lowerAgentID string
	dbfx.QueryRow(t, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
		VALUES ($1, 'dup-agent-upper', 'local', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, testWorkspaceID, legacyUpperID).Scan(&upperAgentID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, upperAgentID) })
	dbfx.QueryRow(t, `
		INSERT INTO agent (workspace_id, name, runtime_mode, runtime_config, runtime_id, visibility, max_concurrent_tasks)
		VALUES ($1, 'dup-agent-lower', 'local', '{}'::jsonb, $2, 'workspace', 1)
		RETURNING id
	`, testWorkspaceID, legacyLowerID).Scan(&lowerAgentID)
	t.Cleanup(func() { testPool.Exec(context.Background(), `DELETE FROM agent WHERE id = $1`, lowerAgentID) })

	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id":      testWorkspaceID,
		"daemon_id":         newDaemonID,
		"legacy_daemon_ids": []string{storedLowerID}, // a single candidate must resolve both stored casings
		"device_name":       "DupHost",
		"runtimes": []map[string]any{
			{"name": "dup-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	newRuntimeID := resp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, newRuntimeID)
	})

	// Both case-duplicate legacy rows must be gone — not just one.
	var stillPresent int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM agent_runtime WHERE id = ANY($1)
	`, []string{legacyUpperID, legacyLowerID}).Scan(&stillPresent)
	if stillPresent != 0 {
		t.Fatalf("expected both case-duplicate legacy rows merged and deleted, %d still present", stillPresent)
	}

	// Both agents must point at the new runtime.
	for _, agentID := range []string{upperAgentID, lowerAgentID} {
		var runtimeID string
		dbfx.QueryRow(t, `SELECT runtime_id FROM agent WHERE id = $1`, agentID).Scan(&runtimeID)
		if runtimeID != newRuntimeID {
			t.Fatalf("agent %s not reassigned: runtime_id=%s, want %s", agentID, runtimeID, newRuntimeID)
		}
	}
}

// TestDaemonRegister_LegacyIDNoMatchIsNoop guards the common case where the
// daemon sends legacy candidates but no matching row exists (e.g. first
// registration on a fresh machine). Registration must still succeed, the new
// row must not have a spurious legacy_daemon_id recorded, and no unrelated
// rows may be touched.
func TestDaemonRegister_LegacyIDNoMatchIsNoop(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id":      testWorkspaceID,
		"daemon_id":         "0192a7a1-5e3c-7be9-9a7d-6e0f1cb3deab",
		"legacy_daemon_ids": []string{"NeverSeenHost", "NeverSeenHost.local"},
		"device_name":       "NeverSeenHost",
		"runtimes": []map[string]any{
			{"name": "fresh-runtime", "type": "claude", "version": "1.0.0", "status": "online"},
		},
	})
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)

	var resp map[string]any
	json.NewDecoder(w.Body).Decode(&resp)
	runtimeID := resp["runtimes"].([]any)[0].(map[string]any)["id"].(string)
	t.Cleanup(func() {
		testPool.Exec(context.Background(), `DELETE FROM agent_runtime WHERE id = $1`, runtimeID)
	})

	var legacy *string
	dbfx.QueryRow(t, `SELECT legacy_daemon_id FROM agent_runtime WHERE id = $1`, runtimeID).Scan(&legacy)
	if legacy != nil {
		t.Fatalf("expected legacy_daemon_id to stay NULL when no merge occurred, got %q", *legacy)
	}
}

// Regression test for #1224: tasks linked only via AutopilotRunID (run_only
// autopilots) must resolve to the autopilot's workspace. Before the fix,
// resolveTaskWorkspaceID fell through and every StartTask call returned 404.
func TestStartTask_AutopilotRunOnlyTask_ResolvesWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "run_only fixture",
		"assignee_id":     agentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)

	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "manual",
		"status":       "running",
	})

	// issue_id is explicitly NULL — the condition that used to trigger 404.
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":       runtimeID,
		"issue_id":         nil,
		"status":           "dispatched",
		"autopilot_run_id": runID,
	})
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	// Cross-workspace daemon token must still 404.
	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/start", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.StartTask(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("StartTask with cross-workspace token: expected 404, got %d: %s", w.Code, w.Body.String())
	}

	// Same-workspace daemon token must succeed — this is the bug in #1224.
	w = httptest.NewRecorder()
	req = newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/start", nil,
		testWorkspaceID, "legit-daemon")
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.StartTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("StartTask for run_only autopilot task: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var status string
	dbfx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id = $1`, taskID).Scan(&status)
	if status != "running" {
		t.Fatalf("expected task status 'running' after StartTask, got %q", status)
	}
}

// ClaimTaskByRuntime must surface the issue's project github_repo resources
// as resp.Repos and hide the workspace-bound repos. Without this the agent
// would see two repo lists in the meta-skill and have no signal about which
// belongs to the current issue.
func TestClaimTask_ProjectGithubReposOverrideWorkspaceRepos(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Workspace repos: two of them, neither matches the project repo URL.
	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": "https://github.com/example/workspace-repo-a", "description": "ws a"},
		{"url": "https://github.com/example/workspace-repo-b", "description": "ws b"},
	})

	// Project + project_resource(github_repo) with a URL that is NOT in the
	// workspace's repos list.
	projectID := dbfx.Project(t, "Claim project repo override")

	const projectRepoURL = "https://github.com/example/project-only-repo"
	const projectRepoRef = "release/v2"
	dbfx.Exec(t, `
		INSERT INTO project_resource (
			project_id, workspace_id, resource_type, resource_ref, position
		) VALUES ($1, $2, 'github_repo', $3::jsonb, 0)
	`, projectID, testWorkspaceID, `{"url":"`+projectRepoURL+`","ref":"`+projectRepoRef+`"}`)

	// Agent + runtime + queued task in this project.
	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "project repo override", testutil.Cols{
		"project_id": projectID,
		"priority":   "medium",
		"number":     88001,
	})

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "test-claim-project-repos")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			Repos            []RepoData            `json:"repos"`
			ProjectID        string                `json:"project_id"`
			ProjectResources []ProjectResourceData `json:"project_resources"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	if resp.Task.ProjectID != projectID {
		t.Errorf("project_id = %q, want %q", resp.Task.ProjectID, projectID)
	}
	if len(resp.Task.Repos) != 1 || resp.Task.Repos[0].URL != projectRepoURL {
		t.Fatalf("expected resp.Repos to contain only the project repo URL, got %+v", resp.Task.Repos)
	}
	if resp.Task.Repos[0].Ref != projectRepoRef {
		t.Fatalf("project repo ref = %q, want %q", resp.Task.Repos[0].Ref, projectRepoRef)
	}
	for _, r := range resp.Task.Repos {
		if strings.HasSuffix(r.URL, "workspace-repo-a") || strings.HasSuffix(r.URL, "workspace-repo-b") {
			t.Errorf("workspace repo %q leaked into resp.Repos despite project override", r.URL)
		}
	}
	if len(resp.Task.ProjectResources) != 1 {
		t.Errorf("expected 1 project_resources entry, got %d", len(resp.Task.ProjectResources))
	}
}

// When an issue belongs to a project that has a description, the claim handler
// must surface that description as project_description so the daemon can inject
// it into the brief. This is the handler-side boundary the execenv tests can't
// cover: if this assignment regresses, the description silently stops reaching
// the agent while the execenv rendering tests still pass.
func TestClaimTask_ProjectDescriptionInjected(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	const projectDescription = "Always write copy in British English. Ship behind a feature flag."
	projectID := dbfx.Project(t, "Claim project description", testutil.Cols{
		"description": projectDescription,
	})

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "project description", testutil.Cols{
		"project_id": projectID,
		"priority":   "medium",
		"number":     88002,
	})

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "test-claim-project-desc")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			ProjectID          string `json:"project_id"`
			ProjectDescription string `json:"project_description"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	if resp.Task.ProjectID != projectID {
		t.Errorf("project_id = %q, want %q", resp.Task.ProjectID, projectID)
	}
	if resp.Task.ProjectDescription != projectDescription {
		t.Errorf("project_description = %q, want %q", resp.Task.ProjectDescription, projectDescription)
	}
}

// The quick-create path resolves its project from the task context JSONB
// (not an issue row), so its project_description wiring is a separate branch
// in the claim handler and needs its own boundary assertion.
func TestClaimTask_QuickCreateInjectsProjectDescription(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	const projectDescription = "Use the design system tokens; never hardcode colors."
	projectID := dbfx.Project(t, "Quick-create project description", testutil.Cols{
		"description": projectDescription,
	})

	quickContext, _ := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       "create a follow-up issue",
		"requester_id": testUserID,
		"workspace_id": testWorkspaceID,
		"project_id":   projectID,
	})
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 2, $3)
	`, agentID, runtimeID, quickContext)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.ProjectID != projectID {
		t.Errorf("quick-create project_id = %q, want %q", task.ProjectID, projectID)
	}
	if task.ProjectDescription != projectDescription {
		t.Errorf("quick-create project_description = %q, want %q", task.ProjectDescription, projectDescription)
	}
}

// When the issue's project has no github_repo resources, the claim handler
// must fall back to workspace repos (the pre-override behavior).
func TestClaimTask_ProjectWithoutRepos_FallsBackToWorkspaceRepos(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	setHandlerTestWorkspaceRepos(t, []map[string]string{
		{"url": "https://github.com/example/workspace-fallback", "description": "ws"},
	})

	projectID := dbfx.Project(t, "Claim project without repos")

	var agentID, runtimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "no project repos", testutil.Cols{
		"project_id": projectID,
		"priority":   "medium",
		"number":     88002,
	})

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
	})

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil, testWorkspaceID, "test-claim-fallback")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)

	var resp struct {
		Task *struct {
			Repos []RepoData `json:"repos"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatal("expected task in response")
	}
	if len(resp.Task.Repos) != 1 || !strings.HasSuffix(resp.Task.Repos[0].URL, "workspace-fallback") {
		t.Fatalf("expected workspace fallback repo, got %+v", resp.Task.Repos)
	}
}

// Regression test for #1276: ClaimTaskByRuntime must populate workspace_id in
// the response for run_only autopilot tasks. Before the fix, resp.WorkspaceID
// stayed empty because ClaimTaskByRuntime only handled IssueID and
// ChatSessionID branches, causing the daemon's execenv to fail with
// "workspace ID is required".
func TestClaimTask_AutopilotRunOnly_PopulatesWorkspaceID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "claim workspace fixture",
		"assignee_id":     agentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)

	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "manual",
		"status":       "running",
	})

	// Create a queued task with only AutopilotRunID (no IssueID, no ChatSessionID).
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":       runtimeID,
		"issue_id":         nil,
		"autopilot_run_id": runID,
	})
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, "test-daemon-claim")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *struct {
			WorkspaceID string `json:"workspace_id"`
			ThreadName  string `json:"thread_name"`
		} `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a task in response, got nil")
	}
	if resp.Task.WorkspaceID == "" {
		t.Fatal("ClaimTaskByRuntime for run_only autopilot: workspace_id is empty in response")
	}
	if resp.Task.WorkspaceID != testWorkspaceID {
		t.Fatalf("expected workspace_id %q, got %q", testWorkspaceID, resp.Task.WorkspaceID)
	}
	if resp.Task.ThreadName != "claim workspace fixture" {
		t.Fatalf("autopilot task thread_name = %q, want autopilot title", resp.Task.ThreadName)
	}
}

// TestClaimTaskByRuntime_TaskWorkspaceMismatch_CancelsAndRejects verifies
// the defense-in-depth check in ClaimTaskByRuntime: if a task is somehow
// dispatched to a runtime whose workspace doesn't match the task's
// resolved workspace (upstream routing / data-integrity bug), the handler
// must 500 AND cancel the dispatched task so it doesn't sit in
// 'dispatched' until the 5-minute sweeper — which would also leave the
// agent stuck reporting 'working' in the UI.
func TestClaimTaskByRuntime_TaskWorkspaceMismatch_CancelsAndRejects(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	// Local agent/runtime (belongs to testWorkspace).
	var localAgentID, localRuntimeID string
	dbfx.QueryRow(t,
		`SELECT id, runtime_id FROM agent WHERE workspace_id = $1 LIMIT 1`,
		testWorkspaceID,
	).Scan(&localAgentID, &localRuntimeID)

	// Foreign workspace with its own issue — what the misrouted task will
	// resolve to.
	foreignWorkspaceID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Mismatch Foreign",
		"slug":         "mismatch-foreign-claim",
		"description":  "",
		"issue_prefix": "MFC",
	})

	foreignIssueID := dbfx.Issue(t, "mismatch-foreign-issue", testutil.Cols{
		"workspace_id": foreignWorkspaceID,
		"priority":     "medium",
		"number":       77001,
	})

	// Construct the inconsistent task: runtime_id belongs to testWorkspace,
	// but issue_id is in foreignWorkspace. This is the data shape a routing
	// bug would produce.
	taskID := dbfx.Task(t, localAgentID, testutil.Cols{
		"runtime_id": localRuntimeID,
		"issue_id":   foreignIssueID,
		"priority":   2,
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+localRuntimeID+"/claim", nil,
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", localRuntimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("ClaimTaskByRuntime (mismatch): expected 500, got %d: %s", w.Code, w.Body.String())
	}

	// Task must NOT remain dispatched — it has to be cancelled so the agent
	// is released immediately rather than stuck until the sweeper fires.
	var status string
	dbfx.QueryRow(t,
		`SELECT status FROM agent_task_queue WHERE id = $1`, taskID,
	).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("ClaimTaskByRuntime (mismatch): expected task status=cancelled, got %q", status)
	}
}

// Regression test for MUL-1198: comment-triggered tasks that finish without
// the agent posting any comment must still deliver a synthesized result
// comment, threaded under the trigger. Before the fix, CompleteTask exempted
// comment-triggered tasks from the auto-synthesis path, so a Claude Code /
// Codex / etc. agent that ended its run with only terminal text (no
// `multica issue comment add` call) left the user staring at a "Completed"
// badge with no reply.
func TestCompleteTask_CommentTriggered_SynthesizesCommentWhenAgentSilent(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	setWorkspaceIssuePrefixForTest(t, "MUL")

	issueID := dbfx.Issue(t, "mul-3310 agent output fixture", testutil.Cols{
		"status": "in_progress",
		"number": 3310,
	})

	triggerCommentID := dbfx.Comment(t, issueID, "please take a look")

	// Comment-triggered, already running (as CompleteAgentTask requires).
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": triggerCommentID,
		"status":             "running",
		"started_at":         testutil.Raw("now()"),
	})

	agentFinalOutput := fmt.Sprintf(
		"sure, see MUL-3310, issue/MUL-3310, feature/MUL-3310, and [MUL-3310](mention://issue/%s)",
		issueID,
	)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": agentFinalOutput},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// Exactly one agent comment on the issue, threaded under the trigger,
	// carrying the agent's final output.
	rows, err := testPool.Query(ctx, `
		SELECT content, parent_id FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
		ORDER BY created_at ASC
	`, issueID, agentID)
	if err != nil {
		t.Fatalf("query synthesized comments: %v", err)
	}
	defer rows.Close()

	var (
		content  string
		parentID *string
		seen     int
	)
	for rows.Next() {
		if err := rows.Scan(&content, &parentID); err != nil {
			t.Fatalf("scan comment: %v", err)
		}
		seen++
	}
	if seen != 1 {
		t.Fatalf("expected exactly 1 synthesized agent comment, got %d", seen)
	}
	if content != agentFinalOutput {
		t.Fatalf("synthesized comment content = %q, want %q", content, agentFinalOutput)
	}
	if parentID == nil || *parentID != triggerCommentID {
		got := "<nil>"
		if parentID != nil {
			got = *parentID
		}
		t.Fatalf("synthesized comment parent_id = %s, want trigger comment %s", got, triggerCommentID)
	}
}

// Companion to the above: when the agent DID post its own comment during the
// run, CompleteTask must not synthesize a duplicate. Guards against the
// common case where the fix is over-eager and creates two comments per task.
func TestCompleteTask_CommentTriggered_SkipsSynthesisWhenAgentAlreadyCommented(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "mul-1198 dedup fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81199,
	})

	triggerCommentID := dbfx.Comment(t, issueID, "please take a look")

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": triggerCommentID,
		"status":             "running",
		"started_at":         testutil.Raw("now()"),
	})

	// Agent posts its own reply during the run — exactly the compliant path.
	dbfx.Exec(t, `
		INSERT INTO comment (issue_id, workspace_id, author_type, author_id, content, type, parent_id)
		VALUES ($1, $2, 'agent', $3, 'done, see PR', 'comment', $4)
	`, issueID, testWorkspaceID, agentID, triggerCommentID)

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "final terminal text that must NOT become a comment"},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
	`, issueID, agentID).Scan(&count)
	if count != 1 {
		t.Fatalf("expected 1 agent comment (the agent's own reply), got %d — synthesis duplicated", count)
	}
}

func TestCompleteTask_CommentTriggered_SuppressesTrivialDoneOutput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "trivial-done-suppression fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81200,
	})

	triggerCommentID := dbfx.Comment(t, issueID, "please follow up")

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": triggerCommentID,
		"status":             "running",
		"started_at":         testutil.Raw("now()"),
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "Done."},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var count int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
	`, issueID, agentID).Scan(&count)
	if count != 0 {
		t.Fatalf("expected no synthesized agent comment for trivial Done output, got %d", count)
	}
}

func TestCompleteTask_AssignmentTriggered_DoesNotSuppressTrivialDoneOutput(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	issueID := dbfx.Issue(t, "assignment-trivial-done fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81201,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id": runtimeID,
		"issue_id":   issueID,
		"status":     "running",
		"started_at": testutil.Raw("now()"),
	})

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/complete",
		map[string]any{"output": "Done."},
		testWorkspaceID, "legit-daemon")
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("taskId", taskID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.CompleteTask(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("CompleteTask: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var content string
	dbfx.QueryRow(t, `
		SELECT content FROM comment
		WHERE issue_id = $1 AND author_type = 'agent' AND author_id = $2
		ORDER BY created_at DESC LIMIT 1
	`, issueID, agentID).Scan(&content)
	if content != "Done." {
		t.Fatalf("synthesized comment content = %q, want Done.", content)
	}
}

type claimRuntimeGuardTask struct {
	PriorSessionID           string   `json:"prior_session_id"`
	PriorWorkDir             string   `json:"prior_work_dir"`
	ChatMessage              string   `json:"chat_message"`
	ThreadName               string   `json:"thread_name"`
	QuickCreateAttachmentIDs []string `json:"quick_create_attachment_ids"`
	QuickCreatePriority      string   `json:"quick_create_priority"`
	QuickCreateDueDate       string   `json:"quick_create_due_date"`
	ProjectID                string   `json:"project_id"`
	ProjectDescription       string   `json:"project_description"`
}

func claimTaskForRuntimeGuard(t *testing.T, runtimeID, daemonID string) *claimRuntimeGuardTask {
	t.Helper()

	w := httptest.NewRecorder()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/claim", nil,
		testWorkspaceID, daemonID)
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("runtimeId", runtimeID)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	testHandler.ClaimTaskByRuntime(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("ClaimTaskByRuntime: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Task *claimRuntimeGuardTask `json:"task"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if resp.Task == nil {
		t.Fatal("expected a task in response, got nil")
	}
	return resp.Task
}

func createRuntimeGuardAgent(t *testing.T, ctx context.Context) (agentID, runtimeID, daemonID string) {
	t.Helper()

	daemonID = "runtime-guard-" + strings.ToLower(strings.ReplaceAll(t.Name(), "/", "-"))
	req := newDaemonTokenRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID,
		"daemon_id":    daemonID,
		"device_name":  "runtime-guard-test",
		"runtimes": []map[string]any{
			{"name": "runtime-guard-current", "type": "opencode", "version": "test", "status": "online"},
		},
	}, testWorkspaceID, daemonID)
	w := testutil.Call(t, testHandler.DaemonRegister, req).Want(http.StatusOK)
	var resp map[string]any
	w.JSON(&resp)
	runtimes, ok := resp["runtimes"].([]any)
	if !ok || len(runtimes) == 0 {
		t.Fatalf("setup: expected registered runtime, got %v", resp)
	}
	runtimeID = runtimes[0].(map[string]any)["id"].(string)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent_runtime WHERE id = $1`, runtimeID) })
	dbfx.Exec(t, `UPDATE agent_runtime SET owner_id = $1 WHERE id = $2`, testUserID, runtimeID)

	dbfx.QueryRow(t, `
		INSERT INTO agent (
			workspace_id, name, runtime_mode, runtime_config,
			runtime_id, visibility, max_concurrent_tasks
		)
		VALUES ($1, $2, 'local', '{}'::jsonb, $3, 'workspace', 3)
		RETURNING id
	`, testWorkspaceID, "Runtime Guard Agent "+t.Name(), runtimeID).Scan(&agentID)
	t.Cleanup(func() { testPool.Exec(ctx, `DELETE FROM agent WHERE id = $1`, agentID) })

	return agentID, runtimeID, daemonID
}

func createRuntimeGuardRuntime(t *testing.T, ctx context.Context, provider string) string {
	t.Helper()

	runtimeID := dbfx.Runtime(t, "Runtime Guard Fixture", testutil.Cols{
		"daemon_id":    testutil.Raw("'runtime-guard-' || gen_random_uuid()::text"),
		"runtime_mode": "local",
		"provider":     provider,
		"status":       "offline",
		"device_info":  testutil.Raw("'{}'::jsonb"),
	})
	return runtimeID
}

func TestChatSessionRuntimeBackfillRequiresMatchingSessionID(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE chat_session (
			id uuid PRIMARY KEY,
			session_id text,
			runtime_id uuid
		) ON COMMIT DROP;
	`); err != nil {
		t.Fatalf("setup temp chat_session table: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		CREATE TEMP TABLE agent_task_queue (
			chat_session_id uuid,
			runtime_id uuid,
			session_id text,
			status text,
			completed_at timestamptz,
			started_at timestamptz,
			dispatched_at timestamptz,
			created_at timestamptz
		) ON COMMIT DROP;
	`); err != nil {
		t.Fatalf("setup temp agent_task_queue table: %v", err)
	}

	const (
		poisonedChatID = "00000000-0000-0000-0000-000000000101"
		matchedChatID  = "00000000-0000-0000-0000-000000000102"
		oldRuntimeID   = "00000000-0000-0000-0000-000000000201"
		newRuntimeID   = "00000000-0000-0000-0000-000000000202"
	)

	if _, err := tx.Exec(ctx, `
		INSERT INTO chat_session (id, session_id, runtime_id)
		VALUES
			($1, 'old-runtime-session', NULL),
			($2, 'matched-runtime-session', NULL);
	`, poisonedChatID, matchedChatID); err != nil {
		t.Fatalf("seed temp chat sessions: %v", err)
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO agent_task_queue (
			chat_session_id, runtime_id, session_id, status,
			completed_at, started_at, dispatched_at, created_at
		)
		VALUES
			($1, $3, 'old-runtime-session', 'completed',
			 now() - interval '2 hours', now() - interval '2 hours', now() - interval '2 hours', now() - interval '2 hours'),
			($1, $4, 'new-runtime-session', 'completed',
			 now() - interval '1 hour', now() - interval '1 hour', now() - interval '1 hour', now() - interval '1 hour'),
			($2, $4, 'matched-runtime-session', 'completed',
			 now() - interval '30 minutes', now() - interval '30 minutes', now() - interval '30 minutes', now() - interval '30 minutes');
	`, poisonedChatID, matchedChatID, oldRuntimeID, newRuntimeID); err != nil {
		t.Fatalf("seed temp task sessions: %v", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE chat_session cs
		SET runtime_id = latest.runtime_id
		FROM (
			SELECT DISTINCT ON (chat_session_id)
				chat_session_id,
				runtime_id,
				session_id
			FROM agent_task_queue
			WHERE chat_session_id IS NOT NULL
			  AND session_id IS NOT NULL
			  AND status IN ('completed', 'failed')
			ORDER BY chat_session_id, COALESCE(completed_at, started_at, dispatched_at, created_at) DESC
		) latest
		WHERE latest.chat_session_id = cs.id
		  AND latest.session_id = cs.session_id
	`); err != nil {
		t.Fatalf("run runtime backfill: %v", err)
	}

	var poisonedRuntimeID *string
	if err := tx.QueryRow(ctx, `
		SELECT runtime_id::text FROM chat_session WHERE id = $1
	`, poisonedChatID).Scan(&poisonedRuntimeID); err != nil {
		t.Fatalf("query poisoned chat runtime: %v", err)
	}
	if poisonedRuntimeID != nil {
		t.Fatalf("expected stale session mismatch to remain NULL, got %s", *poisonedRuntimeID)
	}

	var matchedRuntimeID string
	if err := tx.QueryRow(ctx, `
		SELECT runtime_id::text FROM chat_session WHERE id = $1
	`, matchedChatID).Scan(&matchedRuntimeID); err != nil {
		t.Fatalf("query matched chat runtime: %v", err)
	}
	if matchedRuntimeID != newRuntimeID {
		t.Fatalf("expected matched session to backfill runtime %s, got %s", newRuntimeID, matchedRuntimeID)
	}
}

func TestClaimTask_IssuePriorSessionRuntimeGuard(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	oldRuntimeID := createRuntimeGuardRuntime(t, ctx, "kimi")

	skipIssueID := dbfx.Issue(t, "runtime-session-skip fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81203,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'old-runtime-session', '/tmp/old-runtime-workdir')
	`, agentID, oldRuntimeID, skipIssueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, skipIssueID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "" {
		t.Fatalf("runtime mismatch: expected empty PriorSessionID, got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/old-runtime-workdir" {
		t.Fatalf("runtime mismatch: expected PriorWorkDir='/tmp/old-runtime-workdir', got %q", task.PriorWorkDir)
	}
	if task.ThreadName != "runtime-session-skip fixture" {
		t.Fatalf("issue task thread_name = %q, want issue title", task.ThreadName)
	}
	dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now()
		WHERE issue_id = $1 AND status IN ('dispatched', 'running')
	`, skipIssueID)

	resumeIssueID := dbfx.Issue(t, "runtime-session-resume fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81204,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'same-runtime-session', '/tmp/same-runtime-workdir')
	`, agentID, runtimeID, resumeIssueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, resumeIssueID)

	task = claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "same-runtime-session" {
		t.Fatalf("runtime match: expected PriorSessionID='same-runtime-session', got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/same-runtime-workdir" {
		t.Fatalf("runtime match: expected PriorWorkDir='/tmp/same-runtime-workdir', got %q", task.PriorWorkDir)
	}

	commentIssueID := dbfx.Issue(t, "comment-triggered-session-skip fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81205,
	})

	triggerCommentID := dbfx.Comment(t, commentIssueID, "please follow up")
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'comment-prior-session', '/tmp/comment-prior-workdir')
	`, agentID, runtimeID, commentIssueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id, trigger_comment_id,
			status, priority
		)
		VALUES ($1, $2, $3, $4, 'queued', 0)
	`, agentID, runtimeID, commentIssueID, triggerCommentID)

	task = claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	// Comment-triggered tasks now resume the prior session by default (same
	// runtime), so the agent keeps the issue's conversation context across turns.
	if task.PriorSessionID != "comment-prior-session" {
		t.Fatalf("comment trigger: expected PriorSessionID='comment-prior-session' (resume default-on), got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/comment-prior-workdir" {
		t.Fatalf("comment trigger: expected PriorWorkDir='/tmp/comment-prior-workdir', got %q", task.PriorWorkDir)
	}
	dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now()
		WHERE issue_id = $1 AND status IN ('dispatched', 'running')
	`, commentIssueID)

	freshIssueID := dbfx.Issue(t, "force-fresh-session fixture", testutil.Cols{
		"status": "in_progress",
		"number": 81206,
	})
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'force-fresh-prior-session', '/tmp/force-fresh-prior-workdir')
	`, agentID, runtimeID, freshIssueID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, issue_id,
			status, priority, force_fresh_session
		)
		VALUES ($1, $2, $3, 'queued', 0, TRUE)
	`, agentID, runtimeID, freshIssueID)

	task = claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "" {
		t.Fatalf("force fresh: expected empty PriorSessionID, got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "" {
		t.Fatalf("force fresh: expected empty PriorWorkDir, got %q", task.PriorWorkDir)
	}
}

// TestClaimTask_ManualRetryReusesWorkdir is the MUL-4869 claim-layer contract: a
// manual retry (rerun_of_task_id set) ALWAYS hands back the source task's
// workdir, and resumes the session only when the source failure did not poison
// the conversation AND the source ran on the claiming runtime. The rerun row's
// force_fresh_session is always true (rollback-safe); the session decision is
// computed here from the source task, including the legacy 400 error-text guard.
// Contrast with a plain force_fresh task carrying no rerun lineage, which resumes
// nothing — covered by TestClaimTask_IssuePriorSessionRuntimeGuard.
func TestClaimTask_ManualRetryReusesWorkdir(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	otherRuntimeID := createRuntimeGuardRuntime(t, ctx, "kimi")

	// insertRerun persists a terminal source task plus a queued rerun that points
	// at it (rerun_of_task_id) with force_fresh_session=true, mimicking what
	// RerunIssue writes, then claims and returns the resolved task. Each case uses
	// a fresh issue so the partial unique index (one pending task per issue+agent)
	// never trips across cases. The source's runtime, failure_reason, and error
	// text drive the runtime-match and resume-safety gates.
	issueNum := 81207
	insertRerun := func(t *testing.T, sourceRuntimeID, failureReason, errorText, session, workdir string) *claimRuntimeGuardTask {
		t.Helper()
		issueNum++
		issueID := dbfx.Issue(t, "manual-retry-reuse fixture", testutil.Cols{
			"status": "in_progress",
			"number": issueNum,
		})
		sourceID := dbfx.Task(t, agentID, testutil.Cols{
			"runtime_id":     sourceRuntimeID,
			"issue_id":       issueID,
			"status":         "failed",
			"failure_reason": failureReason,
			"error":          errorText,
			"session_id":     session,
			"work_dir":       workdir,
		})
		// force_fresh_session is always true on a rerun row (rollback-safe); the
		// new claim handler resumes from the source task regardless.
		dbfx.Exec(t, `
			INSERT INTO agent_task_queue (
				agent_id, runtime_id, issue_id, status, priority,
				rerun_of_task_id, force_fresh_session
			)
			VALUES ($1, $2, $3, 'queued', 0, $4, TRUE)
		`, agentID, runtimeID, issueID, sourceID)
		return claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	}

	t.Run("resume_safe_same_runtime_reuses_both", func(t *testing.T) {
		task := insertRerun(t, runtimeID, "timeout", "", "safe-session", "/tmp/retry-safe-workdir")
		if task.PriorWorkDir != "/tmp/retry-safe-workdir" {
			t.Fatalf("PriorWorkDir = %q, want /tmp/retry-safe-workdir", task.PriorWorkDir)
		}
		if task.PriorSessionID != "safe-session" {
			t.Fatalf("PriorSessionID = %q, want safe-session (transient failure resumes)", task.PriorSessionID)
		}
	})

	t.Run("poisoned_reason_same_runtime_reuses_workdir_fresh_session", func(t *testing.T) {
		task := insertRerun(t, runtimeID, "agent_error.context_overflow", "", "poison-session", "/tmp/retry-poison-workdir")
		if task.PriorWorkDir != "/tmp/retry-poison-workdir" {
			t.Fatalf("PriorWorkDir = %q, want /tmp/retry-poison-workdir (workdir reused even when session poisoned)", task.PriorWorkDir)
		}
		if task.PriorSessionID != "" {
			t.Fatalf("PriorSessionID = %q, want empty (poisoned session starts fresh)", task.PriorSessionID)
		}
	})

	t.Run("legacy_400_error_text_reuses_workdir_fresh_session", func(t *testing.T) {
		// failure_reason is a benign generic 'agent_error', but the raw error
		// carries the Anthropic 400 invalid_request_error marker — the exact
		// source path must still refuse to resume that session (defense-in-depth
		// mirrored from GetLastTaskSession).
		task := insertRerun(t, runtimeID, "agent_error", "API error 400 invalid_request_error: prompt is too long", "legacy400-session", "/tmp/retry-legacy400-workdir")
		if task.PriorWorkDir != "/tmp/retry-legacy400-workdir" {
			t.Fatalf("PriorWorkDir = %q, want /tmp/retry-legacy400-workdir", task.PriorWorkDir)
		}
		if task.PriorSessionID != "" {
			t.Fatalf("PriorSessionID = %q, want empty (legacy 400 invalid_request_error must not resume)", task.PriorSessionID)
		}
	})

	t.Run("different_runtime_reuses_workdir_drops_session", func(t *testing.T) {
		task := insertRerun(t, otherRuntimeID, "timeout", "", "cross-session", "/tmp/retry-cross-workdir")
		if task.PriorWorkDir != "/tmp/retry-cross-workdir" {
			t.Fatalf("PriorWorkDir = %q, want /tmp/retry-cross-workdir (workdir offered regardless of runtime, best-effort)", task.PriorWorkDir)
		}
		if task.PriorSessionID != "" {
			t.Fatalf("PriorSessionID = %q, want empty (cross-runtime session cannot resolve)", task.PriorSessionID)
		}
	})
}

func TestClaimTask_ChatPriorSessionRuntimeGuard(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)
	oldRuntimeID := createRuntimeGuardRuntime(t, ctx, "kimi")

	skipSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":      "runtime guard skip chat",
		"session_id": "old-chat-session",
		"work_dir":   "/tmp/old-chat-workdir",
		"runtime_id": oldRuntimeID,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'old-chat-session', '/tmp/old-chat-workdir')
	`, agentID, oldRuntimeID, skipSessionID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, skipSessionID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "" {
		t.Fatalf("chat runtime mismatch: expected empty PriorSessionID, got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/old-chat-workdir" {
		t.Fatalf("chat runtime mismatch: expected PriorWorkDir='/tmp/old-chat-workdir', got %q", task.PriorWorkDir)
	}
	dbfx.Exec(t, `
		UPDATE agent_task_queue
		SET status = 'completed', completed_at = now()
		WHERE chat_session_id = $1 AND status IN ('dispatched', 'running')
	`, skipSessionID)

	resumeSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":      "runtime guard resume chat",
		"session_id": "same-chat-session",
		"work_dir":   "/tmp/same-chat-workdir",
		"runtime_id": runtimeID,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, resumeSessionID)

	task = claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "same-chat-session" {
		t.Fatalf("chat runtime match: expected PriorSessionID='same-chat-session', got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/same-chat-workdir" {
		t.Fatalf("chat runtime match: expected PriorWorkDir='/tmp/same-chat-workdir', got %q", task.PriorWorkDir)
	}
}

// TestClaimTask_ChatDeliversAllUnansweredUserMessages pins the fix for the
// regression the MUL-2968 debounce exposed: when several user messages are
// debounced into a single run, the agent must receive ALL of them, not just
// the most recent. Before the fix the daemon prompt was the single latest
// user message, so "看上海天气" then "还有青岛" answered only Qingdao.
func TestClaimTask_ChatDeliversAllUnansweredUserMessages(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	sessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title": "debounce delivery chat",
	})

	// Two user messages debounced into one run (explicit created_at so the
	// ASC ordering is deterministic).
	dbfx.Exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content, created_at) VALUES
			($1, 'user', '看上海天气', now()),
			($1, 'user', '还有青岛',   now() + interval '1 second')
	`, sessionID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 2)
	`, agentID, runtimeID, sessionID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.ChatMessage != "看上海天气\n\n还有青岛" {
		t.Fatalf("chat prompt must include every unanswered user message in order; got %q", task.ChatMessage)
	}
	if task.ThreadName != "debounce delivery chat" {
		t.Fatalf("chat task thread_name = %q, want chat session title", task.ThreadName)
	}

	// Complete the run and record the agent's assistant reply, then send a
	// fresh user message — only the new one should be delivered next.
	dbfx.Exec(t, `
		UPDATE agent_task_queue SET status = 'completed', completed_at = now()
		WHERE chat_session_id = $1 AND status IN ('dispatched', 'running')
	`, sessionID)
	dbfx.Exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content, created_at)
		VALUES ($1, 'assistant', '上海与青岛天气如下…', now() + interval '2 second')
	`, sessionID)
	dbfx.Exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content, created_at)
		VALUES ($1, 'user', '深圳呢', now() + interval '3 second')
	`, sessionID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority)
		VALUES ($1, $2, $3, 'queued', 2)
	`, agentID, runtimeID, sessionID)

	task = claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.ChatMessage != "深圳呢" {
		t.Fatalf("after a reply, only the new user message must be delivered; got %q", task.ChatMessage)
	}
}

// TestClaimTask_ChatPopulatesInitiator verifies MUL-2645 for chat tasks: the
// claim response surfaces the STORED task initiator (initiator_user_id captured
// at enqueue), NOT chat_session.creator_id. This is the MUL-2645 review fix: for
// Lark group chats the session creator is the installer, not the sender, so the
// claim must read the stored sender. The test pins this by making the creator a
// DIFFERENT user (the "installer") from the stored initiator (the sender) and
// asserting the claim resolves the sender.
func TestClaimTask_ChatPopulatesInitiator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	// A separate user stands in for the Lark group session creator (installer).
	installerID := dbfx.Insert(t, "user", testutil.Cols{
		"name":  "Installer User",
		"email": "installer-test@multica.ai",
	})

	sessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"creator_id": installerID,
		"title":      "initiator chat",
	})

	dbfx.Exec(t, `
		INSERT INTO chat_message (chat_session_id, role, content) VALUES ($1, 'user', 'hi there')
	`, sessionID)
	// initiator_user_id = the real sender (testUserID), distinct from creator.
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, chat_session_id, status, priority, initiator_user_id)
		VALUES ($1, $2, $3, 'queued', 2, $4)
	`, agentID, runtimeID, sessionID, testUserID)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	var resp struct {
		Task *struct {
			InitiatorType  string `json:"initiator_type"`
			InitiatorID    string `json:"initiator_id"`
			InitiatorName  string `json:"initiator_name"`
			InitiatorEmail string `json:"initiator_email"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimed task, got %s", w.Body.String())
	}
	if resp.Task.InitiatorType != "member" || resp.Task.InitiatorID != testUserID ||
		resp.Task.InitiatorName != handlerTestName || resp.Task.InitiatorEmail != handlerTestEmail {
		t.Errorf("chat initiator = {type:%q id:%q name:%q email:%q}, want {member %q %q %q}",
			resp.Task.InitiatorType, resp.Task.InitiatorID, resp.Task.InitiatorName, resp.Task.InitiatorEmail,
			testUserID, handlerTestName, handlerTestEmail)
	}
}

func TestClaimTask_QuickCreatePopulatesThreadName(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()
	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	quickPrompt := "create a follow-up issue for Codex session titles"
	attachmentID := "019ec09d-6222-722b-bdfa-427b105d80be"
	quickContext, _ := json.Marshal(map[string]any{
		"type":           "quick_create",
		"prompt":         quickPrompt,
		"requester_id":   testUserID,
		"workspace_id":   testWorkspaceID,
		"priority":       "high",
		"due_date":       "2026-08-01",
		"attachment_ids": []string{attachmentID},
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (agent_id, runtime_id, status, priority, context)
		VALUES ($1, $2, 'queued', 2, $3)
	`, agentID, runtimeID, quickContext)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.ThreadName != quickPrompt {
		t.Fatalf("quick-create task thread_name = %q, want prompt", task.ThreadName)
	}
	if len(task.QuickCreateAttachmentIDs) != 1 || task.QuickCreateAttachmentIDs[0] != attachmentID {
		t.Fatalf("quick-create attachment ids = %#v, want [%q]", task.QuickCreateAttachmentIDs, attachmentID)
	}
	if task.QuickCreatePriority != "high" || task.QuickCreateDueDate != "2026-08-01" {
		t.Fatalf("quick-create fields = {%q, %q}, want {high, 2026-08-01}", task.QuickCreatePriority, task.QuickCreateDueDate)
	}
}

func TestClaimTask_ChatForceFreshSessionSkipsPriorSession(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	chatSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":      "force fresh chat",
		"session_id": "chat-pointer-session",
		"work_dir":   "/tmp/chat-pointer-workdir",
		"runtime_id": runtimeID,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'task-row-session', '/tmp/task-row-workdir')
	`, agentID, runtimeID, chatSessionID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority, force_fresh_session
		)
		VALUES ($1, $2, $3, 'queued', 0, TRUE)
	`, agentID, runtimeID, chatSessionID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "" {
		t.Fatalf("force fresh chat: expected empty PriorSessionID, got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "" {
		t.Fatalf("force fresh chat: expected empty PriorWorkDir, got %q", task.PriorWorkDir)
	}
}

// Locks the legacy-row fallback: chat_session.runtime_id IS NULL (e.g. a row
// the migration left untouched because no prior task matched the cs pointer)
// but a completed task on the claiming runtime exists. ClaimTaskByRuntime
// must recover the session from the task row, not start a fresh conversation.
func TestClaimTask_ChatLegacyNullRuntimeFallsBackToTaskRow(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	agentID, runtimeID, daemonID := createRuntimeGuardAgent(t, ctx)

	legacySessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title":      "runtime guard legacy chat",
		"session_id": nil,
		"work_dir":   nil,
		"runtime_id": nil,
	})

	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority, started_at, completed_at,
			session_id, work_dir
		)
		VALUES ($1, $2, $3, 'completed', 0, now(), now(), 'legacy-fallback-session', '/tmp/legacy-fallback-workdir')
	`, agentID, runtimeID, legacySessionID)
	dbfx.Exec(t, `
		INSERT INTO agent_task_queue (
			agent_id, runtime_id, chat_session_id,
			status, priority
		)
		VALUES ($1, $2, $3, 'queued', 0)
	`, agentID, runtimeID, legacySessionID)

	task := claimTaskForRuntimeGuard(t, runtimeID, daemonID)
	if task.PriorSessionID != "legacy-fallback-session" {
		t.Fatalf("legacy fallback: expected PriorSessionID='legacy-fallback-session', got %q", task.PriorSessionID)
	}
	if task.PriorWorkDir != "/tmp/legacy-fallback-workdir" {
		t.Fatalf("legacy fallback: expected PriorWorkDir='/tmp/legacy-fallback-workdir', got %q", task.PriorWorkDir)
	}
}

// TestGetChatSessionGCCheck verifies the chat session gc-check endpoint
// matches the same anti-enumeration shape as GetIssueGCCheck: cross-workspace
// daemon tokens get 404, same-workspace tokens get the live status.
func TestGetChatSessionGCCheck(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)

	sessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title": "gc-check fixture",
	})
	defer testPool.Exec(ctx, `DELETE FROM chat_session WHERE id = $1`, sessionID)

	// Cross-workspace daemon token must 404 with no oracle.
	req := newDaemonTokenRequest("GET", "/api/daemon/chat-sessions/"+sessionID+"/gc-check", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	req = withURLParam(req, "sessionId", sessionID)
	w := testutil.Call(t, testHandler.GetChatSessionGCCheck, req).Want(http.StatusNotFound)

	// Same-workspace daemon token sees the live row.
	req = newDaemonTokenRequest("GET", "/api/daemon/chat-sessions/"+sessionID+"/gc-check", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "sessionId", sessionID)
	w = testutil.Call(t, testHandler.GetChatSessionGCCheck, req).Want(http.StatusOK)
	var resp struct {
		Status    string `json:"status"`
		UpdatedAt string `json:"updated_at"`
	}
	w.JSON(&resp)
	if resp.Status != "active" {
		t.Fatalf("expected status %q, got %q", "active", resp.Status)
	}
	if resp.UpdatedAt == "" {
		t.Fatal("expected updated_at to be set")
	}

	// Hard-deleted session: 404 — exactly what the daemon needs to reclaim
	// the workdir on the next GC pass after a user runs DeleteChatSession.
	dbfx.Exec(t, `DELETE FROM chat_session WHERE id = $1`, sessionID)
	req = newDaemonTokenRequest("GET", "/api/daemon/chat-sessions/"+sessionID+"/gc-check", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "sessionId", sessionID)
	w = testutil.Call(t, testHandler.GetChatSessionGCCheck, req).Want(http.StatusNotFound)
}

// TestGetAutopilotRunGCCheck verifies the autopilot-run gc-check endpoint:
// 200 with status+completed_at on success, 404 on cross-workspace probe.
func TestGetAutopilotRunGCCheck(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID string
	dbfx.QueryRow(t, `SELECT id FROM agent WHERE workspace_id = $1 LIMIT 1`, testWorkspaceID).Scan(&agentID)

	autopilotID := dbfx.Insert(t, "autopilot", testutil.Cols{
		"workspace_id":    testWorkspaceID,
		"title":           "gc-check autopilot",
		"assignee_id":     agentID,
		"execution_mode":  "run_only",
		"created_by_type": "member",
		"created_by_id":   testUserID,
	})
	defer testPool.Exec(ctx, `DELETE FROM autopilot WHERE id = $1`, autopilotID)

	runID := dbfx.Insert(t, "autopilot_run", testutil.Cols{
		"autopilot_id": autopilotID,
		"source":       "manual",
		"status":       "completed",
		"completed_at": testutil.Raw("NOW() - INTERVAL '6 days'"),
	})

	// Cross-workspace probe.
	req := newDaemonTokenRequest("GET", "/api/daemon/autopilot-runs/"+runID+"/gc-check", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	req = withURLParam(req, "runId", runID)
	w := testutil.Call(t, testHandler.GetAutopilotRunGCCheck, req).Want(http.StatusNotFound)

	// Same-workspace probe.
	req = newDaemonTokenRequest("GET", "/api/daemon/autopilot-runs/"+runID+"/gc-check", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "runId", runID)
	w = testutil.Call(t, testHandler.GetAutopilotRunGCCheck, req).Want(http.StatusOK)
	var resp struct {
		Status      string `json:"status"`
		CompletedAt string `json:"completed_at"`
	}
	w.JSON(&resp)
	if resp.Status != "completed" {
		t.Fatalf("expected status %q, got %q", "completed", resp.Status)
	}
	if resp.CompletedAt == "" {
		t.Fatal("expected completed_at to be set for terminal run")
	}
}

// TestGetTaskGCCheck verifies the task gc-check endpoint that quick-create
// workdirs key on. Same anti-enumeration shape via requireDaemonTaskAccess.
func TestGetTaskGCCheck(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	// Quick-create-shaped task: no issue_id, no chat_session_id, no run id.
	// context.type is set so ResolveTaskWorkspaceID can recover workspace.
	quickContext, _ := json.Marshal(map[string]any{
		"type":         "quick_create",
		"prompt":       "fixture",
		"requester_id": testUserID,
		"workspace_id": testWorkspaceID,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"status":       "completed",
		"context":      quickContext,
		"completed_at": testutil.Raw("NOW()"),
	})
	defer testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)

	// Cross-workspace probe.
	req := newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/gc-check", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	req = withURLParam(req, "taskId", taskID)
	w := testutil.Call(t, testHandler.GetTaskGCCheck, req).Want(http.StatusNotFound)

	// Same-workspace probe — terminal task returns its status.
	req = newDaemonTokenRequest("GET", "/api/daemon/tasks/"+taskID+"/gc-check", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "taskId", taskID)
	w = testutil.Call(t, testHandler.GetTaskGCCheck, req).Want(http.StatusOK)
	var resp struct {
		Status      string `json:"status"`
		CompletedAt string `json:"completed_at"`
	}
	w.JSON(&resp)
	if resp.Status != "completed" {
		t.Fatalf("expected status %q, got %q", "completed", resp.Status)
	}
	if resp.CompletedAt == "" {
		t.Fatal("expected completed_at to be set for completed task")
	}
}

// ---------------------------------------------------------------------------
// Membership Cache Integration Tests
//
// These tests don't just exercise the cache primitive (that's covered in
// auth/membership_cache_test.go). They prove two things that the unit tests
// can't:
//
//  1. requireDaemonWorkspaceAccess actually short-circuits the DB on a cache
//     hit (the "ghost user" trick below).
//  2. Each handler that mutates membership actually calls
//     h.MembershipCache.Invalidate(...) — so a future refactor that drops
//     one of those calls will fail CI instead of silently leaking a stale
//     authorization grant for up to MembershipCacheTTL.
// ---------------------------------------------------------------------------

// installFreshMembershipCache swaps in a Redis-backed MembershipCache against
// a freshly-flushed Redis DB for the test, restoring the original on cleanup.
func installFreshMembershipCache(t *testing.T) {
	t.Helper()
	rdb := newRedisTestClient(t)
	origCache := testHandler.MembershipCache
	testHandler.MembershipCache = auth.NewMembershipCache(rdb)
	t.Cleanup(func() { testHandler.MembershipCache = origCache })
}

// createEphemeralUser inserts a throwaway user with a unique email and
// deletes it on test cleanup. Returns the user id as a string.
func createEphemeralUser(t *testing.T, label string) string {
	t.Helper()
	email := fmt.Sprintf("membership-cache-%s-%s@multica.ai", label, uuid.NewString())
	userID := dbfx.Insert(t, "user", testutil.Cols{
		"name":  "Membership Cache Test " + label,
		"email": email,
	})
	return userID
}

// createEphemeralMember creates a throwaway user AND a member row in the
// given workspace. Returns (userID, memberID). Both rows are cleaned up on
// test exit.
func createEphemeralMember(t *testing.T, workspaceID, label, role string) (string, string) {
	t.Helper()
	userID := createEphemeralUser(t, label)
	memberID := dbfx.Insert(t, "member", testutil.Cols{
		"workspace_id": workspaceID,
		"user_id":      userID,
		"role":         role,
	})
	return userID, memberID
}

// TestRequireDaemonWorkspaceAccess_CacheHit proves the cache lookup actually
// short-circuits the DB query. The trick: the request actor is a "ghost"
// user with NO member row in the workspace. With an empty cache the access
// check must fail; after priming the cache it must succeed. If a future
// change ever bypasses the cache and falls through to the DB, the priming
// step stops mattering and the second assertion catches it.
func TestRequireDaemonWorkspaceAccess_CacheHit(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	installFreshMembershipCache(t)
	ctx := context.Background()

	ghostUserID := createEphemeralUser(t, "ghost")

	// Baseline: with an empty cache the ghost has no path to access.
	req := newRequestAsUser(ghostUserID, "GET", "/api/daemon/workspaces/"+testWorkspaceID+"/repos", nil)
	w := httptest.NewRecorder()
	if testHandler.requireDaemonWorkspaceAccess(w, req, testWorkspaceID) {
		t.Fatal("setup: ghost user must not be allowed without cache priming")
	}

	// Priming the cache is the only thing that changes — the access check
	// must now succeed via the cache short-circuit.
	testHandler.MembershipCache.Set(ctx, ghostUserID, testWorkspaceID)

	req = newRequestAsUser(ghostUserID, "GET", "/api/daemon/workspaces/"+testWorkspaceID+"/repos", nil)
	w = httptest.NewRecorder()
	if !testHandler.requireDaemonWorkspaceAccess(w, req, testWorkspaceID) {
		t.Fatalf("expected access via cache hit, got denied (status %d)", w.Code)
	}
}

func TestRequireDaemonWorkspaceAccess_CacheMissBackfills(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	installFreshMembershipCache(t)

	ctx := context.Background()

	// Cache is empty — verify miss.
	if testHandler.MembershipCache.Get(ctx, testUserID, testWorkspaceID) {
		t.Fatal("expected cache miss before request")
	}

	// Make a request that triggers DB lookup.
	req := newRequest("GET", "/api/daemon/workspaces/"+testWorkspaceID+"/repos", nil)
	w := httptest.NewRecorder()
	if !testHandler.requireDaemonWorkspaceAccess(w, req, testWorkspaceID) {
		t.Fatalf("expected access granted via DB lookup, got denied (status %d)", w.Code)
	}

	// Cache should now be backfilled.
	if !testHandler.MembershipCache.Get(ctx, testUserID, testWorkspaceID) {
		t.Fatal("expected cache to be backfilled after DB hit")
	}
}

// TestMembershipCache_InvalidatedOnDeleteMember drives a real DeleteMember
// HTTP handler call and asserts the cache entry for the removed member is
// gone afterwards. Guards against future refactors that move or drop the
// h.MembershipCache.Invalidate(...) line in workspace.go.
func TestMembershipCache_InvalidatedOnDeleteMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	installFreshMembershipCache(t)
	ctx := context.Background()

	targetUserID, targetMemberID := createEphemeralMember(t, testWorkspaceID, "delete", "admin")
	testHandler.MembershipCache.Set(ctx, targetUserID, testWorkspaceID)
	if !testHandler.MembershipCache.Get(ctx, targetUserID, testWorkspaceID) {
		t.Fatal("setup: expected cache hit after Set")
	}

	req := newRequest("DELETE", "/api/workspaces/"+testWorkspaceID+"/members/"+targetMemberID, nil)
	req = withURLParams(req, "id", testWorkspaceID, "memberId", targetMemberID)
	testutil.Call(t, testHandler.DeleteMember, req).Want(http.StatusNoContent)

	if testHandler.MembershipCache.Get(ctx, targetUserID, testWorkspaceID) {
		t.Fatal("DeleteMember handler did not invalidate membership cache for removed user")
	}
}

// TestMembershipCache_InvalidatedOnUpdateMember drives a real UpdateMember
// (role change) call and asserts the cache entry is invalidated. The cache
// stores only the existence of membership, but UpdateMember still flushes
// the entry so any downstream caller that did add role-aware caching later
// would not silently see a stale role.
func TestMembershipCache_InvalidatedOnUpdateMember(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	installFreshMembershipCache(t)
	ctx := context.Background()

	targetUserID, targetMemberID := createEphemeralMember(t, testWorkspaceID, "update", "admin")
	testHandler.MembershipCache.Set(ctx, targetUserID, testWorkspaceID)

	req := newRequest("PUT", "/api/workspaces/"+testWorkspaceID+"/members/"+targetMemberID,
		map[string]any{"role": "member"})
	req = withURLParams(req, "id", testWorkspaceID, "memberId", targetMemberID)
	testutil.Call(t, testHandler.UpdateMember, req).Want(http.StatusOK)

	if testHandler.MembershipCache.Get(ctx, targetUserID, testWorkspaceID) {
		t.Fatal("UpdateMember handler did not invalidate membership cache for updated user")
	}
}

// TestMembershipCache_InvalidatedOnLeaveWorkspace exercises the self-removal
// path (LeaveWorkspace, not DeleteMember). Both handlers route through
// revokeAndRemoveMember, but the Invalidate call lives in the handler — a
// refactor that consolidates them could drop one invalidation without the
// other test catching it.
func TestMembershipCache_InvalidatedOnLeaveWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	installFreshMembershipCache(t)
	ctx := context.Background()

	targetUserID, _ := createEphemeralMember(t, testWorkspaceID, "leave", "admin")
	testHandler.MembershipCache.Set(ctx, targetUserID, testWorkspaceID)

	req := newRequestAsUser(targetUserID, "DELETE", "/api/workspaces/"+testWorkspaceID+"/leave", nil)
	req = withURLParam(req, "id", testWorkspaceID)
	testutil.Call(t, testHandler.LeaveWorkspace, req).Want(http.StatusNoContent)

	if testHandler.MembershipCache.Get(ctx, targetUserID, testWorkspaceID) {
		t.Fatal("LeaveWorkspace handler did not invalidate membership cache for leaver")
	}
}

// TestMembershipCache_InvalidatedOnDeleteWorkspace exercises the bulk
// invalidation path: when the workspace is deleted, every member's cache
// entry must be flushed. We create an isolated workspace with two members
// (owner + extra) so the shared testWorkspace stays intact.
func TestMembershipCache_InvalidatedOnDeleteWorkspace(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}
	installFreshMembershipCache(t)
	ctx := context.Background()

	const slug = "membership-cache-delete-ws"
	_, _ = testPool.Exec(ctx, `DELETE FROM workspace WHERE slug = $1`, slug)
	wsID := dbfx.Insert(t, "workspace", testutil.Cols{
		"name":         "Membership Cache Delete WS",
		"slug":         slug,
		"description":  "DeleteWorkspace cache invalidation test",
		"issue_prefix": "MCD",
	})

	// testUser must be an owner of the isolated workspace to call
	// DeleteWorkspace.
	dbfx.Exec(t, `
		INSERT INTO member (workspace_id, user_id, role) VALUES ($1, $2, 'owner')
	`, wsID, testUserID)

	extraUserID, _ := createEphemeralMember(t, wsID, "ws-delete-extra", "admin")

	testHandler.MembershipCache.Set(ctx, testUserID, wsID)
	testHandler.MembershipCache.Set(ctx, extraUserID, wsID)

	req := newRequest("DELETE", "/api/workspaces/"+wsID, nil)
	req = withURLParam(req, "id", wsID)
	testutil.Call(t, testHandler.DeleteWorkspace, req).Want(http.StatusNoContent)

	if testHandler.MembershipCache.Get(ctx, testUserID, wsID) {
		t.Fatal("DeleteWorkspace handler did not invalidate owner cache entry")
	}
	if testHandler.MembershipCache.Get(ctx, extraUserID, wsID) {
		t.Fatal("DeleteWorkspace handler did not invalidate extra-member cache entry")
	}
}

// createCommentTriggeredClaimTask seeds a queued comment-triggered task whose
// trigger comment is rooted under parentID (nil → trigger is itself a root).
// Returns the task id and the trigger comment id.
func createCommentTriggeredClaimTask(t *testing.T, ctx context.Context, agentID, runtimeID, issueID string, parentID *string) (string, string) {
	t.Helper()
	commentID := dbfx.Comment(t, issueID, "trigger comment", testutil.Cols{
		"parent_id": parentID,
	})

	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":         runtimeID,
		"issue_id":           issueID,
		"trigger_comment_id": commentID,
	})
	return taskID, commentID
}

type claimCommentTaskResp struct {
	Task *struct {
		ID               string `json:"id"`
		PriorSessionID   string `json:"prior_session_id"`
		TriggerCommentID string `json:"trigger_comment_id"`
		NewCommentCount  int    `json:"new_comment_count"`
		NewCommentsSince string `json:"new_comments_since"`
	} `json:"task"`
}

func claimCommentTask(t *testing.T, runtimeID, daemonID string) claimCommentTaskResp {
	t.Helper()
	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, daemonID)
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	var resp claimCommentTaskResp
	w.JSON(&resp)
	if resp.Task == nil {
		t.Fatalf("expected a claimed task, got nil: %s", w.Body.String())
	}
	return resp
}

// TestClaimTaskByRuntime_CommentTaskPopulatesNewCommentCount
// verifies the claim response carries new_comment_count + new_comments_since for
// a comment task when the agent ran before: the count is ISSUE-WIDE (covers
// every thread, not just the triggering one), excludes the injected trigger
// comment, excludes the agent's own comments, and the since anchor is the prior
// run's started_at.
func TestClaimTaskByRuntime_CommentTaskPopulatesNewCommentCount(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Comment newcount runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Comment newcount agent")

	// A prior run establishes the "since" anchor (its started_at, in the past).
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"started_at":   testutil.Raw("now() - interval '1 hour'"),
		"completed_at": testutil.Raw("now() - interval '50 minutes'"),
	})

	threadRootID := dbfx.Comment(t, issueID, "same-thread context")

	dbfx.Comment(t, issueID, "unrelated thread context")

	dbfx.Comment(t, issueID, "agent self reply", testutil.Cols{
		"author_type": "agent",
		"author_id":   agentID,
		"parent_id":   threadRootID,
	})

	// The trigger comment (member-authored, created now) lands after the anchor
	// but is injected into the prompt, so it should not be counted.
	_, triggerID := createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, &threadRootID)

	resp := claimCommentTask(t, runtimeID, "comment-newcount-claim")
	if resp.Task.TriggerCommentID != triggerID {
		t.Fatalf("trigger_comment_id = %s, want %s", resp.Task.TriggerCommentID, triggerID)
	}
	if resp.Task.NewCommentsSince == "" {
		t.Errorf("new_comments_since must be set when a prior run exists, got empty")
	}
	// Issue-wide: the same-thread context comment AND the unrelated-thread root
	// both count; only the agent's own reply and the injected trigger are excluded.
	if resp.Task.NewCommentCount != 2 {
		t.Errorf("new_comment_count = %d, want 2 (issue-wide: same-thread + unrelated thread)", resp.Task.NewCommentCount)
	}
}

// TestClaimTaskByRuntime_CommentTaskPopulatesInitiator verifies MUL-2645: the
// claim response surfaces the triggering comment's member author as the task
// initiator (type + id + name + email), so a workspace-visible agent learns who
// actually asked rather than seeing the runtime owner. createCommentTriggeredClaimTask
// authors the trigger comment as the fixture member (testUserID).
func TestClaimTaskByRuntime_CommentTaskPopulatesInitiator(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Comment initiator runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Comment initiator agent")
	taskID, _ := createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	req := newDaemonTokenRequest("POST", "/api/daemon/runtimes/"+runtimeID+"/tasks/claim", nil, testWorkspaceID, "comment-initiator-claim")
	req = withURLParam(req, "runtimeId", runtimeID)
	w := testutil.Call(t, testHandler.ClaimTaskByRuntime, req).Want(http.StatusOK)
	var resp struct {
		Task *struct {
			ID             string `json:"id"`
			InitiatorType  string `json:"initiator_type"`
			InitiatorID    string `json:"initiator_id"`
			InitiatorName  string `json:"initiator_name"`
			InitiatorEmail string `json:"initiator_email"`
		} `json:"task"`
	}
	w.JSON(&resp)
	if resp.Task == nil || resp.Task.ID != taskID {
		t.Fatalf("expected claimed task %s, got %s", taskID, w.Body.String())
	}
	if resp.Task.InitiatorType != "member" {
		t.Errorf("initiator_type = %q, want %q", resp.Task.InitiatorType, "member")
	}
	if resp.Task.InitiatorID != testUserID {
		t.Errorf("initiator_id = %q, want %q", resp.Task.InitiatorID, testUserID)
	}
	if resp.Task.InitiatorName != handlerTestName {
		t.Errorf("initiator_name = %q, want %q", resp.Task.InitiatorName, handlerTestName)
	}
	if resp.Task.InitiatorEmail != handlerTestEmail {
		t.Errorf("initiator_email = %q, want %q", resp.Task.InitiatorEmail, handlerTestEmail)
	}
}

func TestClaimTaskByRuntime_CommentTaskOmitsDeltaWhenOnlyTriggerIsNew(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Comment trigger-only runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Comment trigger-only agent")

	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"started_at":   testutil.Raw("now() - interval '1 hour'"),
		"completed_at": testutil.Raw("now() - interval '50 minutes'"),
	})

	_, triggerID := createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimCommentTask(t, runtimeID, "comment-trigger-only-claim")
	if resp.Task.TriggerCommentID != triggerID {
		t.Fatalf("trigger_comment_id = %s, want %s", resp.Task.TriggerCommentID, triggerID)
	}
	if resp.Task.NewCommentCount != 0 {
		t.Errorf("new_comment_count = %d, want 0 when only the injected trigger is new", resp.Task.NewCommentCount)
	}
	if resp.Task.NewCommentsSince != "" {
		t.Errorf("new_comments_since = %q, want empty when only the injected trigger is new", resp.Task.NewCommentsSince)
	}
}

// TestClaimTaskByRuntime_CommentResumeDefaultOn verifies comment-triggered tasks
// resume the prior session by default (no env flag), as long as the prior
// session ran on the same runtime.
func TestClaimTaskByRuntime_CommentResumeDefaultOn(t *testing.T) {
	if testHandler == nil || testPool == nil {
		t.Skip("database not available")
	}
	ctx := context.Background()
	runtimeID := createClaimReclaimRuntime(t, ctx, "Comment resume runtime")
	agentID, issueID := createClaimReclaimAgentAndIssue(t, ctx, runtimeID, "Comment resume agent")

	// A prior completed task on the same (agent, issue, runtime) with a session.
	const priorSession = "sess-prior-123"
	dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":   runtimeID,
		"issue_id":     issueID,
		"status":       "completed",
		"session_id":   priorSession,
		"completed_at": testutil.Raw("now()"),
	})

	createCommentTriggeredClaimTask(t, ctx, agentID, runtimeID, issueID, nil)

	resp := claimCommentTask(t, runtimeID, "comment-resume-default")
	if resp.Task.PriorSessionID != priorSession {
		t.Errorf("prior_session_id = %q, want %q (comment resume is default-on)", resp.Task.PriorSessionID, priorSession)
	}
}

// TestAckTaskCancelled verifies the cancel-ack endpoint settles a deferred
// chat finalization (marker claimed, Stopped. row written for a transcript
// that filled in late) and keeps the anti-enumeration shape of
// requireDaemonTaskAccess for cross-workspace tokens.
func TestAckTaskCancelled(t *testing.T) {
	if testHandler == nil {
		t.Skip("database not available")
	}

	ctx := context.Background()

	var agentID, runtimeID string
	dbfx.QueryRow(t, `
		SELECT a.id, a.runtime_id FROM agent a WHERE a.workspace_id = $1 LIMIT 1
	`, testWorkspaceID).Scan(&agentID, &runtimeID)

	chatSessionID := dbfx.ChatSession(t, agentID, testutil.Cols{
		"title": "cancel ack test",
	})

	// Cancelled chat task with a pending deferred-finalize marker, plus a
	// transcript row that landed after the cancel (the daemon's late flush).
	taskID := dbfx.Task(t, agentID, testutil.Cols{
		"runtime_id":                runtimeID,
		"issue_id":                  nil,
		"chat_session_id":           chatSessionID,
		"status":                    "cancelled",
		"started_at":                testutil.Raw("now()"),
		"completed_at":              testutil.Raw("now()"),
		"chat_finalize_deferred_at": testutil.Raw("now()"),
	})
	t.Cleanup(func() {
		testPool.Exec(ctx, `DELETE FROM task_message WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM chat_message WHERE task_id = $1`, taskID)
		testPool.Exec(ctx, `DELETE FROM agent_task_queue WHERE id = $1`, taskID)
	})
	dbfx.Exec(t, `
		INSERT INTO task_message (task_id, seq, type, content)
		VALUES ($1, 1, 'text', 'late flush')
	`, taskID)

	// Cross-workspace daemon token must still 404 and leave the marker armed.
	req := newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/cancel-ack", nil,
		"00000000-0000-0000-0000-000000000000", "attacker-daemon")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.AckTaskCancelled, req).Want(http.StatusNotFound)
	var deferredAt *time.Time
	dbfx.QueryRow(t, `
		SELECT chat_finalize_deferred_at FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&deferredAt)
	if deferredAt == nil {
		t.Fatal("cross-workspace ack must not claim the marker")
	}

	// Same-workspace token settles the deferred finalize.
	req = newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/cancel-ack", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.AckTaskCancelled, req).Want(http.StatusOK)
	dbfx.QueryRow(t, `
		SELECT chat_finalize_deferred_at FROM agent_task_queue WHERE id = $1
	`, taskID).Scan(&deferredAt)
	if deferredAt != nil {
		t.Errorf("marker should be claimed, got %v", deferredAt)
	}
	var stopped int
	dbfx.QueryRow(t, `
		SELECT count(*) FROM chat_message WHERE task_id = $1 AND role = 'assistant' AND content = 'Stopped.'
	`, taskID).Scan(&stopped)
	if stopped != 1 {
		t.Errorf("Stopped. rows = %d, want 1", stopped)
	}

	// Idempotent: a second ack is a no-op (no duplicate Stopped.).
	req = newDaemonTokenRequest("POST", "/api/daemon/tasks/"+taskID+"/cancel-ack", nil,
		testWorkspaceID, "legit-daemon")
	req = withURLParam(req, "taskId", taskID)
	testutil.Call(t, testHandler.AckTaskCancelled, req).Want(http.StatusOK)
	dbfx.QueryRow(t, `
		SELECT count(*) FROM chat_message WHERE task_id = $1 AND role = 'assistant' AND content = 'Stopped.'
	`, taskID).Scan(&stopped)
	if stopped != 1 {
		t.Errorf("Stopped. rows after second ack = %d, want 1", stopped)
	}
}
