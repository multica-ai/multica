package qoderruntime

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/png"
	"io"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
	"github.com/multica-ai/multica/server/internal/testutil"
)

const workspaceID = "11111111-1111-4111-8111-111111111111"

func TestAPIRecoversBlackholedHTTP2Connection(t *testing.T) {
	started := make(chan struct{}, 1)
	var posts atomic.Int32
	server, connections := testutil.NewBlackholeHTTP2Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.ProtoMajor != 2 {
			t.Errorf("expected HTTP/2, got %s", r.Proto)
		}
		if r.Method == http.MethodPost {
			posts.Add(1)
			started <- struct{}{}
			<-r.Context().Done()
			return
		}
		_, _ = io.WriteString(w, `{}`)
	}))
	a, err := newAPI(server.URL, "test-token", "")
	if err != nil {
		t.Fatal(err)
	}
	testutil.TrustHTTP2Server(t, a.client, server)
	a.client.Timeout = 2 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	if err := a.call(ctx, http.MethodGet, "/warm", nil, nil); err != nil {
		t.Fatal(err)
	}
	conn := <-connections
	defer conn.Close()
	done := make(chan error, 1)
	go func() { done <- a.call(ctx, http.MethodPost, "/sessions", nil, nil) }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("session request did not reach server")
	}
	conn.Drop.Store(true)
	if err := <-done; err == nil {
		t.Fatal("interrupted request unexpectedly succeeded")
	}
	if err := a.call(ctx, http.MethodGet, "/recovered", nil, nil); err != nil {
		t.Fatalf("Qoder client reused the dead connection: %v", err)
	}
	select {
	case <-connections:
	default:
		t.Fatal("Qoder recovery did not open a new connection")
	}
	if count := posts.Load(); count != 1 {
		t.Fatalf("session request was replayed %d times", count)
	}
}

type harness struct {
	b                       *Bridge
	status                  string
	task                    *daemon.Task
	events                  []map[string]any
	creates, sends, cancels int
	lostSend                bool
	complete, failure       map[string]any
	messages                []map[string]any
	prompt                  string
	sessionAgent            string
	issue                   map[string]any
	issueStatus             int
	issueBody               string
	issuePosts              int
	issueGet                string
	downloads               map[string]string
	uploads                 map[string]string
	qoderFiles              map[string]qoderFile
	replyUploads            []replyUpload
	replyUploadStatus       []int
	rejectUploads           map[string]int
	sessionResources        []map[string]any
	sentContent             []map[string]any
	usage                   []map[string]any
	usageAfterFinish        bool
	sessionUsage            string
	usageStatus             int
	cancelAcks              int
	cancelAckStatus         int
	comments, evaluations   []map[string]any
}

