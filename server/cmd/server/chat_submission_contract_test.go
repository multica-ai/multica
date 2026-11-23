package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/events"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
	"github.com/prometheus/client_golang/prometheus"
)

func submissionHTTP(serverURL, token, workspaceID, method, path string, body any) (int, map[string]any, error) {
	encoded, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	req, err := http.NewRequest(method, serverURL+path, bytes.NewReader(encoded))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("X-Workspace-ID", workspaceID)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && err != io.EOF {
		return resp.StatusCode, nil, err
	}
	return resp.StatusCode, out, nil
}

func submissionStatus(t *testing.T, serverURL, token, workspaceID, method, path string, body any, want int) map[string]any {
	t.Helper()
	status, out, err := submissionHTTP(serverURL, token, workspaceID, method, path, body)
	if err != nil || status != want {
		t.Fatalf("%s %s status=%d want=%d response=%v error=%v", method, path, status, want, out, err)
	}
	return out
}

func cleanupSubmissionSession(t *testing.T, token, workspaceID, sessionID string) {
	t.Helper()
	t.Cleanup(func() {
		status, body, err := submissionHTTP(testServer.URL, token, workspaceID, "DELETE", "/api/chat/sessions/"+sessionID, nil)
		if err != nil || (status != http.StatusNoContent && status != http.StatusNotFound) {
			t.Errorf("cleanup session=%s status=%d body=%v error=%v", sessionID, status, body, err)
		}
	})
}

func submissionCounts(t *testing.T, sessionID string, wantMessages, wantTasks, wantReceipts int) {
	t.Helper()
	fx := workspaceFixture(t)
	for _, check := range []struct {
		table string
		want  int
	}{{"chat_message", wantMessages}, {"agent_task_queue", wantTasks}, {"chat_submission_receipt", wantReceipts}} {
		if got := fx.Count(t, "SELECT count(*) FROM "+check.table+" WHERE chat_session_id=$1", sessionID); got != check.want {
			t.Errorf("%s count=%d want=%d", check.table, got, check.want)
		}
	}
}

func TestChatSubmissionIdentityAndConflict(t *testing.T) {
	sessionID, _ := submissionSession(t)
	path := "/api/chat/sessions/" + sessionID + "/messages"
	id := uuid.NewString()
	body := map[string]any{"content": "exact content\n", "submission_id": id}
	first := submissionSend(t, sessionID, body)
	for _, attachmentIDs := range []any{nil, []string{}} {
		body["attachment_ids"] = attachmentIDs
		if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(first, replay) {
			t.Fatalf("equivalent replay changed acceptance first=%v replay=%v", first, replay)
		}
	}
	for _, content := range []string{"exact content", "different content", ""} {
		submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, map[string]any{"content": content, "submission_id": id}, http.StatusConflict)
	}
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, map[string]any{"content": "exact content\n", "submission_id": id, "attachment_ids": []string{uuid.NewString()}}, http.StatusConflict)
	submissionCounts(t, sessionID, 1, 1, 1)
	body["submission_id"] = uuid.NewString()
	second := submissionSend(t, sessionID, body)
	if second["task_id"] == first["task_id"] || second["queued"] != true {
		t.Fatalf("new submission identity response=%v", second)
	}
	for _, invalid := range []string{"", "malformed", "1234"} {
		submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, map[string]any{"content": "new", "submission_id": invalid}, http.StatusBadRequest)
	}
	for i := 0; i < 2; i++ {
		submissionSend(t, sessionID, map[string]any{"content": "legacy null", "submission_id": nil})
		submissionSend(t, sessionID, map[string]any{"content": "legacy omitted"})
	}
	submissionCounts(t, sessionID, 6, 6, 2)
}

type submissionWakeupCounter struct{ calls atomic.Int32 }

func (w *submissionWakeupCounter) NotifyTaskAvailable(string, string) { w.calls.Add(1) }

