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

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func mediaInputFixture(t *testing.T, h *Handler, extra testutil.Cols) string {
	t.Helper()
	data := []byte("declared media input")
	link, err := h.Storage.Upload(t.Context(), uuid.NewString(), data, "text/plain", "input.txt")
	if err != nil {
		t.Fatal(err)
	}
	cols := testutil.Cols{"workspace_id": testWorkspaceID, "uploader_type": "member", "uploader_id": testUserID, "filename": "input.txt", "content_type": "text/plain", "size_bytes": len(data), "url": link}
	for k, v := range extra {
		cols[k] = v
	}
	return dbfx.Insert(t, "attachment", cols)
}

func mediaTransferRequest(a *protocol.MediaAssignment, method string, data []byte, inputID string) *http.Request {
	r := withURLParam(httptest.NewRequest(method, "/?filename=output.png", bytes.NewReader(data)), "id", a.Job.ID)
	chi.RouteContext(r.Context()).URLParams.Add("attachmentID", inputID)
	r.Header.Set("Authorization", "Bearer "+a.Token)
	return r
}

func TestMediaRegistrationDropsRetiredSources(t *testing.T) {
	daemonID := uuid.NewString()
	tags := []string{"media-video-flow-v1", "media-image-whisk-v1", "media-image-agy-v1"}
	rt := dbfx.Runtime(t, "image host", testutil.Cols{"provider": "antigravity", "daemon_id": daemonID, "metadata": `{"capabilities":["media-video-flow-v1","media-image-whisk-v1"]}`})
	req := newRequest("POST", "/api/daemon/register", map[string]any{"workspace_id": testWorkspaceID, "daemon_id": daemonID, "runtimes": []map[string]any{{"name": "image host", "type": "antigravity", "status": "online", "media_capabilities": tags}}})
	req.Header.Set("X-Client-Capabilities", "rpc-v1,media-image-codex-v1")
	testutil.Call(t, testHandler.DaemonRegister, req).Want(200)
	var meta struct {
		Capabilities []string `json:"capabilities"`
	}
	if err := json.Unmarshal(loadRuntime(t, rt).Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(meta.Capabilities, []string{"rpc-v1", "media-image-agy-v1"}) {
		t.Fatal("retired or foreign capabilities persisted", meta)
	}
	for _, provider := range []string{"auto", "flow", "whisk"} {
		for _, capability := range []string{"video-generate", "image-remix"} {
			testutil.Call(t, testHandler.CreateMediaJob, newRequest("POST", "/", protocol.MediaRequest{Provider: provider, Capability: capability, Prompt: "unsupported"})).Want(400)
		}
	}
}

func TestMediaQuotaReceiptCoolsAssignedTagAfterRegistrationChanges(t *testing.T) {
	rt := mediaTestRuntime(t, "codex", 0)
	job := createTestMediaJob(t, testHandler, protocol.MediaRequest{Provider: "auto", Capability: "image-generate", Prompt: "image"})
	a := claimTestMediaJob(t, testHandler, rt)
	if a == nil || a.Job.ID != job.ID {
		t.Fatal("missing assignment")
	}
	dbfx.Exec(t, `UPDATE agent_runtime SET metadata=jsonb_set(metadata,'{capabilities}','[]') WHERE id=$1`, rt)
	var result protocol.MediaJob
	testutil.Call(t, testHandler.ReportMediaJob, newRequest("POST", "/", protocol.MediaResult{RuntimeID: rt, JobID: job.ID, Attempt: a.Attempt, RateLimited: true, RetryAfter: 60})).Want(200).JSON(&result)
	var meta struct {
		Cooldowns map[string]int64 `json:"media_cooldowns"`
	}
	if err := json.Unmarshal(loadRuntime(t, rt).Metadata, &meta); err != nil {
		t.Fatal(err)
	}
	if result.State != "queued" || meta.Cooldowns["media-image-codex-v1"] <= time.Now().Unix() || meta.Cooldowns["media-image-agy-v1"] != 0 {
		t.Fatal("lost assigned cooldown", meta, result)
	}
}

func TestMediaClaimSettlesLostAssignment(t *testing.T) {
	for _, uploaded := range []bool{false, true} {
		for _, next := range []bool{false, true} {
			name := "missing output"
			if uploaded {
				name = "persisted output"
			}
			if next {
				name += " with next job"
			}
			t.Run(name, func(t *testing.T) {
				h := *testHandler
				t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
				h.Storage = storage.NewLocalStorageFromEnv()
				rt := mediaTestRuntime(t, "codex", 0)
				job := createTestMediaJob(t, &h, protocol.MediaRequest{Provider: "codex", Capability: "image-generate", Prompt: "first"})
				a := claimTestMediaJob(t, &h, rt)
				if a == nil {
					t.Fatal("first assignment missing")
				}
				attachment := ""
				if uploaded {
					var att AttachmentResponse
					testutil.Call(t, h.UploadMediaOutput, mediaTransferRequest(a, "POST", append([]byte{137, 80, 78, 71, 13, 10, 26, 10}, make([]byte, 512)...), "")).Want(200).JSON(&att)
					attachment = att.ID
					t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM attachment WHERE id=$1", att.ID) })
				}
				var nextJob protocol.MediaJob
				if next {
					nextJob = createTestMediaJob(t, &h, protocol.MediaRequest{Provider: "codex", Capability: "image-generate", Prompt: "next"})
				}
				b := claimTestMediaJob(t, &h, rt)
				if next && (b == nil || b.Job.ID != nextJob.ID) || !next && b != nil {
					t.Fatal("lost assignment still occupies runtime")
				}
				var got protocol.MediaJob
				testutil.Call(t, h.GetMediaJob, withURLParam(newRequest("GET", "/", nil), "id", job.ID)).Want(200).JSON(&got)
				if uploaded {
					if got.State != "completed" || got.AttachmentID != attachment || got.Markdown == "" {
						t.Fatalf("lost receipt discarded durable output: %+v", got)
					}
				} else if got.State != "failed" || got.Error != "assignment lost" {
					t.Fatalf("lost dispatch not finalized: %+v", got)
				}
				testutil.Call(t, h.MediaAttemptStatus, mediaTransferRequest(a, "GET", nil, "")).Want(409)
				// A late receipt cannot revive or replace the settled state.
				var late protocol.MediaJob
				testutil.Call(t, h.ReportMediaJob, newRequest("POST", "/", protocol.MediaResult{RuntimeID: rt, JobID: job.ID, Attempt: a.Attempt, RateLimited: true})).Want(200).JSON(&late)
				if late.State != got.State {
					t.Fatal("late receipt changed settled job")
				}
			})
		}
	}
}

func TestMediaTransferGuards(t *testing.T) {
	h := *testHandler
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	h.Storage = storage.NewLocalStorageFromEnv()
	rt := mediaTestRuntime(t, "codex", 0)
	declared := mediaInputFixture(t, &h, nil)
	undeclared := mediaInputFixture(t, &h, nil)
	createTestMediaJob(t, &h, protocol.MediaRequest{Provider: "codex", Capability: "image-generate", Prompt: "p", Inputs: []protocol.MediaInput{{AttachmentID: declared}}})
	a := claimTestMediaJob(t, &h, rt)
	if a == nil {
		t.Fatal("no assignment")
	}
	testutil.Call(t, h.DownloadMediaInput, mediaTransferRequest(a, "GET", nil, declared)).Want(200)
	testutil.Call(t, h.DownloadMediaInput, mediaTransferRequest(a, "GET", nil, undeclared)).Want(404)
	testutil.Call(t, h.UploadMediaOutput, mediaTransferRequest(a, "POST", []byte("<!DOCTYPE html><html>not a media output</html>"), "")).Want(400)
	var attachments int
	dbfx.QueryRow(t, "SELECT count(*) FROM media_job WHERE id=$1 AND attachment_id IS NOT NULL", a.Job.ID).Scan(&attachments)
	if attachments != 0 {
		t.Fatal("non-media upload saved")
	}
}

func TestMediaRevocationStopsAllTransfers(t *testing.T) {
	for _, mode := range []string{"runtime sharing", "workspace membership"} {
		t.Run(mode, func(t *testing.T) {
			h := *testHandler
			t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
			h.Storage = storage.NewLocalStorageFromEnv()
			rt := mediaTestRuntime(t, "codex", 0)
			caller := dbfx.User(t, "media caller", uuid.NewString()+"@example.test")
			member := dbfx.Member(t, testWorkspaceID, caller, "member")
			dbfx.Exec(t, "UPDATE agent_runtime SET visibility='private',shared_with=ARRAY[$2::uuid] WHERE id=$1", rt, caller)
			input := mediaInputFixture(t, &h, nil)
			var job protocol.MediaJob
			testutil.Call(t, h.CreateMediaJob, newRequestAsUser(caller, "POST", "/", protocol.MediaRequest{Provider: "codex", Capability: "image-generate", Prompt: "p", Inputs: []protocol.MediaInput{{AttachmentID: input}}})).Want(202).JSON(&job)
			t.Cleanup(func() { testPool.Exec(context.Background(), "DELETE FROM media_job WHERE id=$1", job.ID) })
			a := claimTestMediaJob(t, &h, rt)
			if a == nil {
				t.Fatal("shared runtime not usable")
			}
			testutil.Call(t, h.MediaAttemptStatus, mediaTransferRequest(a, "GET", nil, "")).Want(204)
			if mode == "runtime sharing" {
				dbfx.Exec(t, "UPDATE agent_runtime SET shared_with='{}' WHERE id=$1", rt)
			} else {
				dbfx.Exec(t, "DELETE FROM member WHERE id=$1", member)
			}
			testutil.Call(t, h.MediaAttemptStatus, mediaTransferRequest(a, "GET", nil, "")).Want(403)
			testutil.Call(t, h.DownloadMediaInput, mediaTransferRequest(a, "GET", nil, input)).Want(403)
			testutil.Call(t, h.UploadMediaOutput, mediaTransferRequest(a, "POST", []byte{137, 80, 78, 71, 13, 10, 26, 10}, "")).Want(403)
		})
	}
}

func TestMediaRejectsOtherPrivateChatInput(t *testing.T) {
	h := *testHandler
	t.Setenv("LOCAL_UPLOAD_DIR", t.TempDir())
	h.Storage = storage.NewLocalStorageFromEnv()
	owner := dbfx.User(t, "other owner", uuid.NewString()+"@example.test")
	dbfx.Member(t, testWorkspaceID, owner, "member")
	agent := dbfx.Agent(t, "chat agent", mediaTestRuntime(t, "codex", 0))
	chat := dbfx.ChatSession(t, agent, testutil.Cols{"creator_id": owner})
	input := mediaInputFixture(t, &h, testutil.Cols{"chat_session_id": chat, "uploader_id": owner})
	testutil.Call(t, h.CreateMediaJob, newRequest("POST", "/", protocol.MediaRequest{Provider: "codex", Capability: "image-edit", Prompt: "p", Inputs: []protocol.MediaInput{{AttachmentID: input}}})).Want(403)
	var jobs int
	dbfx.QueryRow(t, "SELECT count(*) FROM media_job WHERE request->'inputs' @> $1::jsonb", `[{"attachment_id":"`+input+`"}]`).Scan(&jobs)
	if jobs != 0 {
		t.Fatal("private chat input accepted")
	}
}

func TestMediaQuotaRetryLimit(t *testing.T) {
	rt := mediaTestRuntime(t, "codex", 0)
	job := createTestMediaJob(t, testHandler, protocol.MediaRequest{Provider: "codex", Capability: "image-generate", Prompt: "p"})
	a := claimTestMediaJob(t, testHandler, rt)
	if a == nil {
		t.Fatal("no assignment")
	}
	dbfx.Exec(t, "UPDATE media_job SET attempts=32 WHERE id=$1", job.ID)
	var result protocol.MediaJob
	testutil.Call(t, testHandler.ReportMediaJob, newRequest("POST", "/", protocol.MediaResult{RuntimeID: rt, JobID: job.ID, Attempt: a.Attempt, RateLimited: true})).Want(200).JSON(&result)
	if result.State != "failed" || result.Error != "media quota retry limit exceeded" {
		t.Fatalf("quota limit ignored: %+v", result)
	}
}