func setup(t *testing.T) *harness {
	t.Helper()
	h := &harness{status: "dispatched"}
	h.task = &daemon.Task{ID: "task-1", RuntimeID: "runtime-1", WorkspaceID: workspaceID, IssueID: "issue-1", AuthToken: "mat_task", Agent: &daemon.AgentData{Instructions: "Run tests", RuntimeConfig: json.RawMessage(`{"qoder_agent_id":"agent_1"}`), Skills: []daemon.SkillData{{Name: "multica-platform"}}}}
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("X-Workspace-ID") != workspaceID {
			t.Error("missing workspace")
		}
		auth := "Bearer multica-secret"
		if strings.HasPrefix(r.URL.Path, "/api/issues") || strings.HasPrefix(r.URL.Path, "/api/attachments/") || r.URL.Path == "/api/upload-file" {
			auth = "Bearer mat_task"
		}
		if r.Header.Get("Authorization") != auth {
			t.Error("wrong credential scope")
		}
		if r.Header.Get("X-Client-Capabilities") != "" {
			t.Error("unsupported capabilities advertised")
		}
		switch {
		case strings.HasSuffix(r.URL.Path, "/tasks/claim"):
			json.NewEncoder(w).Encode(map[string]any{"task": h.task})
			h.task = nil
		case r.URL.Path == "/api/issues" && r.Method == http.MethodPost:
			h.issuePosts++
			json.NewDecoder(r.Body).Decode(&h.issue)
			if h.issueStatus != 0 {
				w.WriteHeader(h.issueStatus)
			}
			if h.issueBody != "" {
				io.WriteString(w, h.issueBody)
			} else {
				io.WriteString(w, `{"identifier":"MUL-7"}`)
			}
		case r.URL.Path == "/api/issues/issue-1/comments" && r.Method == http.MethodPost:
			var c map[string]any
			json.NewDecoder(r.Body).Decode(&c)
			h.comments = append(h.comments, c)
			io.WriteString(w, `{}`)
		case r.URL.Path == "/api/issues/issue-1/squad-evaluated" && r.Method == http.MethodPost:
			var e map[string]any
			json.NewDecoder(r.Body).Decode(&e)
			h.evaluations = append(h.evaluations, e)
			io.WriteString(w, `{}`)
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			if h.issueGet != "" {
				io.WriteString(w, h.issueGet)
			} else {
				io.WriteString(w, `{"title":"Fix bug","description":"Use repository context"}`)
			}
		case strings.HasPrefix(r.URL.Path, "/api/attachments/"):
			body, ok := h.downloads[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, body)
		case r.URL.Path == "/api/upload-file" && r.Method == http.MethodPost:
			if len(h.replyUploadStatus) > 0 {
				status := h.replyUploadStatus[0]
				h.replyUploadStatus = h.replyUploadStatus[1:]
				w.WriteHeader(status)
				io.WriteString(w, `{"error":"refused"}`)
				return
			}
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(file)
			h.replyUploads = append(h.replyUploads, replyUpload{header.Filename, r.FormValue("task_id"), string(data), h.complete != nil})
			io.WriteString(w, `{"id":"att-1"}`)
		case strings.HasSuffix(r.URL.Path, "/status"):
			json.NewEncoder(w).Encode(map[string]string{"status": h.status})
		case strings.HasSuffix(r.URL.Path, "/start"):
			h.status = "running"
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/complete"):
			json.NewDecoder(r.Body).Decode(&h.complete)
			h.status = "completed"
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			json.NewDecoder(r.Body).Decode(&h.failure)
			h.status = "failed"
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/messages"):
			var req struct {
				Messages []map[string]any `json:"messages"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			h.messages = append(h.messages, req.Messages...)
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/usage"):
			if h.usageStatus != 0 {
				w.WriteHeader(h.usageStatus)
				return
			}
			var req struct {
				Usage []map[string]any `json:"usage"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			h.usage = append(h.usage, req.Usage...)
			h.usageAfterFinish = h.usageAfterFinish || h.complete != nil || h.failure != nil
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			h.cancelAcks++
			if h.cancelAckStatus != 0 {
				w.WriteHeader(h.cancelAckStatus)
				return
			}
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/session"):
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected Multica request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.Close)
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer qoder-secret" && !strings.HasPrefix(r.URL.Path, "/blob/") {
			t.Error("wrong Qoder credential")
		}
		switch {
		case r.URL.Path == "/sessions" && r.Method == "POST":
			h.creates++
			var input struct {
				Agent     string           `json:"agent"`
				Resources []map[string]any `json:"resources"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			h.sessionAgent = input.Agent
			h.sessionResources = input.Resources
			io.WriteString(w, `{"id":"sess_1","status":"idle"}`)
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == "POST":
			h.sends++
			raw, _ := io.ReadAll(r.Body)
			var req struct {
				Events []event `json:"events"`
			}
			json.Unmarshal(raw, &req)
			var content struct {
				Events []struct {
					Content []map[string]any `json:"content"`
				} `json:"events"`
			}
			json.Unmarshal(raw, &content)
			h.sentContent = content.Events[0].Content
			h.prompt = req.Events[0].text()
			// Like Qoder, echo the full submitted content on send and in the event list.
			userEvent := map[string]any{"id": "evt_user", "type": "user.message", "content": h.sentContent}
			h.events = append(h.events, userEvent)
			if h.lostSend {
				http.Error(w, "unknown", 502)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{userEvent}})
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == "GET":
			if page := r.URL.Query().Get("page"); page != "" && !strings.HasPrefix(page, "next:") {
				t.Error("event ID used as page cursor")
			}
			json.NewEncoder(w).Encode(eventListPage(r, h.events))
		case r.URL.Path == "/files" && r.Method == "POST":
			file, header, err := r.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(file)
			if r.FormValue("name") != header.Filename {
				t.Error("upload name mismatch")
			}
			if status := h.rejectUploads[header.Filename]; status != 0 {
				w.WriteHeader(status)
				io.WriteString(w, `{"error":{"type":"invalid_request_error","message":"unsupported file"}}`)
				return
			}
			if h.uploads == nil {
				h.uploads = map[string]string{}
			}
			h.uploads[header.Filename] = string(data)
			fmt.Fprintf(w, `{"id":"file_%d","type":"file"}`, len(h.uploads))
		case strings.HasPrefix(r.URL.Path, "/files/") && r.Method == "GET":
			id, content := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/files/"), "/content")
			f, ok := h.qoderFiles[id]
			if !ok {
				http.NotFound(w, r)
				return
			}
			if !content {
				scope := f.scope
				if scope == "" {
					scope = "sess_1"
				}
				json.NewEncoder(w).Encode(map[string]any{"id": id, "downloadable": f.downloadable, "scope": map[string]string{"id": scope, "type": "session"}})
				return
			}
			if !f.downloadable {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"url": "http://" + r.Host + "/blob/" + id, "expires_at": "2026-11-04T00:00:00Z"})
		case strings.HasPrefix(r.URL.Path, "/blob/"):
			// A presigned URL carries no credential.
			if r.Header.Get("Authorization") != "" {
				t.Error("credential sent to presigned download")
			}
			io.WriteString(w, h.qoderFiles[strings.TrimPrefix(r.URL.Path, "/blob/")].data)
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			h.cancels++
			io.WriteString(w, `{"status":"canceling"}`)
		case r.URL.Path == "/sessions/sess_1":
			if h.sessionUsage != "" {
				io.WriteString(w, `{"status":"idle","usage":`+h.sessionUsage+`}`)
				return
			}
			io.WriteString(w, `{"status":"idle"}`)
		default:
			t.Errorf("unexpected Qoder request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(q.Close)
	var err error
	h.b, err = New(Config{MulticaURL: m.URL, MulticaToken: "multica-secret", WorkspaceID: workspaceID, QoderURL: q.URL, QoderToken: "qoder-secret", EnvironmentID: "env_1", StateDir: t.TempDir()}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = &journal{Binding: "test", RuntimeID: "runtime-1"}
	return h
}
func (h *harness) finish(reason string) {
	h.events = append(h.events, map[string]any{"id": "evt_reply", "type": "agent.message", "content": []map[string]string{{"type": "text", "text": "Tests passed. PR: https://example.com/pr/1"}}}, map[string]any{"id": "evt_idle", "type": "session.status_idle", "stop_reason": map[string]string{"type": reason}})
}
func TestIssueRun(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	h.events = []map[string]any{{"id": "evt_initial", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}}}
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete != nil {
		t.Fatal("completed before user turn")
	}
	if !strings.Contains(h.prompt, "Fix bug") || strings.Contains(h.prompt, "secret") || strings.Contains(h.prompt, "mat_task") {
		t.Fatalf("incorrect prompt: %s", h.prompt)
	}
	h.finish("end_turn")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete["session_id"] != "sess_1" || !strings.Contains(fmt.Sprint(h.complete["output"]), "Tests passed") {
		t.Fatalf("result: %#v", h.complete)
	}
	if h.creates != 1 || h.sends != 1 || len(h.messages) != 1 || h.b.state.first() != nil {
		t.Fatal("incorrect lifecycle")
	}
	data, err := os.ReadFile(h.b.path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "secret") {
		t.Fatal("credential persisted")
	}
}
func quickCreateTask(h *harness) {
	h.task.IssueID = ""
	h.task.QuickCreatePrompt = "fix the slow inbox"
	h.task.QuickCreateDueDate = "2026-11-01"
	h.task.ProjectID = "project-1"
	h.task.Agent.ID = "agent-uuid"
}
func (h *harness) draft(text string) {
	h.events = append(h.events, map[string]any{"id": "evt_reply", "type": "agent.message", "content": []map[string]string{{"type": "text", "text": text}}}, map[string]any{"id": "evt_idle", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}})
}
func TestQuickCreateRunCreatesIssue(t *testing.T) {
	h := setup(t)
	quickCreateTask(h)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.failure != nil || !strings.Contains(h.prompt, "fix the slow inbox") {
		t.Fatalf("incorrect quick-create prompt: %s (failure %#v)", h.prompt, h.failure)
	}
	h.draft("```json\n{\"title\":\"Speed up inbox\",\"description\":\"Inbox loads slowly\",\"priority\":\"high\"}\n```")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"title": "Speed up inbox", "description": "Inbox loads slowly", "priority": "high", "due_date": "2026-11-01", "project_id": "project-1", "assignee_type": "agent", "assignee_id": "agent-uuid", "origin_type": "quick_create", "origin_id": "task-1"}
	for k, v := range want {
		if h.issue[k] != v {
			t.Fatalf("issue %s = %v, want %v (%#v)", k, h.issue[k], v, h.issue)
		}
	}
	if !strings.Contains(fmt.Sprint(h.complete["output"]), "MUL-7") {
		t.Fatalf("result: %#v", h.complete)
	}
}
func TestQuickCreateWithoutDraftFails(t *testing.T) {
	h := setup(t)
	quickCreateTask(h)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.draft("I could not do it")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.issue != nil || h.complete != nil || !strings.Contains(fmt.Sprint(h.failure["error"]), "issue draft") {
		t.Fatalf("unexpected report: %#v", h.failure)
	}
}
func TestQuickCreateCredentialIsNotPersisted(t *testing.T) {
	h := setup(t)
	quickCreateTask(h)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	h.draft(`{"title":"Speed up inbox"}`)
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.issue != nil || !strings.Contains(fmt.Sprint(h.failure["error"]), "restarted") {
		t.Fatalf("unexpected report: %#v", h.failure)
	}
}
func TestQuickCreateAttachmentsFailBeforeExecution(t *testing.T) {
	h := setup(t)
	quickCreateTask(h)
	h.task.QuickCreateAttachmentIDs = []string{"attachment-1"}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || h.issuePosts != 0 || !strings.Contains(fmt.Sprint(h.failure["error"]), "attachments") {
		t.Fatalf("quick-create attachment ignored: creates=%d posts=%d failure=%#v", h.creates, h.issuePosts, h.failure)
	}
}
func testPNG(t *testing.T, width, height int, noise bool) string {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	if noise {
		rand.New(rand.NewSource(1)).Read(img.Pix)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func (h *harness) attach(t *testing.T, files ...[3]string) {
	t.Helper()
	var atts []map[string]any
	h.downloads = map[string]string{}
	for i, f := range files {
		u := fmt.Sprintf("/api/attachments/a%d/download", i)
		atts = append(atts, map[string]any{"id": fmt.Sprint("a", i), "filename": f[0], "content_type": f[1], "size_bytes": len(f[2]), "download_url": u})
		h.downloads[u] = f[2]
	}
	raw, _ := json.Marshal(map[string]any{"title": "Fix bug", "description": "See attachments", "attachments": atts})
	h.issueGet = string(raw)
}

func TestIssueAttachmentsReachSession(t *testing.T) {
	h := setup(t)
	screen := testPNG(t, 4, 4, false)
	h.attach(t,
		[3]string{"notes.md", "text/markdown", "hello"},
		[3]string{"screen.png", "image/png", screen},
		[3]string{"spec.pdf", "application/pdf", "%PDF"},
		[3]string{"notes.md", "text/plain", "bye"},
		[3]string{"page.png", "image/png", testPNG(t, 400, 9000, false)},
		[3]string{"broken.png", "image/png", "\x89PNG"})
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.failure != nil || h.sends != 1 {
		t.Fatalf("run failed: %#v", h.failure)
	}
	if h.uploads["notes.md"] != "hello" || h.uploads["spec.pdf"] != "%PDF" || h.uploads["notes-2.md"] != "bye" || len(h.uploads) != 3 {
		t.Fatalf("uploads: %#v", h.uploads)
	}
	want := []string{"file_1 /data/attachments/notes.md", "file_2 /data/attachments/spec.pdf", "file_3 /data/attachments/notes-2.md"}
	var got []string
	for _, res := range h.sessionResources {
		if res["type"] == "file" {
			got = append(got, fmt.Sprint(res["file_id"], " ", res["mount_path"]))
		}
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("mounted resources: %v", h.sessionResources)
	}
	if len(h.sentContent) != 2 || h.sentContent[1]["type"] != "image" {
		t.Fatalf("message content: %#v", h.sentContent)
	}
	source, _ := h.sentContent[1]["source"].(map[string]any)
	if source["type"] != "base64" || source["media_type"] != "image/png" || source["data"] != base64.StdEncoding.EncodeToString([]byte(screen)) {
		t.Fatalf("image source: %#v", source)
	}
	for _, s := range []string{"/data/attachments/notes.md (mounted file)", "screen.png (image attached", "/data/attachments/spec.pdf (mounted file)", "page.png (image larger than 8000 px per side): not available", "broken.png (unreadable image): not available"} {
		if !strings.Contains(h.prompt, s) {
			t.Fatalf("prompt missing %q: %s", s, h.prompt)
		}
	}
	if r := h.b.state.first(); r == nil || len(r.Images) != 0 {
		t.Fatal("sent images kept in the journal")
	}
	h.finish("end_turn")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil {
		t.Fatalf("run did not complete: %#v", h.failure)
	}
}

// The submitted images come back in the send response and the event list;
// both must stay readable for the run to be confirmed and completed.
func TestIssueImagesFitEchoedEvents(t *testing.T) {
	h := setup(t)
	img := testPNG(t, 700, 700, true)
	h.attach(t, [3]string{"a.png", "image/png", img}, [3]string{"b.png", "image/png", img}, [3]string{"c.png", "image/png", img})
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if r := h.b.state.first(); r == nil || r.Phase != "running" {
		t.Fatalf("send not confirmed: %#v", r)
	}
	if len(h.sentContent) != 3 || !strings.Contains(h.prompt, "c.png (image too large): not available") {
		t.Fatalf("image budget not applied: %d blocks, prompt %s", len(h.sentContent), h.prompt)
	}
	h.finish("end_turn")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil {
		t.Fatalf("run did not complete: %#v", h.failure)
	}
}

func TestWebPImageSize(t *testing.T) {
	header := func(chunk string, body ...byte) []byte {
		b := append([]byte("RIFF\x00\x00\x00\x00WEBP"+chunk+"\x00\x00\x00\x00"), body...)
		return append(b, make([]byte, 32)...)
	}
	cases := map[string][]byte{
		"VP8":  header("VP8 ", 0, 0, 0, 0x9d, 0x01, 0x2a, 0x90, 0x01, 0x28, 0x23), // 400x9000
		"VP8L": header("VP8L", 0x2f, 0x8f, 0xc1, 0xc9, 0x08),                      // 400x9000
		"VP8X": header("VP8X", 0, 0, 0, 0, 0x8f, 0x01, 0x00, 0x27, 0x23, 0x00),    // 400x9000
	}
	for name, data := range cases {
		w, hgt, ok := imageSize("image/webp", data)
		if !ok || w != 400 || hgt != 9000 {
			t.Errorf("%s: got %dx%d ok=%v", name, w, hgt, ok)
		}
	}
	if _, _, ok := imageSize("image/webp", []byte("RIFF")); ok {
		t.Error("truncated WebP accepted")
	}
}

// lostCreate leaves a quick-create run whose first create request failed
// after it was marked submitted, as when the response is lost.
func lostCreate(t *testing.T, h *harness) {
	t.Helper()
	quickCreateTask(h)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.issueStatus = http.StatusBadGateway
	h.draft(`{"title":"Speed up inbox"}`)
	h.b.step(ctx)
	if h.issuePosts != 1 || h.complete != nil || h.failure != nil {
		t.Fatalf("first create should be retried: posts=%d complete=%#v failure=%#v", h.issuePosts, h.complete, h.failure)
	}
}
func TestQuickCreateRetryAcceptsOwnDuplicate(t *testing.T) {
	h := setup(t)
	lostCreate(t, h)
	h.issueStatus = http.StatusConflict
	h.issueBody = `{"code":"active_duplicate_issue","error":"duplicate"}`
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failure != nil || !strings.Contains(fmt.Sprint(h.complete["output"]), "Speed up inbox") {
		t.Fatalf("retry should complete: complete=%#v failure=%#v", h.complete, h.failure)
	}
}
func TestQuickCreateRetryRejectsOtherConflict(t *testing.T) {
	h := setup(t)
	lostCreate(t, h)
	h.issueStatus = http.StatusConflict
	h.issueBody = `{"error":"the target status was archived"}`
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete != nil || !strings.Contains(fmt.Sprint(h.failure["error"]), "rejected") {
		t.Fatalf("non-duplicate conflict should fail: complete=%#v failure=%#v", h.complete, h.failure)
	}
}
func TestQuickCreateRestartAfterSubmitAsksToCheck(t *testing.T) {
	h := setup(t)
	lostCreate(t, h)
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	h.issueStatus = 0
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.issuePosts != 1 || !strings.Contains(fmt.Sprint(h.failure["error"]), "check whether it was created") {
		t.Fatalf("unexpected report: posts=%d failure=%#v", h.issuePosts, h.failure)
	}
}
func TestChatRun(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	h.task.IssueID = ""
	h.task.ChatSessionID = "chat-1"
	h.task.ChatMessage = "Introduce yourself"
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.failure != nil || !strings.Contains(h.prompt, "Introduce yourself") || !strings.Contains(h.prompt, "Audience: direct room.") || strings.Contains(h.prompt, "pull request") {
		t.Fatalf("incorrect chat prompt: %s (failure %#v)", h.prompt, h.failure)
	}
	h.finish("end_turn")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete["session_id"] != "sess_1" || !strings.Contains(fmt.Sprint(h.complete["output"]), "Tests passed") {
		t.Fatalf("result: %#v", h.complete)
	}
}
func leaderRun(t *testing.T, reply string) *harness {
	t.Helper()
	h := setup(t)
	h.task.IsLeaderTask = true
	h.task.TriggerCommentID = "comment-1"
	h.task.Agent.Instructions = "## Squad Operating Protocol\nDelegate."
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.failure != nil || !strings.Contains(h.prompt, "Squad Operating Protocol") || !strings.Contains(h.prompt, "OUTCOME:") || strings.Contains(h.prompt, "pull request whose title") {
		t.Fatalf("incorrect leader prompt: %s (failure %#v)", h.prompt, h.failure)
	}
	h.draft(reply)
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	return h
}
func TestSquadLeaderRunDelegates(t *testing.T) {
	h := leaderRun(t, "OUTCOME: action\nREASON: Bob owns the API\n[@Bob](mention://agent/bob) please fix the handler.")
	if len(h.comments) != 1 || h.comments[0]["content"] != "[@Bob](mention://agent/bob) please fix the handler." || h.comments[0]["parent_id"] != "comment-1" {
		t.Fatalf("comments: %#v", h.comments)
	}
	if len(h.evaluations) != 1 || h.evaluations[0]["outcome"] != "action" || h.evaluations[0]["reason"] != "Bob owns the API" {
		t.Fatalf("evaluations: %#v", h.evaluations)
	}
	if h.complete == nil || h.failure != nil {
		t.Fatalf("result: %#v %#v", h.complete, h.failure)
	}
}
func TestSquadLeaderNoActionPostsNoComment(t *testing.T) {
	h := leaderRun(t, "**OUTCOME:** no_action\n**REASON:** progress update only\nNothing to do.")
	if len(h.comments) != 0 || len(h.evaluations) != 1 || h.evaluations[0]["outcome"] != "no_action" || h.evaluations[0]["reason"] != "progress update only" {
		t.Fatalf("comments %#v evaluations %#v", h.comments, h.evaluations)
	}
	if h.complete == nil {
		t.Fatalf("not completed: %#v", h.failure)
	}
}
func TestSquadLeaderUnformattedReplyStillDelegates(t *testing.T) {
	h := leaderRun(t, "[@Bob](mention://agent/bob) take this one.")
	if len(h.comments) != 1 || len(h.evaluations) != 1 || h.evaluations[0]["outcome"] != "action" {
		t.Fatalf("comments %#v evaluations %#v", h.comments, h.evaluations)
	}
}
func TestSquadLeaderWithoutIssueFails(t *testing.T) {
	h := setup(t)
	h.task.IsLeaderTask = true
	h.task.IssueID = ""
	h.task.ChatSessionID = "chat-1"
	h.task.ChatMessage = "hi"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failure == nil || h.creates != 0 {
		t.Fatalf("expected failure before execution: %#v", h.failure)
	}
}
func TestChatAudience(t *testing.T) {
	for _, tc := range []struct{ channel, chatType, want string }{
		{"", "", "Audience: direct room."},
		{"slack", "p2p", "Audience: direct room."},
		{"slack", "group", "Audience: group room; not private"},
		{"feishu", "", "Audience: unknown."},
	} {
		if got := chatAudience(tc.channel, tc.chatType); !strings.Contains(got, tc.want) {
			t.Errorf("chatAudience(%q, %q) = %q, want %q", tc.channel, tc.chatType, got, tc.want)
		}
	}
}

// chatAttach puts files on the chat message the way the claim does: IDs and
// names only, with metadata and content behind /api/attachments. A file with
// no content was deleted before the run read it.
func chatAttach(downloads map[string]string, task *daemon.Task, files ...[3]string) {
	for i, f := range files {
		id := fmt.Sprint("c", i)
		task.ChatMessageAttachments = append(task.ChatMessageAttachments, daemon.ChatAttachmentMeta{ID: id, Filename: f[0], ContentType: f[1]})
		if f[2] == "" {
			continue
		}
		u := "/api/attachments/" + id + "/download"
		meta, _ := json.Marshal(map[string]any{"id": id, "filename": f[0], "content_type": f[1], "size_bytes": len(f[2]), "download_url": u})
		downloads["/api/attachments/"+id] = string(meta)
		downloads[u] = f[2]
	}
}

func TestChatAttachmentsReachSession(t *testing.T) {
	h := setup(t)
	h.task.IssueID = ""
	h.task.ChatSessionID = "chat-1"
	h.task.ChatMessage = "What is in these?"
	screen := testPNG(t, 4, 4, false)
	h.downloads = map[string]string{}
	chatAttach(h.downloads, h.task,
		[3]string{"notes.md", "text/markdown", "hello"},
		[3]string{"screen.png", "image/png", screen},
		[3]string{"report.pdf", "application/pdf", "%PDF"},
		[3]string{"setup.exe", "application/octet-stream", "MZ"},
		[3]string{"old.txt", "text/plain", ""})
	h.rejectUploads = map[string]int{"setup.exe": 400}
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.failure != nil || h.sends != 1 {
		t.Fatalf("run failed: %#v", h.failure)
	}
	if h.uploads["notes.md"] != "hello" || h.uploads["report.pdf"] != "%PDF" || len(h.uploads) != 2 {
		t.Fatalf("uploads: %#v", h.uploads)
	}
	if len(h.sessionResources) != 2 || h.sessionResources[0]["mount_path"] != "/data/attachments/task-1/notes.md" || h.sessionResources[1]["mount_path"] != "/data/attachments/task-1/report.pdf" {
		t.Fatalf("mounted resources: %v", h.sessionResources)
	}
	if len(h.sentContent) != 2 || h.sentContent[1]["type"] != "image" {
		t.Fatalf("message content: %#v", h.sentContent)
	}
	for _, s := range []string{"What is in these?", "Message attachments:", "/data/attachments/task-1/notes.md (mounted file)", "screen.png (image attached", "/data/attachments/task-1/report.pdf (mounted file)", "setup.exe (rejected by Qoder Files): not available", "old.txt (deleted): not available"} {
		if !strings.Contains(h.prompt, s) {
			t.Fatalf("prompt missing %q: %s", s, h.prompt)
		}
	}
	h.finish("end_turn")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil {
		t.Fatalf("run did not complete: %#v", h.failure)
	}
}

func TestAmbiguousSubmitReconcilesAfterRestart(t *testing.T) {
	h := setup(t)
	h.lostSend = true
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("expected ambiguous failure")
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	if j.first().Phase != "sending" || j.first().SessionID != "sess_1" {
		t.Fatal("missing intent")
	}
	h.finish("end_turn")
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.sends != 1 || h.creates != 1 || h.complete == nil {
		t.Fatal("restart duplicated/lost execution")
	}
}
func TestUnconfirmedSubmissionDoesNotResend(t *testing.T) {
	h := setup(t)
	h.lostSend = true
	_ = h.b.step(context.Background())
	h.events = nil
	for range 2 {
		if err := h.b.step(context.Background()); err == nil {
			t.Fatal("expected reconciliation warning")
		}
	}
	if h.sends != 1 {
		t.Fatal("duplicated submission")
	}
}
func TestCancelStopsRemote(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.status = "cancelled"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.cancels != 1 || h.b.state.first() != nil || h.complete != nil {
		t.Fatal("incorrect cancellation")
	}
}
func TestRequiresActionIsNotSuccess(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.finish("requires_action")
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete != nil || !strings.Contains(fmt.Sprint(h.failure["error"]), "requires_action") {
		t.Fatalf("unexpected report: %#v", h.failure)
	}
}
func TestLocalDirectoryFailsBeforeExecution(t *testing.T) {
	h := setup(t)
	h.task.ProjectResources = []daemon.ProjectResourceData{{ResourceType: "local_directory"}}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || h.failure == nil {
		t.Fatal("local directory ignored")
	}
}
func TestStateLockAndBinding(t *testing.T) {
	path := filepath.Join(t.TempDir(), "lock")
	f, err := lockState(path)
	if err != nil {
		t.Fatal(err)
	}
	if second, err := lockState(path); err == nil {
		second.Close()
		t.Fatal("second process acquired lock")
	}
	f.Close()
	f, err = lockState(path)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	path = filepath.Join(t.TempDir(), "state.json")
	if err := save(path, &journal{Binding: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := readJournal(path, "b"); err == nil {
		t.Fatal("wrong binding accepted")
	}
}
func TestAPIRejectsRedirectsAndRedactsErrors(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("followed redirect") }))
	defer target.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", target.URL)
		w.WriteHeader(302)
		io.WriteString(w, "sensitive-secret")
	}))
	defer source.Close()
	a, err := newAPI(source.URL, "credential", "")
	if err != nil {
		t.Fatal(err)
	}
	err = a.call(context.Background(), http.MethodGet, "/", nil, nil)
	if err == nil || strings.Contains(err.Error(), "sensitive-secret") {
		t.Fatalf("unsafe error: %v", err)
	}
	if _, err := newAPI("http://remote.example", "token", ""); err == nil {
		t.Fatal("plaintext remote URL accepted")
	}
}

