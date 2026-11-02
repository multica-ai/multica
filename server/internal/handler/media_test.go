package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/daemonws"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func mediaTestRuntime(t *testing.T, provider string, used int) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"status": "ok", "windows": []map[string]any{{"name": "primary", "used_percent": used}}})
	tag := protocol.MediaCapabilityTag(protocol.MediaProvider(provider), "image-generate")
	meta, _ := json.Marshal(map[string]any{"capabilities": []string{tag}})
	return dbfx.Runtime(t, "media", testutil.Cols{"provider": provider, "daemon_id": uuid.NewString(), "metadata": string(meta), "plan_quota": string(raw)})
}

func createTestMediaJob(t *testing.T, h *Handler, request protocol.MediaRequest) protocol.MediaJob {
	t.Helper()
	var job protocol.MediaJob
	testutil.Call(t, h.CreateMediaJob, newRequest("POST", "/api/media/jobs", request)).Want(202).JSON(&job)
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM media_job WHERE id=$1", job.ID) })
	return job
}

func claimTestMediaJob(t *testing.T, h *Handler, runtimeID string) *protocol.MediaAssignment {
	t.Helper()
	raw, _ := json.Marshal(protocol.MediaClaimRequest{RuntimeID: runtimeID})
	status, body, err := h.DaemonRPCHandler(t.Context(), daemonws.ClientIdentity{UserID: testUserID, WorkspaceID: testWorkspaceID, RuntimeIDs: []string{runtimeID}}, "media.claim", raw)
	if err != nil || status != 200 {
		t.Fatalf("claim: %d %s %v", status, body, err)
	}
	var out protocol.MediaClaimResponse
	if err := json.Unmarshal(body, &out); err != nil {
		t.Fatal(err)
	}
	return out.Assignment
}

func TestMediaRoutingReceiptAndCancellation(t *testing.T) {
	h := *testHandler
	first := mediaTestRuntime(t, "codex", 5)
	second := mediaTestRuntime(t, "antigravity", 25)
	job := createTestMediaJob(t, &h, protocol.MediaRequest{Provider: "auto", Capability: "image-generate", Prompt: "image"})
	if claimTestMediaJob(t, &h, second) != nil {
		t.Fatal("lower quota rank claimed")
	}
	a := claimTestMediaJob(t, &h, first)
	if a == nil || a.Job.ID != job.ID {
		t.Fatal("highest quota candidate did not claim")
	}
	report := protocol.MediaResult{RuntimeID: first, JobID: job.ID, Attempt: a.Attempt, RateLimited: true, RetryAfter: 60}
	testutil.Call(t, h.ReportMediaJob, newRequest("POST", "/", report)).Want(200)
	testutil.Call(t, h.ReportMediaJob, newRequest("POST", "/", report)).Want(200)
	if claimTestMediaJob(t, &h, first) != nil {
		t.Fatal("cooldown ignored")
	}
	b := claimTestMediaJob(t, &h, second)
	if b == nil || b.Attempt == a.Attempt {
		t.Fatal("429 did not switch to agy")
	}
	testutil.Call(t, h.ReportMediaJob, newRequest("POST", "/", report)).Want(409)
	testutil.Call(t, h.CancelMediaJob, withURLParam(newRequest("POST", "/", nil), "id", job.ID)).Want(200)
	req := withURLParam(httptest.NewRequest("GET", "/", nil), "id", job.ID)
	req.Header.Set("Authorization", "Bearer "+b.Token)
	testutil.Call(t, h.MediaAttemptStatus, req).Want(409)
	var got protocol.MediaJob
	testutil.Call(t, h.ReportMediaJob, newRequest("POST", "/", protocol.MediaResult{RuntimeID: second, JobID: job.ID, Attempt: b.Attempt})).Want(200).JSON(&got)
	if got.State != "cancelled" {
		t.Fatal("late completion resurrected cancelled job")
	}
}