func TestChatSubmissionConcurrentFirstSendAndPersistentReplay(t *testing.T) {
	sessionID, _ := submissionSession(t)
	bus := events.New()
	var messageEvents, queuedEvents atomic.Int32
	bus.Subscribe(protocol.EventChatMessage, func(events.Event) { messageEvents.Add(1) })
	bus.Subscribe(protocol.EventTaskQueued, func(events.Event) { queuedEvents.Add(1) })
	hub := realtime.NewHub()
	go hub.Run()
	metrics := obsmetrics.NewBusinessMetrics()
	registry := prometheus.NewRegistry()
	registry.MustRegister(metrics.Collectors()...)
	router, h := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{BusinessMetrics: metrics})
	wakeups := &submissionWakeupCounter{}
	h.TaskService.Wakeup = wakeups
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	body := map[string]any{"content": "parallel accepted turn", "submission_id": uuid.NewString()}
	path := "/api/chat/sessions/" + sessionID + "/messages"
	start := make(chan struct{})
	const requests = 12
	results := make(chan map[string]any, requests)
	errors := make(chan error, requests)
	var wg sync.WaitGroup
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			status, result, err := submissionHTTP(server.URL, testToken, testWorkspaceID, "POST", path, body)
			if err != nil || status != http.StatusCreated {
				errors <- fmt.Errorf("status=%d response=%v error=%v", status, result, err)
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errors)
	for err := range errors {
		t.Error(err)
	}
	var original map[string]any
	for result := range results {
		if original == nil {
			original = result
		}
		if !reflect.DeepEqual(original, result) {
			t.Errorf("parallel responses diverged original=%v result=%v", original, result)
		}
	}
	submissionCounts(t, sessionID, 1, 1, 1)
	if messageEvents.Load() != 1 || queuedEvents.Load() != 1 {
		t.Fatalf("events message=%d queued=%d", messageEvents.Load(), queuedEvents.Load())
	}
	// A fresh router builds fresh service instances and reads the committed receipt.
	var beforeTouch string
	workspaceFixture(t).QueryRow(t, `SELECT updated_at::text FROM chat_session WHERE id=$1`, sessionID).Scan(&beforeTouch)
	restartedRouter, restartedHandler := NewRouterWithOptions(testPool, hub, bus, analytics.NoopClient{}, nil, RouterOptions{BusinessMetrics: metrics})
	restartedHandler.TaskService.Wakeup = wakeups
	restarted := httptest.NewServer(restartedRouter)
	t.Cleanup(restarted.Close)
	replay := submissionStatus(t, restarted.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusCreated)
	if !reflect.DeepEqual(original, replay) || messageEvents.Load() != 1 || queuedEvents.Load() != 1 {
		t.Fatalf("fresh instance replay=%v original=%v events=%d/%d", replay, original, messageEvents.Load(), queuedEvents.Load())
	}
	var afterTouch string
	workspaceFixture(t).QueryRow(t, `SELECT updated_at::text FROM chat_session WHERE id=$1`, sessionID).Scan(&afterTouch)
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var sentMetrics float64
	for _, family := range families {
		if family.GetName() == "multica_chat_message_sent_total" {
			for _, metric := range family.Metric {
				sentMetrics += metric.GetCounter().GetValue()
			}
		}
	}
	if beforeTouch != afterTouch || wakeups.calls.Load() != 1 || sentMetrics != 1 {
		t.Fatalf("replay side effects touch=%s/%s wakeups=%d chat metrics=%v", beforeTouch, afterTouch, wakeups.calls.Load(), sentMetrics)
	}
}