func TestOpaquePaginationWithEmptyProjectedPage(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	requests := 0
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions/sess_1" {
			io.WriteString(w, `{"status":"idle"}`)
			return
		}
		requests++
		if requests == 1 {
			if r.URL.Query().Get("after_id") != "evt_user" {
				t.Error("missing event cursor")
			}
			io.WriteString(w, `{"data":[],"has_more":true,"next_page":"opaque-cursor"}`)
		} else {
			if r.URL.Query().Get("page") != "opaque-cursor" || r.URL.Query().Get("after_id") != "" {
				t.Error("incorrect page request")
			}
			io.WriteString(w, `{"data":[{"id":"evt_done","type":"session.status_idle","stop_reason":{"type":"end_turn"}}],"has_more":false}`)
		}
	}))
	defer q.Close()
	h.b.qoder.base = q.URL
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil || requests != 2 {
		t.Fatal("pagination lost terminal event")
	}
}

func TestTerminalReportSurvivesRestart(t *testing.T) {
	h := setup(t)
	h.b.state.Runs = []*run{&run{TaskID: "task-1", SessionID: "sess_1", Phase: "final", Outcome: "completed", Output: "Recovered result"}}
	if err := h.b.persist(); err != nil {
		t.Fatal(err)
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = j
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.complete["output"] != "Recovered result" || h.creates != 0 || h.sends != 0 {
		t.Fatal("terminal recovery re-executed task")
	}
}

func TestRejectedMessageFailsRun(t *testing.T) {
	h := setup(t)
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions" {
			io.WriteString(w, `{"id":"sess_1"}`)
			return
		}
		http.Error(w, "bad message", 400)
	}))
	defer q.Close()
	h.b.qoder.base = q.URL
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failure == nil || h.b.state.first() != nil {
		t.Fatal("definite rejection left run pending")
	}
}