func TestMediaOutputBindingAndTokenScope(t *testing.T) {
	h := *testHandler
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	h.Storage = storage.NewLocalStorageFromEnv()
	runtimeID := mediaTestRuntime(t, "codex", 0)
	issueID := dbfx.Issue(t, "media output")
	agentID := dbfx.Agent(t, "media producer", runtimeID)
	taskID := dbfx.Task(t, agentID, testutil.Cols{"issue_id": issueID, "runtime_id": runtimeID})
	req := newRequest("POST", "/", protocol.MediaRequest{Capability: "image-generate", Provider: "codex", Prompt: "image"})
	req.Header.Set("X-Actor-Source", "task_token")
	req.Header.Set("X-Task-ID", taskID)
	req.Header.Set("X-Agent-ID", agentID)
	var job protocol.MediaJob
	testutil.Call(t, h.CreateMediaJob, req).Want(202).JSON(&job)
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM media_job WHERE id=$1", job.ID) })
	a := claimTestMediaJob(t, &h, runtimeID)
	if a == nil {
		t.Fatal("no assignment")
	}
	data := append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 2048)...)
	upload := func(token, jobID string) *http.Request {
		r := withURLParam(httptest.NewRequest("POST", "/?filename=image.png", bytes.NewReader(data)), "id", jobID)
		r.Header.Set("Authorization", "Bearer "+token)
		return r
	}
	testutil.Call(t, h.UploadMediaOutput, upload(a.Token, uuid.NewString())).Want(401)
	var att, duplicate AttachmentResponse
	testutil.Call(t, h.UploadMediaOutput, upload(a.Token, job.ID)).Want(200).JSON(&att)
	testutil.Call(t, h.UploadMediaOutput, upload(a.Token, job.ID)).Want(200).JSON(&duplicate)
	t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM attachment WHERE id=$1", att.ID) })
	if att.ID != duplicate.ID || att.IssueID == nil || *att.IssueID != issueID {
		t.Fatalf("binding/idempotency: %+v %+v", att, duplicate)
	}
	stored, err := h.Queries.GetAttachment(t.Context(), db.GetAttachmentParams{ID: parseUUID(att.ID), WorkspaceID: parseUUID(testWorkspaceID)})
	if err != nil {
		t.Fatal(err)
	}
	if uuidToString(stored.TaskID) != taskID || uuidToString(stored.UploaderID) != agentID {
		t.Fatal("output not bound to original producer")
	}
	var completed protocol.MediaJob
	testutil.Call(t, h.ReportMediaJob, newRequest("POST", "/", protocol.MediaResult{RuntimeID: runtimeID, JobID: job.ID, Attempt: a.Attempt})).Want(200).JSON(&completed)
	if completed.State != "completed" || completed.AttachmentID != att.ID || completed.MarkdownURL == "" || completed.Markdown == "" {
		t.Fatalf("missing durable receipt: %+v", completed)
	}
	testutil.Call(t, h.UploadMediaOutput, upload(a.Token, job.ID)).Want(409)
	// Another task of the same caller cannot inspect this task's job.
	read := withURLParam(newRequest("GET", "/", nil), "id", job.ID)
	read.Header.Set("X-Actor-Source", "task_token")
	read.Header.Set("X-Task-ID", uuid.NewString())
	testutil.Call(t, h.GetMediaJob, read).Want(404)
}