func TestChatSubmissionAttachmentSnapshotAndCanonicalRequest(t *testing.T) {
	sessionID, _ := submissionSession(t)
	fx := workspaceFixture(t)
	newAttachment := func() string {
		return fx.Insert(t, "attachment", testutil.Cols{
			"workspace_id": testWorkspaceID, "chat_session_id": sessionID,
			"uploader_type": "member", "uploader_id": testUserID,
			"filename": "offline.txt", "url": "https://example.invalid/offline.txt", "content_type": "text/plain", "size_bytes": 1,
		})
	}
	a, b := newAttachment(), newAttachment()
	missing := uuid.NewString()
	body := map[string]any{"content": "attached", "submission_id": uuid.NewString(), "attachment_ids": []string{a, b, a, missing}}
	first := submissionSend(t, sessionID, body)
	bound, ok := first["attachment_ids"].([]any)
	if !ok || len(bound) != 2 {
		t.Fatalf("actual binding snapshot=%v", first)
	}
	body["attachment_ids"] = []string{missing, b, a}
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(first, replay) {
		t.Fatalf("canonical request changed acceptance %v", replay)
	}
	fx.Exec(t, `DELETE FROM attachment WHERE id=$1`, a)
	fx.Exec(t, `UPDATE attachment SET chat_message_id=NULL, chat_session_id=NULL WHERE id=$1`, b)
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(first, replay) {
		t.Fatalf("attachment deletion changed historical acceptance %v", replay)
	}
	if count := fx.Count(t, `SELECT count(*) FROM attachment WHERE id=$1 AND chat_message_id IS NOT NULL`, b); count != 0 {
		t.Fatal("replay rebound an attachment")
	}
	body["attachment_ids"] = []string{b}
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", body, http.StatusConflict)
	submissionCounts(t, sessionID, 1, 1, 1)
}

func TestChatSubmissionReplayAfterArchiveAndDelete(t *testing.T) {
	sessionID, agentID := submissionSession(t)
	fx := workspaceFixture(t)
	body := map[string]any{"content": "accepted before archive", "submission_id": uuid.NewString()}
	first := submissionSend(t, sessionID, body)
	fx.Exec(t, `UPDATE chat_session SET status='archived' WHERE id=$1`, sessionID)
	fx.Exec(t, `UPDATE agent SET archived_at=now(), runtime_id=NULL WHERE id=$1`, agentID)
	t.Cleanup(func() {
		fx.Exec(t, `UPDATE agent SET archived_at=NULL, runtime_id=(SELECT id FROM agent_runtime WHERE workspace_id=$2 AND name='Integration Test Runtime') WHERE id=$1`, agentID, testWorkspaceID)
	})
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(first, replay) {
		t.Fatalf("archive changed historical acceptance %v", replay)
	}
	path := "/api/chat/sessions/" + sessionID
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "DELETE", path, nil, http.StatusNoContent)
	submissionCounts(t, sessionID, 0, 0, 0)
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path+"/messages", body, http.StatusNotFound)
}

func TestChatSubmissionTransactionFailureRollsBackTurn(t *testing.T) {
	sessionID, _ := submissionSession(t)
	body := map[string]any{"content": "atomic accepted turn", "submission_id": uuid.NewString()}
	hub := realtime.NewHub()
	go hub.Run()
	router, h := NewRouterWithOptions(testPool, hub, events.New(), analytics.NoopClient{}, nil, RouterOptions{})
	server := httptest.NewServer(router)
	t.Cleanup(server.Close)
	h.TaskService.TxStarter = membershipFailCommitPool{testPool}
	path := "/api/chat/sessions/" + sessionID + "/messages"
	submissionStatus(t, server.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusInternalServerError)
	submissionCounts(t, sessionID, 0, 0, 0)
	var title string
	workspaceFixture(t).QueryRow(t, `SELECT title FROM chat_session WHERE id=$1`, sessionID).Scan(&title)
	if title != "" {
		t.Fatalf("failed transaction left a title=%q", title)
	}
	h.TaskService.TxStarter = nil
	submissionStatus(t, server.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusInternalServerError)
	submissionCounts(t, sessionID, 0, 0, 0)
	h.TaskService.TxStarter = testPool
	submissionStatus(t, server.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusCreated)
	submissionCounts(t, sessionID, 1, 1, 1)
}