func TestRegistrationAndShutdown(t *testing.T) {
	h := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces/" + workspaceID:
			json.NewEncoder(w).Encode(map[string]string{"id": workspaceID})
		case "/api/daemon/register":
			var req struct {
				Runtimes []struct {
					Type string `json:"type"`
					Mode string `json:"runtime_mode"`
				} `json:"runtimes"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			if len(req.Runtimes) != 1 || req.Runtimes[0].Type != Provider || req.Runtimes[0].Mode != "cloud" {
				t.Error("wrong registration provider")
			}
			io.WriteString(w, `{"runtimes":[{"id":"runtime-1","provider":"qoder_cloud"}]}`)
		case "/api/daemon/heartbeat":
			io.WriteString(w, `{}`)
			cancel()
		default:
			t.Errorf("unexpected startup request %s", r.URL)
		}
	}))
	defer m.Close()
	h.b.multica.base = m.URL
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/agents/agent_1" && r.URL.Path != "/environments/env_1" {
			t.Error("unexpected remote validation")
		}
		id := strings.TrimPrefix(strings.TrimPrefix(r.URL.Path, "/agents/"), "/environments/")
		json.NewEncoder(w).Encode(map[string]string{"id": id})
	}))
	defer q.Close()
	h.b.qoder.base = q.URL
	err := h.b.Run(ctx)
	if err != nil && ctx.Err() == nil {
		t.Fatal(err)
	}
}

func TestPerAgentSessionBinding(t *testing.T) {
	for _, id := range []string{"agent_first", "agent_second"} {
		t.Run(id, func(t *testing.T) {
			h := setup(t)
			h.task.Agent.RuntimeConfig = json.RawMessage(fmt.Sprintf(`{"qoder_agent_id":%q}`, id))
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			if h.sessionAgent != id {
				t.Fatalf("session agent = %q, want %q", h.sessionAgent, id)
			}
			restored, err := readJournal(h.b.path, "test")
			if err != nil || restored.first().AgentID != id {
				t.Fatal("per-run agent binding not persisted")
			}
		})
	}
}
func TestMissingAgentBindingFailsBeforeSession(t *testing.T) {
	for _, raw := range []string{`{}`, `{"qoder_agent_id":"../agents"}`, `{"qoder_agent_id":1}`} {
		h := setup(t)
		h.task.Agent.RuntimeConfig = json.RawMessage(raw)
		if err := h.b.step(context.Background()); err != nil {
			t.Fatal(err)
		}
		if h.creates != 0 || h.status != "failed" {
			t.Fatal("unbound task created a QCA session")
		}
	}
}

func TestRunDeadlineReleasesQueue(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.b.state.first().Deadline = time.Now().Add(-time.Second)
	h.sessionUsage = `{"total_credits":0.4}`
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failure == nil || h.b.state.first() != nil || h.cancels != 1 {
		t.Fatal("expired run did not cancel and finish")
	}
	h.wantCredits(t, 400000)
}

func TestAmbiguousCreationIsNotRepeated(t *testing.T) {
	h := setup(t)
	h.b.state.Runs = []*run{&run{TaskID: "task-1", Phase: "creating", Deadline: time.Now().Add(time.Hour)}}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || h.failure == nil {
		t.Fatal("ambiguous creation was repeated")
	}
}

func TestTimeoutKeepsFailedCancellationForRecovery(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer remote.Close()
	h.b.qoder.base = remote.URL
	h.b.state.first().Deadline = time.Now().Add(-time.Second)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.b.state.first() != nil || len(h.b.state.Cleanup) != 1 || h.failure == nil {
		t.Fatal("failed cancellation blocked queue or lost session")
	}
	j, err := readJournal(h.b.path, "test")
	if err != nil {
		t.Fatal(err)
	}
	if len(j.Cleanup) != 1 || j.Cleanup[0] != "sess_1" {
		t.Fatal("cleanup not durable")
	}
}

func TestDeletedRemoteSessionFailsRun(t *testing.T) {
	h := setup(t)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	remote := httptest.NewServer(http.NotFoundHandler())
	defer remote.Close()
	h.b.qoder.base = remote.URL
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failure == nil || h.b.state.first() != nil {
		t.Fatal("deleted session not terminal")
	}
}

func TestRateLimitedCreationRetriesAfterDelay(t *testing.T) {
	h := setup(t)
	attempts := 0
	remote := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { attempts++; w.WriteHeader(http.StatusTooManyRequests) }))
	defer remote.Close()
	h.b.qoder.base = remote.URL
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.b.state.first() == nil || h.b.state.first().Phase != "prepared" || h.failure != nil {
		t.Fatal("rate limit became permanent failure")
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if attempts != 1 {
		t.Fatal("rate limit retry ignored backoff")
	}
}

func TestSubmissionDeadlineReleasesUnconfirmedRun(t *testing.T) {
	h := setup(t)
	h.lostSend = true
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("expected lost response")
	}
	h.events = nil
	h.b.state.first().SubmissionDeadline = time.Now().Add(-time.Second)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failure == nil || h.b.state.first() != nil || h.sends != 1 {
		t.Fatal("unconfirmed submission blocked queue or resent")
	}
}

func TestManagedJournalPersistsWithoutLocalFiles(t *testing.T) {
	h := setup(t)
	var saved []byte
	h.b.cfg.SaveState = func(_ context.Context, raw []byte) error { saved = append([]byte(nil), raw...); return nil }
	h.b.path = "/not/a/writable/path"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	restored, err := decodeJournal(saved, "test")
	if err != nil {
		t.Fatal(err)
	}
	if restored.first() == nil || restored.first().SessionID != "sess_1" || restored.first().Deadline.IsZero() {
		t.Fatal("managed recovery lost session or deadline")
	}
}

func TestToolResultsPreserveCallIdentity(t *testing.T) {
	for _, prefix := range []string{"agent.", "agent.mcp_"} {
		t.Run(prefix, func(t *testing.T) {
			h := setup(t)
			ctx := context.Background()
			if err := h.b.step(ctx); err != nil {
				t.Fatal(err)
			}
			h.events = append(h.events,
				map[string]any{"id": "call-a", "type": prefix + "tool_use", "name": "Read"},
				map[string]any{"id": "call-b", "type": prefix + "tool_use", "name": "Read"},
				map[string]any{"id": "result-b", "type": prefix + "tool_result", "tool_use_id": "call-b"},
				map[string]any{"id": "result-a", "type": prefix + "tool_result", "tool_use_id": "call-a"},
			)
			if err := h.b.step(ctx); err != nil {
				t.Fatal(err)
			}
			if len(h.messages) != 4 {
				t.Fatalf("got %d messages", len(h.messages))
			}
			for i, want := range []string{"call-a", "call-b", "call-b", "call-a"} {
				if h.messages[i]["call_id"] != want {
					t.Errorf("message %d: call_id = %v, want %s", i, h.messages[i]["call_id"], want)
				}
			}
		})
	}
}

// first returns the oldest active run, which single-run tests inspect.
func (j *journal) first() *run {
	if len(j.Runs) == 0 {
		return nil
	}
	return j.Runs[0]
}

func TestJournalStorageConfig(t *testing.T) {
	base := Config{MulticaURL: "http://127.0.0.1:1", MulticaToken: "m", WorkspaceID: workspaceID, QoderURL: "http://127.0.0.1:2", QoderToken: "q", EnvironmentID: "env_1"}
	load := func(context.Context) ([]byte, error) { return nil, nil }
	save := func(context.Context, []byte) error { return nil }

	external := base
	external.LoadState, external.SaveState = load, save
	if _, err := New(external, nil); err != nil {
		t.Fatalf("external journal needs no state directory: %v", err)
	}
	onDisk := base
	onDisk.StateDir = t.TempDir()
	if _, err := New(onDisk, nil); err != nil {
		t.Fatalf("on-disk journal rejected: %v", err)
	}
	for name, cfg := range map[string]Config{
		"no journal":     base,
		"load only":      func() Config { c := base; c.LoadState = load; c.StateDir = t.TempDir(); return c }(),
		"save only":      func() Config { c := base; c.SaveState = save; c.StateDir = t.TempDir(); return c }(),
		"no environment": func() Config { c := external; c.EnvironmentID = ""; return c }(),
	} {
		if _, err := New(cfg, nil); err == nil {
			t.Errorf("%s accepted", name)
		}
	}
}

// eventListPage serves GET /sessions/{id}/events from the full event list,
// honouring after_id, order and limit, and the opaque "next:<id>" page cursor
// it hands out itself.
func eventListPage(r *http.Request, events []map[string]any) map[string]any {
	q := r.URL.Query()
	after := q.Get("after_id")
	if page := q.Get("page"); page != "" {
		after = strings.TrimPrefix(page, "next:")
	}
	if after != "" {
		for i, e := range events {
			if e["id"] == after {
				events = events[i+1:]
				break
			}
		}
	}
	if q.Get("order") == "desc" {
		reversed := make([]map[string]any, 0, len(events))
		for i := len(events) - 1; i >= 0; i-- {
			reversed = append(reversed, events[i])
		}
		events = reversed
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 20
	}
	response := map[string]any{"data": events, "has_more": false}
	if len(events) > limit {
		response["data"] = events[:limit]
		response["has_more"] = true
		response["next_page"] = fmt.Sprint("next:", events[limit-1]["id"])
	}
	return response
}

// Each event fits the response limit, but a full page of them does not; the
// bridge must fall back to smaller pages instead of failing the run.
func TestOversizedEventPageIsReadInSmallerPages(t *testing.T) {
	h := setup(t)
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("x", 5<<20)
	for i := 0; i < 2; i++ {
		h.events = append(h.events, map[string]any{"id": fmt.Sprint("evt_tool_", i), "type": "agent.tool_result", "content": []map[string]string{{"type": "text", "text": big}}})
	}
	h.finish("end_turn")
	for i := 0; i < 5 && h.complete == nil; i++ {
		if err := h.b.step(ctx); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	if h.complete == nil || h.failure != nil {
		t.Fatalf("run did not complete: %#v", h.failure)
	}
}

// A follow-up on a session whose earlier turn carried images must read only
// its own turn: the two turns' images together exceed the response limit.
func TestImageFollowUpReadsOnlyItsOwnTurn(t *testing.T) {
	h := setup(t)
	img := testPNG(t, 700, 700, true)
	h.attach(t, [3]string{"a.png", "image/png", img}, [3]string{"b.png", "image/png", img})
	var events []map[string]any
	var metadata map[string]string
	creates, posts, fullReads := 0, 0, 0
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/sessions" && r.Method == http.MethodPost:
			creates++
			var req struct {
				Metadata map[string]string `json:"metadata"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
			}
			metadata = req.Metadata
			json.NewEncoder(w).Encode(map[string]string{"id": "sess_1"})
		case r.URL.Path == "/sessions/sess_1" && r.Method == http.MethodGet:
			json.NewEncoder(w).Encode(map[string]any{"status": "idle", "agent": "agent_1", "environment_id": "env_1", "metadata": metadata})
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == http.MethodPost:
			var req struct {
				Events []map[string]any `json:"events"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				t.Error(err)
				return
			}
			posts++
			req.Events[0]["id"] = fmt.Sprint("evt_user_", posts)
			events = append(events, req.Events[0])
			json.NewEncoder(w).Encode(map[string]any{"data": req.Events})
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == http.MethodGet:
			if posts > 1 && r.URL.Query().Get("after_id") == "" && r.URL.Query().Get("page") == "" && r.URL.Query().Get("order") != "desc" {
				fullReads++
			}
			json.NewEncoder(w).Encode(eventListPage(r, events))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer q.Close()
	h.b.qoder.base = q.URL
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	events = append(events, map[string]any{"id": "evt_idle_1", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}})
	if err := h.b.step(ctx); err != nil {
		t.Fatalf("finish first turn: %v", err)
	}
	if h.complete == nil || h.b.state.first() != nil {
		t.Fatal("first turn did not complete")
	}

	h.task = newTask("task-2", "sess_1")
	h.task.IssueID = "issue-1"
	h.status, h.complete = "dispatched", nil
	if err := h.b.step(ctx); err != nil {
		t.Fatalf("follow-up: %v", err)
	}
	run := h.b.state.first()
	if creates != 1 || posts != 2 || run == nil || !run.Continued || !run.Started {
		t.Fatalf("follow-up not continued and confirmed: creates=%d posts=%d run=%#v", creates, posts, run)
	}
	if fullReads != 0 {
		t.Fatalf("follow-up re-read the session history %d times", fullReads)
	}
	events = append(events, map[string]any{"id": "evt_idle_2", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}})
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.complete == nil || h.failure != nil {
		t.Fatalf("follow-up did not complete: %#v", h.failure)
	}
}