func TestMediaPermissionsAndMalformedInput(t *testing.T) {
	for _, body := range []any{`{`, protocol.MediaRequest{Capability: "no", Provider: "auto", Prompt: "p"}, protocol.MediaRequest{Capability: "image-edit", Provider: "auto", Prompt: "p"}, protocol.MediaRequest{Capability: "image-generate", Provider: "auto", Prompt: "p", Inputs: []protocol.MediaInput{{AttachmentID: "bad"}}}} {
		testutil.Call(t, testHandler.CreateMediaJob, newRequest("POST", "/", body)).Want(400)
	}
	req := newRequest("POST", "/", protocol.MediaRequest{Capability: "image-generate", Provider: "auto", Prompt: "p", TaskID: uuid.NewString()})
	testutil.Call(t, testHandler.CreateMediaJob, req).Want(403)
	job := createTestMediaJob(t, testHandler, protocol.MediaRequest{Capability: "image-generate", Provider: "auto", Prompt: "p"})
	if _, err := testPool.Exec(t.Context(), "UPDATE media_job SET expires_at=$2 WHERE id=$1", job.ID, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	var expired protocol.MediaJob
	testutil.Call(t, testHandler.GetMediaJob, withURLParam(newRequest("GET", "/", nil), "id", job.ID)).Want(200).JSON(&expired)
	if expired.State != "failed" {
		t.Fatal("expired queue did not fail")
	}
}

func TestMediaCapabilitiesArePerRuntime(t *testing.T) {
	req := httptest.NewRequest("POST", "/", nil)
	req.Header.Set("X-Client-Capabilities", "rpc-v1,media-image-codex-v1,media-video-flow-v1")
	if got := runtimeMediaCapabilities(req, "claude", []string{"media-image-codex-v1"}); !slices.Equal(got, []string{"rpc-v1"}) {
		t.Fatalf("broadcast leaked: %v", got)
	}
	if got := runtimeMediaCapabilities(req, "codex", []string{"media-image-codex-v1", "media-video-flow-v1"}); !slices.Equal(got, []string{"rpc-v1", "media-image-codex-v1"}) {
		t.Fatalf("wrong capabilities: %v", got)
	}
}

func TestMediaRPCRejectsRuntimeOutsideConnection(t *testing.T) {
	runtimeID := mediaTestRuntime(t, "codex", 0)
	raw, _ := json.Marshal(protocol.MediaClaimRequest{RuntimeID: runtimeID})
	status, _, _ := testHandler.DaemonRPCHandler(t.Context(), daemonws.ClientIdentity{UserID: testUserID, RuntimeIDs: []string{uuid.NewString()}}, "media.claim", raw)
	if status != 404 {
		t.Fatal("connection claimed an unbound runtime")
	}
}

func TestMediaQueueDoesNotStarveOtherOperations(t *testing.T) {
	for i := 0; i < 51; i++ {
		createTestMediaJob(t, testHandler, protocol.MediaRequest{Provider: "agy", Capability: "image-generate", Prompt: "waiting for agy host"})
	}
	runtimeID := mediaTestRuntime(t, "codex", 0)
	job := createTestMediaJob(t, testHandler, protocol.MediaRequest{Provider: "codex", Capability: "image-generate", Prompt: "ready"})
	claim := claimTestMediaJob(t, testHandler, runtimeID)
	if claim == nil || claim.Job.ID != job.ID {
		t.Fatal("unavailable agy queue starved codex runtime")
	}
}

func TestMediaRegistrationPersistsOnlyInstanceCapabilities(t *testing.T) {
	daemonID := uuid.NewString()
	cooldown := time.Now().Add(time.Hour).Unix()
	raw, _ := json.Marshal(map[string]any{"media_cooldowns": map[string]int64{"media-image-codex-v1": cooldown}})
	codex := dbfx.Runtime(t, "media codex", testutil.Cols{"provider": "codex", "daemon_id": daemonID, "metadata": string(raw)})
	claude := dbfx.Runtime(t, "ordinary claude", testutil.Cols{"provider": "claude", "daemon_id": daemonID})
	req := newRequest("POST", "/api/daemon/register", map[string]any{
		"workspace_id": testWorkspaceID, "daemon_id": daemonID,
		"runtimes": []map[string]any{
			{"name": "media codex", "type": "codex", "status": "online", "media_capabilities": []string{"media-image-codex-v1"}},
			{"name": "ordinary claude", "type": "claude", "status": "online"},
		},
	})
	req.Header.Set("X-Client-Capabilities", "rpc-v1,media-image-codex-v1,media-video-flow-v1")
	testutil.Call(t, testHandler.DaemonRegister, req).Want(200)
	for _, id := range []string{codex, claude} {
		row := loadRuntime(t, id)
		var meta struct {
			Capabilities []string         `json:"capabilities"`
			Cooldowns    map[string]int64 `json:"media_cooldowns"`
		}
		if err := json.Unmarshal(row.Metadata, &meta); err != nil {
			t.Fatal(err)
		}
		if !slices.Contains(meta.Capabilities, "rpc-v1") || slices.Contains(meta.Capabilities, "media-video-flow-v1") {
			t.Fatal("transport/native capabilities mixed", meta)
		}
		if slices.Contains(meta.Capabilities, "media-image-codex-v1") != (id == codex) {
			t.Fatal("native capability broadcast", meta)
		}
		if id == codex && meta.Cooldowns["media-image-codex-v1"] != cooldown {
			t.Fatal("registration cleared server cooldown")
		}
	}
}