func TestChatSubmissionActorAndSessionNamespaces(t *testing.T) {
	sessionID, agentID := submissionSession(t)
	path := "/api/chat/sessions/" + sessionID + "/messages"
	body := map[string]any{"content": "scoped identity", "submission_id": uuid.NewString()}
	member := submissionSend(t, sessionID, body)
	tokenA := mintAgentTaskToken(t, agentID, member["task_id"].(string), testUserID)
	agent := submissionStatus(t, testServer.URL, tokenA, testWorkspaceID, "POST", path, body, http.StatusCreated)
	if member["task_id"] == agent["task_id"] {
		t.Fatal("member and agent shared an acceptance namespace")
	}
	// A new token bound to another task of the same logical agent replays it.
	fx := workspaceFixture(t)
	var runtimeID string
	fx.QueryRow(t, `SELECT runtime_id::text FROM agent WHERE id=$1`, agentID).Scan(&runtimeID)
	taskID := fx.Task(t, agentID, testutil.Cols{"runtime_id": runtimeID, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	tokenB := mintAgentTaskToken(t, agentID, taskID, testUserID)
	replay := submissionStatus(t, testServer.URL, tokenB, testWorkspaceID, "POST", path, body, http.StatusCreated)
	if !reflect.DeepEqual(agent, replay) {
		t.Fatalf("token rotation changed logical caller agent=%v replay=%v", agent, replay)
	}
	otherAgent := fx.Agent(t, "Other submission actor", runtimeID)
	otherTask := fx.Task(t, otherAgent, testutil.Cols{"runtime_id": runtimeID, "originator_user_id": testUserID, "accountable_user_id": testUserID})
	otherToken := mintAgentTaskToken(t, otherAgent, otherTask, testUserID)
	other := submissionStatus(t, testServer.URL, otherToken, testWorkspaceID, "POST", path, body, http.StatusCreated)
	if other["task_id"] == agent["task_id"] || other["task_id"] == member["task_id"] {
		t.Fatal("different logical agent reused an acceptance")
	}
	otherSession, _ := submissionSession(t)
	independent := submissionSend(t, otherSession, body)
	if independent["task_id"] == member["task_id"] {
		t.Fatal("different session reused an acceptance")
	}
	submissionCounts(t, sessionID, 3, 3, 3)
	submissionCounts(t, otherSession, 1, 1, 1)
}

func TestChatSubmissionCurrentAuthorizationBeforeReplay(t *testing.T) {
	sessionID, agentID := submissionSession(t)
	fx := workspaceFixture(t)
	body := map[string]any{"content": "private acceptance", "submission_id": uuid.NewString()}
	first := submissionSend(t, sessionID, body)
	agentToken := mintAgentTaskToken(t, agentID, first["task_id"].(string), testUserID)
	submissionStatus(t, testServer.URL, agentToken, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", body, http.StatusCreated)
	path := "/api/chat/sessions/" + sessionID + "/messages"
	otherID := fx.User(t, "Other submission user", uuid.NewString()+"@example.test")
	fx.Member(t, testWorkspaceID, otherID, "member")
	otherToken, err := generateTestJWT(otherID, "other@example.test", "Other")
	if err != nil {
		t.Fatal(err)
	}
	submissionStatus(t, testServer.URL, otherToken, testWorkspaceID, "POST", path, body, http.StatusForbidden)
	foreign := fx.Workspace(t, "Foreign submission", "foreign-"+uuid.NewString())
	fx.Member(t, foreign, testUserID, "owner")
	submissionStatus(t, testServer.URL, testToken, foreign, "POST", path, body, http.StatusNotFound)
	fx.Exec(t, `UPDATE agent SET visibility='private', permission_mode='private', owner_id=$2 WHERE id=$1`, agentID, otherID)
	t.Cleanup(func() {
		fx.Exec(t, `UPDATE agent SET visibility='workspace', owner_id=$2 WHERE id=$1`, agentID, testUserID)
	})
	// Workspace owners retain view access but cannot invoke another owner's private agent.
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusForbidden)
	submissionStatus(t, testServer.URL, agentToken, testWorkspaceID, "POST", path, body, http.StatusForbidden)
	fx.Exec(t, `UPDATE member SET role='member' WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		fx.Exec(t, `UPDATE member SET role='owner' WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, testUserID)
	})
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusForbidden)
	fx.Exec(t, `UPDATE agent SET visibility='workspace', owner_id=$2 WHERE id=$1`, agentID, testUserID)
	fx.Exec(t, `DELETE FROM member WHERE workspace_id=$1 AND user_id=$2`, testWorkspaceID, testUserID)
	t.Cleanup(func() {
		fx.Exec(t, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner') ON CONFLICT DO NOTHING`, testWorkspaceID, testUserID)
	})
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, body, http.StatusNotFound)
	submissionStatus(t, testServer.URL, agentToken, testWorkspaceID, "POST", path, body, http.StatusNotFound)
	fx.Exec(t, `INSERT INTO member (workspace_id,user_id,role) VALUES ($1,$2,'owner')`, testWorkspaceID, testUserID)
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(first, replay) {
		t.Fatalf("restored authorization changed acceptance=%v", replay)
	}
	submissionCounts(t, sessionID, 2, 2, 2)
}

func TestChatSubmissionCancelAndExplicitRestore(t *testing.T) {
	sessionID, _ := submissionSession(t)
	body := map[string]any{"content": "draft to restore", "submission_id": uuid.NewString()}
	first := submissionSend(t, sessionID, body)
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/tasks/"+first["task_id"].(string)+"/cancel", nil, http.StatusOK)
	// Unstarted empty turns are removed immediately. A replay acknowledges the
	// original accepted task without bringing that cancelled message back.
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(first, replay) {
		t.Fatalf("cancelled acceptance changed %v", replay)
	}
	submissionCounts(t, sessionID, 0, 1, 1)
	fx := workspaceFixture(t)
	var status string
	fx.QueryRow(t, `SELECT status FROM agent_task_queue WHERE id=$1`, first["task_id"]).Scan(&status)
	if status != "cancelled" {
		t.Fatalf("replay changed cancelled task to %s", status)
	}
	body["submission_id"] = uuid.NewString()
	restored := submissionSend(t, sessionID, body)
	if restored["task_id"] == first["task_id"] {
		t.Fatal("explicit restore must create a new task")
	}
	submissionCounts(t, sessionID, 1, 2, 2)
}

func TestChatSubmissionQueueEditPreservesAcceptance(t *testing.T) {
	sessionID, _ := submissionSession(t)
	submissionSend(t, sessionID, map[string]any{"content": "visible head"})
	body := map[string]any{"content": "original follow-up", "submission_id": uuid.NewString()}
	accepted := submissionSend(t, sessionID, body)
	path := "/api/tasks/" + accepted["task_id"].(string) + "/cancel?expected_status=queued&chat_session_id=" + sessionID + "&queue_action=edit"
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", path, nil, http.StatusOK)
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(accepted, replay) || replay["queued"] != true {
		t.Fatalf("queue edit changed acceptance=%v", replay)
	}
	body["content"] = "edited follow-up"
	submissionStatus(t, testServer.URL, testToken, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", body, http.StatusConflict)
	body["submission_id"] = uuid.NewString()
	submissionSend(t, sessionID, body)
	submissionCounts(t, sessionID, 2, 3, 2)
}

func TestChatSubmissionIgnoresSpoofedActorHeaders(t *testing.T) {
	sessionID, agentID := submissionSession(t)
	body := map[string]any{"content": "member actor", "submission_id": uuid.NewString()}
	accepted := submissionSend(t, sessionID, body)
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest("POST", testServer.URL+"/api/chat/sessions/"+sessionID+"/messages", bytes.NewReader(encoded))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("X-Workspace-ID", testWorkspaceID)
	req.Header.Set("X-Agent-ID", agentID)
	req.Header.Set("X-Task-ID", accepted["task_id"].(string))
	req.Header.Set("X-Actor-Source", "task_token")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusCreated {
		resp.Body.Close()
		t.Fatalf("spoofed-header status=%d", resp.StatusCode)
	}
	var replay map[string]any
	readJSON(t, resp, &replay)
	if !reflect.DeepEqual(accepted, replay) {
		t.Fatalf("spoofed actor changed member acceptance=%v", replay)
	}
	submissionCounts(t, sessionID, 1, 1, 1)
}

func TestChatSubmissionPreservesProjectContextContract(t *testing.T) {
	fx := workspaceFixture(t)
	caller := fx.User(t, "Submission project member", uuid.NewString()+"@example.test")
	fx.Member(t, testWorkspaceID, caller, "member")
	token, err := generateTestJWT(caller, "project-member@example.test", "Project member")
	if err != nil {
		t.Fatal(err)
	}
	runtime := fx.Runtime(t, "Submission project runtime", testutil.Cols{"owner_id": caller})
	agent := fx.Agent(t, "Submission project agent", runtime, testutil.Cols{"owner_id": caller})
	project := fx.Project(t, "Submission project context")
	created := submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", "/api/chat/sessions", map[string]any{"agent_id": agent, "project_id": project}, http.StatusCreated)
	sessionID := created["id"].(string)
	cleanupSubmissionSession(t, token, testWorkspaceID, sessionID)
	path := "/api/chat/sessions/" + sessionID + "/messages"
	body := map[string]any{"content": "project conversation", "submission_id": uuid.NewString()}
	accepted := submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", path, body, http.StatusCreated)
	fx.Exec(t, `UPDATE project SET visibility='private' WHERE id=$1`, project)
	submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", "/api/chat/sessions", map[string]any{"agent_id": agent, "project_id": project}, http.StatusNotFound)
	submissionStatus(t, testServer.URL, token, testWorkspaceID, "PATCH", "/api/chat/sessions/"+sessionID, map[string]any{"project_id": project}, http.StatusNotFound)
	// The existing send path retains the selected project until claim time.
	// Both legacy and keyed sends keep that behavior after visibility changes.
	submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", path, map[string]any{"content": "legacy after visibility change"}, http.StatusCreated)
	if replay := submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", path, body, http.StatusCreated); !reflect.DeepEqual(accepted, replay) {
		t.Fatalf("project visibility changed historical acceptance=%v", replay)
	}
	body["submission_id"] = uuid.NewString()
	submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", path, body, http.StatusCreated)
	var stored string
	fx.QueryRow(t, `SELECT project_id::text FROM chat_session WHERE id=$1`, sessionID).Scan(&stored)
	if stored != project {
		t.Fatalf("selected context changed to %s", stored)
	}
}

func TestChatSubmissionAttachmentOwnershipPerActor(t *testing.T) {
	sessionID, agentID := submissionSession(t)
	fx := workspaceFixture(t)
	attachment := fx.Insert(t, "attachment", testutil.Cols{
		"workspace_id": testWorkspaceID, "chat_session_id": sessionID,
		"uploader_type": "agent", "uploader_id": agentID,
		"filename": "agent.txt", "url": "https://example.invalid/agent.txt", "content_type": "text/plain", "size_bytes": 1,
	})
	body := map[string]any{"content": "actor scoped attachment", "submission_id": uuid.NewString(), "attachment_ids": []string{attachment}}
	member := submissionSend(t, sessionID, body)
	if bound, ok := member["attachment_ids"].([]any); !ok || len(bound) != 0 {
		t.Fatalf("member bound another actor's attachment=%v", member)
	}
	token := mintAgentTaskToken(t, agentID, member["task_id"].(string), testUserID)
	agent := submissionStatus(t, testServer.URL, token, testWorkspaceID, "POST", "/api/chat/sessions/"+sessionID+"/messages", body, http.StatusCreated)
	if bound, ok := agent["attachment_ids"].([]any); !ok || len(bound) != 1 || bound[0] != attachment {
		t.Fatalf("agent failed to bind its own attachment=%v", agent)
	}
	if replay := submissionSend(t, sessionID, body); !reflect.DeepEqual(member, replay) {
		t.Fatalf("member's empty binding snapshot changed=%v", replay)
	}
	submissionCounts(t, sessionID, 2, 2, 2)
}
