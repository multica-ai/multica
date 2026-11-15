package qoderruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/daemon"
)

// multiHarness serves several tasks and sessions at once; setup() models one.
type multiHarness struct {
	b             *Bridge
	mu            sync.Mutex
	queue         []*daemon.Task
	claims        int
	status        map[string]string
	completed     map[string]map[string]any
	failed        map[string]map[string]any
	sessions      map[string][]map[string]any
	sessionStatus map[string]string
	creates       int
	sentTo        []string
	prompts       map[string]string
	metadata      map[string]map[string]string
	resources     map[string][]map[string]any
	// mounted models each session's mounted resources like the official API:
	// repositories registered at creation with sesr_… ids, files appended by
	// the add-resource endpoint, tokens updated by the per-resource endpoint.
	mounted  map[string][]mountedResource
	tokenLog []tokenUpdate
	// resourcePageSize bounds GET /sessions/{id}/resources pages (default 100).
	resourcePageSize int
	issue            string
	downloads        map[string]string
	uploads          int
	mounts           map[string][]map[string]any
	mountStatus      int
	// resourceStatus queues one-time HTTP statuses for credential-update
	// POSTs; once empty, updates are recorded normally.
	resourceStatus []int
	// calls records credential updates, attachment mounts and message sends in order.
	calls []string
}

type mountedResource struct {
	id, typ, url, fileID string
}

type tokenUpdate struct {
	session, resourceID, token string
}

// mountResource registers a created or added resource and hands out the next
// sesr_ id.
func (h *multiHarness) mountResource(session string, res map[string]any) {
	id := fmt.Sprintf("sesr_%d", len(h.mounted[session])+1)
	typ, _ := res["type"].(string)
	url, _ := res["url"].(string)
	fileID, _ := res["file_id"].(string)
	h.mounted[session] = append(h.mounted[session], mountedResource{id: id, typ: typ, url: url, fileID: fileID})
}

func newTask(id, prior string) *daemon.Task {
	return &daemon.Task{ID: id, RuntimeID: "runtime-1", WorkspaceID: workspaceID, IssueID: "issue-" + id, IssueIdentifier: "MUL-42", AuthToken: "mat_task", PriorSessionID: prior, Agent: &daemon.AgentData{Instructions: "Run tests", RuntimeConfig: json.RawMessage(`{"qoder_agent_id":"agent_1"}`)}}
}

func setupMulti(t *testing.T, limit int, tasks ...*daemon.Task) *multiHarness {
	t.Helper()
	h := &multiHarness{queue: tasks, status: map[string]string{}, completed: map[string]map[string]any{}, failed: map[string]map[string]any{}, sessions: map[string][]map[string]any{}, sessionStatus: map[string]string{}, prompts: map[string]string{}, metadata: map[string]map[string]string{}, resources: map[string][]map[string]any{}, downloads: map[string]string{}, mounts: map[string][]map[string]any{}, mounted: map[string][]mountedResource{}}
	taskID := func(path string) string {
		rest := strings.TrimPrefix(path, "/api/daemon/tasks/")
		return rest[:strings.Index(rest, "/")]
	}
	m := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/tasks/claim"):
			h.claims++
			var next *daemon.Task
			if len(h.queue) > 0 {
				next, h.queue = h.queue[0], h.queue[1:]
				h.status[next.ID] = "dispatched"
			}
			json.NewEncoder(w).Encode(map[string]any{"task": next})
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			if h.issue != "" {
				io.WriteString(w, h.issue)
				return
			}
			io.WriteString(w, `{"title":"Fix the login bug","description":"Use repository context"}`)
		case strings.HasPrefix(r.URL.Path, "/api/attachments/"):
			body, ok := h.downloads[r.URL.Path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			io.WriteString(w, body)
		case strings.HasSuffix(r.URL.Path, "/status"):
			json.NewEncoder(w).Encode(map[string]string{"status": h.status[taskID(r.URL.Path)]})
		case strings.HasSuffix(r.URL.Path, "/start"):
			h.status[taskID(r.URL.Path)] = "running"
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/complete"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			h.completed[taskID(r.URL.Path)] = body
			h.status[taskID(r.URL.Path)] = "completed"
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/fail"):
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			h.failed[taskID(r.URL.Path)] = body
			h.status[taskID(r.URL.Path)] = "failed"
			io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/messages"), strings.HasSuffix(r.URL.Path, "/session"), strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected Multica request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.Close)
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		defer h.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		switch {
		case r.URL.Path == "/sessions" && r.Method == http.MethodPost:
			h.creates++
			id := fmt.Sprintf("sess_new_%d", h.creates)
			h.sessionStatus[id] = "idle"
			var req struct {
				Metadata  map[string]string `json:"metadata"`
				Resources []map[string]any  `json:"resources"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			h.metadata[id] = req.Metadata
			h.resources[id] = req.Resources
			for _, res := range req.Resources {
				if typ, _ := res["type"].(string); typ == "github_repository" {
					h.mountResource(id, res)
				}
			}
			fmt.Fprintf(w, `{"id":%q}`, id)
		case r.URL.Path == "/files" && r.Method == http.MethodPost:
			h.uploads++
			fmt.Fprintf(w, `{"id":"file_%d"}`, h.uploads)
		case len(parts) == 3 && parts[2] == "resources" && r.Method == http.MethodPost:
			// The official add-resource endpoint only accepts file resources.
			var res map[string]any
			json.NewDecoder(r.Body).Decode(&res)
			if typ, _ := res["type"].(string); typ != "file" {
				w.WriteHeader(http.StatusBadRequest)
				io.WriteString(w, `{"error":{"message":"only file resources can be added"}}`)
				return
			}
			if h.mountStatus != 0 {
				w.WriteHeader(h.mountStatus)
				return
			}
			h.mounts[parts[1]] = append(h.mounts[parts[1]], res)
			h.mountResource(parts[1], res)
			h.calls = append(h.calls, "mount "+parts[1])
			io.WriteString(w, `{}`)
		case len(parts) == 3 && parts[2] == "resources" && r.Method == http.MethodGet:
			// The resource list pages like the event list, with opaque cursors.
			mounted := h.mounted[parts[1]]
			after := strings.TrimPrefix(r.URL.Query().Get("page"), "next:")
			if after != "" {
				for i, res := range mounted {
					if res.id == after {
						mounted = mounted[i+1:]
						break
					}
				}
			}
			size := h.resourcePageSize
			if size <= 0 {
				size = 100
			}
			data := make([]map[string]string, 0, len(mounted))
			for _, res := range mounted {
				data = append(data, map[string]string{"id": res.id, "type": res.typ, "url": res.url})
			}
			page := map[string]any{"data": data, "has_more": false}
			if len(mounted) > size {
				page["data"] = data[:size]
				page["has_more"] = true
				page["next_page"] = "next:" + mounted[size-1].id
			}
			json.NewEncoder(w).Encode(page)
		case len(parts) == 4 && parts[2] == "resources" && r.Method == http.MethodPost:
			// The per-resource credential update endpoint.
			if len(h.resourceStatus) > 0 {
				status := h.resourceStatus[0]
				h.resourceStatus = h.resourceStatus[1:]
				w.WriteHeader(status)
				return
			}
			var res *mountedResource
			for i := range h.mounted[parts[1]] {
				if h.mounted[parts[1]][i].id == parts[3] {
					res = &h.mounted[parts[1]][i]
					break
				}
			}
			if res == nil || res.typ != "github_repository" {
				http.NotFound(w, r)
				return
			}
			var body struct {
				AuthorizationToken string `json:"authorization_token"`
			}
			json.NewDecoder(r.Body).Decode(&body)
			h.tokenLog = append(h.tokenLog, tokenUpdate{session: parts[1], resourceID: parts[3], token: body.AuthorizationToken})
			h.calls = append(h.calls, "credential "+parts[1])
			io.WriteString(w, `{}`)
		case len(parts) == 3 && parts[2] == "events" && r.Method == http.MethodPost:
			h.calls = append(h.calls, "send "+parts[1])
			var req struct {
				Events []event `json:"events"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			h.sentTo = append(h.sentTo, parts[1])
			h.prompts[parts[1]] = req.Events[0].text()
			h.sessionStatus[parts[1]] = "running"
			h.sessions[parts[1]] = append(h.sessions[parts[1]], map[string]any{"id": fmt.Sprintf("evt_%s_%d", parts[1], len(h.sessions[parts[1]])), "type": "user.message", "content": []map[string]string{{"type": "text", "text": req.Events[0].text()}}})
			io.WriteString(w, `{"data":[]}`)
		case len(parts) == 3 && parts[2] == "events":
			json.NewEncoder(w).Encode(eventListPage(r, h.sessions[parts[1]]))
		case len(parts) == 2 && r.Method == http.MethodGet:
			status, ok := h.sessionStatus[parts[1]]
			if !ok {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"status": status, "agent": "agent_1", "environment_id": "env_1", "metadata": h.metadata[parts[1]]})
		default:
			t.Errorf("unexpected Qoder request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(q.Close)
	var err error
	h.b, err = New(Config{MulticaURL: m.URL, MulticaToken: "multica-secret", WorkspaceID: workspaceID, QoderURL: q.URL, QoderToken: "qoder-secret", EnvironmentID: "env_1", StateDir: t.TempDir(), MaxConcurrency: limit}, nil)
	if err != nil {
		t.Fatal(err)
	}
	h.b.state = &journal{Binding: "test", RuntimeID: "runtime-1"}
	return h
}

func (h *multiHarness) finish(session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := len(h.sessions[session])
	h.sessions[session] = append(h.sessions[session],
		map[string]any{"id": fmt.Sprintf("evt_%s_%d", session, n), "type": "agent.message", "content": []map[string]string{{"type": "text", "text": "done in " + session}}},
		map[string]any{"id": fmt.Sprintf("evt_%s_%d", session, n+1), "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}})
	h.sessionStatus[session] = "idle"
}

func TestConcurrentRunsRespectLimit(t *testing.T) {
	h := setupMulti(t, 2, newTask("task-1", ""), newTask("task-2", ""), newTask("task-3", ""))
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.creates != 2 || len(h.b.state.Runs) != 2 || h.claims != 2 || len(h.queue) != 1 {
		t.Fatalf("creates=%d runs=%d claims=%d queued=%d", h.creates, len(h.b.state.Runs), h.claims, len(h.queue))
	}
	h.finish("sess_new_1")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.completed["task-1"]["session_id"] != "sess_new_1" {
		t.Fatalf("task-1 result: %#v", h.completed["task-1"])
	}
	if h.completed["task-2"] != nil || h.creates != 3 || len(h.b.state.Runs) != 2 || len(h.queue) != 0 {
		t.Fatalf("freed slot not reused: creates=%d runs=%d queued=%d", h.creates, len(h.b.state.Runs), len(h.queue))
	}
}

func TestFollowUpContinuesIdleSession(t *testing.T) {
	h := setupMulti(t, 1, newTask("task-1", "sess_prev"))
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint(nil)}
	h.sessions["sess_prev"] = []map[string]any{
		{"id": "evt_old_0", "type": "user.message", "content": []map[string]string{{"type": "text", "text": "earlier run"}}},
		{"id": "evt_old_1", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}},
	}
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || len(h.sentTo) != 1 || h.sentTo[0] != "sess_prev" {
		t.Fatalf("follow-up did not continue: creates=%d sentTo=%v", h.creates, h.sentTo)
	}
	if h.completed["task-1"] != nil {
		t.Fatal("earlier idle event completed the follow-up")
	}
	h.finish("sess_prev")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if h.completed["task-1"]["session_id"] != "sess_prev" || h.completed["task-1"]["output"] != "done in sess_prev" {
		t.Fatalf("result: %#v", h.completed["task-1"])
	}
}

func chatFollowUp(h *multiHarness, files ...[3]string) {
	task := h.queue[0]
	task.IssueID, task.IssueIdentifier = "", ""
	task.ChatSessionID = "chat-1"
	task.ChatMessage = "Read the attached notes"
	chatAttach(h.downloads, task, files...)
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint(nil)}
}

// A chat follow-up keeps its conversation: the message's files are mounted
// into the earlier session before the message that names them.
func TestChatFollowUpMountsAttachmentsIntoPriorSession(t *testing.T) {
	h := setupMulti(t, 1, newTask("task-1", "sess_prev"))
	chatFollowUp(h, [3]string{"notes.md", "text/markdown", "hello"}, [3]string{"todo.txt", "text/plain", "ship it"})
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || fmt.Sprint(h.calls) != "[mount sess_prev mount sess_prev send sess_prev]" {
		t.Fatalf("creates=%d calls=%v failed=%v", h.creates, h.calls, h.failed)
	}
	var paths []string
	for _, m := range h.mounts["sess_prev"] {
		paths = append(paths, fmt.Sprint(m["type"], " ", m["file_id"], " ", m["mount_path"]))
	}
	if want := "[file file_1 /data/attachments/task-1/notes.md file file_2 /data/attachments/task-1/todo.txt]"; fmt.Sprint(paths) != want {
		t.Fatalf("mounts: %v", paths)
	}
	if !strings.Contains(h.prompts["sess_prev"], "/data/attachments/task-1/notes.md (mounted file)") {
		t.Fatalf("prompt: %s", h.prompts["sess_prev"])
	}
}

// If the earlier session refuses the files, nothing has been sent yet, so the
// message goes to a fresh session that has them.
func TestChatFollowUpStartsFreshWhenMountIsRejected(t *testing.T) {
	h := setupMulti(t, 1, newTask("task-1", "sess_prev"))
	chatFollowUp(h, [3]string{"notes.md", "text/markdown", "hello"})
	h.mountStatus = http.StatusConflict
	ctx := context.Background()
	for i := 0; i < 2 && len(h.sentTo) == 0; i++ {
		if err := h.b.step(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if h.creates != 1 || fmt.Sprint(h.sentTo) != "[sess_new_1]" || len(h.failed) != 0 {
		t.Fatalf("creates=%d sentTo=%v failed=%v", h.creates, h.sentTo, h.failed)
	}
	if res := h.resources["sess_new_1"]; len(res) != 1 || res[0]["mount_path"] != "/data/attachments/task-1/notes.md" {
		t.Fatalf("fresh session resources: %v", res)
	}
}

// An issue run uploads every issue attachment again, so it still starts a
// fresh session instead of mounting duplicates into the earlier one.
func TestIssueFollowUpWithAttachmentsStartsFreshSession(t *testing.T) {
	h := setupMulti(t, 1, newTask("task-1", "sess_prev"))
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint(nil)}
	h.downloads["/api/attachments/a0/download"] = "hello"
	h.issue = `{"title":"Fix","description":"See notes","attachments":[{"id":"a0","filename":"notes.md","content_type":"text/markdown","size_bytes":5,"download_url":"/api/attachments/a0/download"}]}`
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 1 || fmt.Sprint(h.sentTo) != "[sess_new_1]" || len(h.mounts) != 0 {
		t.Fatalf("creates=%d sentTo=%v mounts=%v", h.creates, h.sentTo, h.mounts)
	}
}

func TestFollowUpStartsNewSessionWhenPriorIsUnavailable(t *testing.T) {
	for name, status := range map[string]string{"busy": "running", "deleted": ""} {
		t.Run(name, func(t *testing.T) {
			h := setupMulti(t, 1, newTask("task-1", "sess_prev"))
			if status != "" {
				h.sessionStatus["sess_prev"] = status
			}
			if err := h.b.step(context.Background()); err != nil {
				t.Fatal(err)
			}
			if h.creates != 1 || len(h.sentTo) != 1 || h.sentTo[0] != "sess_new_1" {
				t.Fatalf("creates=%d sentTo=%v", h.creates, h.sentTo)
			}
		})
	}
}

func repoTask(id, prior, repo, ref string) *daemon.Task {
	task := newTask(id, prior)
	task.Agent.RuntimeConfig = json.RawMessage(fmt.Sprintf(`{"qoder_agent_id":"agent_1","repository":{"url":%q,"ref":%q}}`, repo, ref))
	return task
}

func TestFollowUpReusesSessionOnlyForSameRepositoryAndCredential(t *testing.T) {
	const repoA, repoB = "https://github.com/acme/a", "https://github.com/acme/b"
	for name, tc := range map[string]struct {
		followUp *daemon.Task
		tokens   map[string]string
		reuse    bool
	}{
		"unchanged":        {repoTask("task-2", "sess_new_1", repoA, "main"), map[string]string{repoA: "tok-a"}, true},
		"other repository": {repoTask("task-2", "sess_new_1", repoB, "main"), map[string]string{repoA: "tok-a", repoB: "tok-b"}, false},
		"other branch":     {repoTask("task-2", "sess_new_1", repoA, "release"), map[string]string{repoA: "tok-a"}, false},
		"rotated token":    {repoTask("task-2", "sess_new_1", repoA, "main"), map[string]string{repoA: "tok-a2"}, false},
	} {
		t.Run(name, func(t *testing.T) {
			h := setupMulti(t, 1, repoTask("task-1", "", repoA, "main"), tc.followUp)
			h.b.cfg.GitHubTokens = map[string]string{repoA: "tok-a"}
			ctx := context.Background()
			if err := h.b.step(ctx); err != nil {
				t.Fatal(err)
			}
			h.b.cfg.GitHubTokens = tc.tokens
			h.finish("sess_new_1")
			if err := h.b.step(ctx); err != nil {
				t.Fatal(err)
			}
			if reused := h.creates == 1 && h.sentTo[len(h.sentTo)-1] == "sess_new_1"; reused != tc.reuse {
				t.Fatalf("reuse=%v, want %v (creates=%d sentTo=%v)", reused, tc.reuse, h.creates, h.sentTo)
			}
			if !tc.reuse {
				got := h.resources["sess_new_2"]
				if len(got) != 1 || got[0]["authorization_token"] != tc.tokens[got[0]["url"].(string)] {
					t.Fatalf("new session resources: %v", got)
				}
			}
		})
	}
}

func TestFollowUpFailsWhenRepositoryCredentialWasRemoved(t *testing.T) {
	const repo = "https://github.com/acme/a"
	h := setupMulti(t, 1, repoTask("task-1", "", repo, ""), repoTask("task-2", "sess_new_1", repo, ""))
	h.b.cfg.GitHubTokens = map[string]string{repo: "tok-a"}
	ctx := context.Background()
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	h.b.cfg.GitHubTokens = nil
	h.finish("sess_new_1")
	if err := h.b.step(ctx); err != nil {
		t.Fatal(err)
	}
	if len(h.sentTo) != 1 || h.creates != 1 || h.failed["task-2"] == nil {
		t.Fatalf("follow-up ran without a credential: creates=%d sentTo=%v failed=%v", h.creates, h.sentTo, h.failed["task-2"])
	}
}

func TestFollowUpSkipsSessionWithoutFingerprint(t *testing.T) {
	h := setupMulti(t, 1, newTask("task-1", "sess_prev"))
	h.sessionStatus["sess_prev"] = "idle"
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 1 || len(h.sentTo) != 1 || h.sentTo[0] != "sess_new_1" {
		t.Fatalf("legacy session reused: creates=%d sentTo=%v", h.creates, h.sentTo)
	}
}

func TestPromptAsksForLinkableBranch(t *testing.T) {
	h := setupMulti(t, 1, newTask("task-1", ""))
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	prompt := h.prompts["sess_new_1"]
	if !strings.Contains(prompt, "multica/mul-42-fix-the-login-bug") || !strings.Contains(prompt, `"MUL-42: "`) {
		t.Fatalf("prompt lacks branch convention: %s", prompt)
	}
}

func TestLegacySingleRunJournalDecodes(t *testing.T) {
	j, err := decodeJournal([]byte(`{"binding":"b","runtime_id":"rt","run":{"task_id":"task-1","session_id":"sess_1","phase":"running"}}`), "b")
	if err != nil {
		t.Fatal(err)
	}
	if j.Run != nil || len(j.Runs) != 1 || j.Runs[0].SessionID != "sess_1" {
		t.Fatalf("legacy run not folded: %#v", j)
	}
	if _, err := decodeJournal([]byte(`{"binding":"b","runs":[{"task_id":"t","phase":"prepared"},{"task_id":"t","phase":"prepared"}]}`), "b"); err == nil {
		t.Fatal("duplicate task accepted")
	}
}

func TestBranchName(t *testing.T) {
	for _, tc := range []struct{ id, title, want string }{
		{"MUL-7", "Fix: Login → SSO!", "multica/mul-7-fix-login-sso"},
		{"MUL-7", "修复登录", "multica/mul-7"},
		{"MUL-7", strings.Repeat("long title ", 10), "multica/mul-7-long-title-long-title-long-title-long"},
	} {
		if got := BranchName(tc.id, tc.title); got != tc.want {
			t.Errorf("BranchName(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}

func TestAgentDefaultRepository(t *testing.T) {
	if _, err := ParseAgentConfig(json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)); err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{
		`{"qoder_agent_id":"agent_1","repository":{"url":"http://github.com/acme/app"}}`,
		`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"refs/tags/v1"}}`,
	} {
		if _, err := ParseAgentConfig(json.RawMessage(raw)); err == nil || !strings.HasPrefix(err.Error(), "default repository") {
			t.Errorf("accepted %s: %v", raw, err)
		}
	}
}

func TestResourceFingerprintSurvivesJournalRoundTrip(t *testing.T) {
	release, err := repositoryResource(Repository{URL: "https://github.com/acme/a", Ref: "release"})
	if err != nil {
		t.Fatal(err)
	}
	defaultBranch, _ := repositoryResource(Repository{URL: "https://github.com/acme/a"})
	raw, _ := json.Marshal(&run{TaskID: "t", Resources: []map[string]any{release}})
	var restored run
	if err := json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	entries := func(resources ...map[string]any) []fingerprintEntry {
		out := make([]fingerprintEntry, 0, len(resources))
		for _, res := range resources {
			key, err := repositoryKey(res["url"].(string))
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, fingerprintEntry{key: key, branch: checkoutBranch(res["checkout"]), credential: "credential"})
		}
		return out
	}
	if got, want := resourceFingerprint(entries(restored.Resources...)), resourceFingerprint(entries(release)); got != want {
		t.Fatal("restored resources fingerprint differently")
	}
	if resourceFingerprint(entries(restored.Resources...)) == resourceFingerprint(entries(defaultBranch)) {
		t.Fatal("restored branch dropped from fingerprint")
	}
}

// A follow-up whose repository credential comes from the GitHub App
// integration keeps reusing the idle session — the fingerprint hashes the
// installation binding, not the rotating token — and pushes the fresh token
// into the session before the follow-up message.
func TestFollowUpRefreshesRotatingGitHubAppCredential(t *testing.T) {
	task := newTask("task-1", "sess_prev")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint([]fingerprintEntry{{key: "https://github.com/acme/app", branch: "main", credential: "github_app:424242"}})}
	h.sessions["sess_prev"] = []map[string]any{
		{"id": "evt_old_0", "type": "user.message", "content": []map[string]string{{"type": "text", "text": "earlier run"}}},
		{"id": "evt_old_1", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}},
	}
	// The prior session already carries the mounted repository; only its
	// credential may be updated, and only through the per-resource endpoint.
	h.mountResource("sess_prev", map[string]any{"type": "github_repository", "url": "https://github.com/acme/app"})
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		return "ghs_fresh", 424242, nil
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || len(h.sentTo) != 1 || h.sentTo[0] != "sess_prev" {
		t.Fatalf("follow-up did not continue: creates=%d sentTo=%v", h.creates, h.sentTo)
	}
	if len(h.tokenLog) != 1 || h.tokenLog[0].session != "sess_prev" || h.tokenLog[0].resourceID != "sesr_1" || h.tokenLog[0].token != "ghs_fresh" {
		t.Fatalf("rotating credential not updated on the mounted resource: %+v", h.tokenLog)
	}
	if fmt.Sprint(h.calls) != "[credential sess_prev send sess_prev]" {
		t.Fatalf("calls: %v", h.calls)
	}
}

// A rotating credential refresh that is rate limited or fails transiently
// must defer the follow-up message, not send it with the stale session
// credential: the run stays "prepared" until the refresh completed, and the
// retry re-resolves, refreshes and only then sends.
func TestFollowUpRefreshRetriesAfterDeferral(t *testing.T) {
	task := newTask("task-1", "sess_prev")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint([]fingerprintEntry{{key: "https://github.com/acme/app", branch: "main", credential: "github_app:424242"}})}
	h.sessions["sess_prev"] = []map[string]any{
		{"id": "evt_old_0", "type": "user.message", "content": []map[string]string{{"type": "text", "text": "earlier run"}}},
		{"id": "evt_old_1", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}},
	}
	h.mountResource("sess_prev", map[string]any{"type": "github_repository", "url": "https://github.com/acme/app"})
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		return "ghs_fresh", 424242, nil
	}
	refresh := func() *run {
		t.Helper()
		run := h.b.state.first()
		if run == nil {
			t.Fatal("run dropped while refresh deferred")
		}
		return run
	}
	// First resource POST is rate limited: the tick defers with the run still
	// "prepared" and no message sent.
	h.resourceStatus = []int{http.StatusTooManyRequests}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := refresh()
	if run.Phase != "prepared" || run.Continued != true {
		t.Fatalf("rate limit did not defer inside prepared: %#v", run)
	}
	if len(h.sentTo) != 0 || len(h.mounts["sess_prev"]) != 0 {
		t.Fatalf("message or mount escaped the deferral: sentTo=%v mounts=%v", h.sentTo, h.mounts["sess_prev"])
	}
	// The backoff holds the run; a tick inside it changes nothing.
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(h.sentTo) != 0 || len(h.mounts["sess_prev"]) != 0 {
		t.Fatalf("backoff ignored: sentTo=%v mounts=%v", h.sentTo, h.mounts["sess_prev"])
	}
	// After the backoff, a transient server error also leaves the run
	// prepared with nothing sent.
	run = refresh()
	run.RetryAt = time.Now().Add(-time.Second)
	h.resourceStatus = []int{http.StatusInternalServerError}
	if err := h.b.step(context.Background()); err == nil {
		t.Fatal("transient refresh failure swallowed")
	}
	run = refresh()
	if run.Phase != "prepared" {
		t.Fatalf("transient error changed state: %#v", run)
	}
	if len(h.sentTo) != 0 {
		t.Fatalf("message sent despite failed refresh: %v", h.sentTo)
	}
	// A clean retry refreshes the credential and only then sends.
	run = refresh()
	run.RetryAt = time.Now().Add(-time.Second)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	run = refresh()
	if run.Phase != "running" {
		t.Fatalf("retry did not complete the refresh: phase=%s", run.Phase)
	}
	if len(h.sentTo) != 1 || h.sentTo[0] != "sess_prev" || h.creates != 0 {
		t.Fatalf("follow-up did not continue after retry: creates=%d sentTo=%v", h.creates, h.sentTo)
	}
	if len(h.tokenLog) != 1 || h.tokenLog[0].session != "sess_prev" || h.tokenLog[0].resourceID != "sesr_1" || h.tokenLog[0].token != "ghs_fresh" {
		t.Fatalf("credential not refreshed on retry: %+v", h.tokenLog)
	}
	if fmt.Sprint(h.calls) != "[credential sess_prev send sess_prev]" {
		t.Fatalf("calls: %v", h.calls)
	}
}

// A run whose rotating GitHub App credential ages past the refresh interval
// re-issues it and pushes the fresh token into the live session, so git work
// after the token's one-hour lifetime still succeeds.
func TestRunningSessionRefreshesRotatingCredential(t *testing.T) {
	task := newTask("task-1", "")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	issued := 0
	tokens := []string{"ghs_first", "ghs_second"}
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		token := tokens[issued]
		issued++
		return token, 424242, nil
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := h.b.state.first()
	if run == nil || run.Phase != "running" || run.CredentialIssuedAt.IsZero() {
		t.Fatalf("run did not start with a rotating credential: %#v", run)
	}
	if h.resources["sess_new_1"][0]["authorization_token"] != "ghs_first" {
		t.Fatalf("creation credential: %v", h.resources["sess_new_1"])
	}
	// A fresh issuance is not due yet: no refresh, no extra issuance.
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if issued != 1 || len(h.tokenLog) != 0 {
		t.Fatalf("premature refresh: issued=%d updates=%v", issued, h.tokenLog)
	}
	// Past the interval the live session receives a fresh token on its
	// mounted repository, without a new session or message.
	run.CredentialIssuedAt = time.Now().Add(-credentialRefreshInterval - time.Minute)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if issued != 2 || h.creates != 1 || len(h.sentTo) != 1 {
		t.Fatalf("refresh spawned work: issued=%d creates=%d sentTo=%v", issued, h.creates, h.sentTo)
	}
	if len(h.tokenLog) != 1 || h.tokenLog[0].session != "sess_new_1" || h.tokenLog[0].resourceID != "sesr_1" || h.tokenLog[0].token != "ghs_second" {
		t.Fatalf("live session not refreshed: %+v", h.tokenLog)
	}
}

// When a deferred credential refresh outlives the prior session (it stopped
// being idle), the follow-up starts a clean fresh session: the new session is
// not treated as continued, so its files are mounted exactly once and the
// stale credit base of the dead session is dropped.
func TestDeferredRefreshFallsBackToFreshSession(t *testing.T) {
	task := newTask("task-1", "sess_prev")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	chatFollowUp(h, [3]string{"notes.md", "text/markdown", "hello"})
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint([]fingerprintEntry{{key: "https://github.com/acme/app", branch: "main", credential: "github_app:424242"}})}
	h.mountResource("sess_prev", map[string]any{"type": "github_repository", "url": "https://github.com/acme/app"})
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		return "ghs_fresh", 424242, nil
	}
	h.resourceStatus = []int{http.StatusTooManyRequests}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := h.b.state.first()
	if run == nil || run.Phase != "prepared" || !run.Continued || len(h.sentTo) != 0 || len(h.calls) != 0 {
		t.Fatalf("refresh was not deferred: %#v calls=%v", run, h.calls)
	}
	// The prior session becomes busy during the backoff window.
	h.sessionStatus["sess_prev"] = "running"
	run.RetryAt = time.Now().Add(-time.Second)
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	run = h.b.state.first()
	if run == nil || run.Continued || run.SessionID != "sess_new_1" || run.Credits.Base != nil {
		t.Fatalf("fresh session carries continued state: %#v", run)
	}
	if h.creates != 1 || fmt.Sprint(h.calls) != "[send sess_new_1]" {
		t.Fatalf("fresh session not clean: creates=%d calls=%v", h.creates, h.calls)
	}
	// The attachment came with the created session, not remounted.
	if len(h.mounts["sess_new_1"]) != 0 || len(h.resources["sess_new_1"]) != 2 {
		t.Fatalf("files mounted twice: mounts=%v resources=%v", h.mounts["sess_new_1"], h.resources["sess_new_1"])
	}
}

// The repository can sit beyond the first page of the session's resource
// list; the refresh must follow the cursor instead of missing it.
func TestFollowUpRefreshFindsRepositoryBeyondFirstPage(t *testing.T) {
	task := newTask("task-1", "sess_prev")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint([]fingerprintEntry{{key: "https://github.com/acme/app", branch: "main", credential: "github_app:424242"}})}
	h.sessions["sess_prev"] = []map[string]any{
		{"id": "evt_old_0", "type": "user.message", "content": []map[string]string{{"type": "text", "text": "earlier run"}}},
		{"id": "evt_old_1", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}},
	}
	h.mountResource("sess_prev", map[string]any{"type": "file", "file_id": "file_earlier"})
	h.mountResource("sess_prev", map[string]any{"type": "github_repository", "url": "https://github.com/acme/app"})
	h.resourcePageSize = 1
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		return "ghs_fresh", 424242, nil
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.creates != 0 || len(h.sentTo) != 1 || h.sentTo[0] != "sess_prev" {
		t.Fatalf("follow-up did not continue: creates=%d sentTo=%v", h.creates, h.sentTo)
	}
	if len(h.tokenLog) != 1 || h.tokenLog[0].resourceID != "sesr_2" || h.tokenLog[0].token != "ghs_fresh" {
		t.Fatalf("paged repository not refreshed: %+v", h.tokenLog)
	}
}

// A prior session that no longer carries the mounted repository cannot take
// the credential update; the follow-up starts a fresh session instead of
// reusing it with a stale credential.
func TestFollowUpStartsFreshWhenRepositoryUnmounted(t *testing.T) {
	task := newTask("task-1", "sess_prev")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint([]fingerprintEntry{{key: "https://github.com/acme/app", branch: "main", credential: "github_app:424242"}})}
	h.sessions["sess_prev"] = []map[string]any{
		{"id": "evt_old_0", "type": "user.message", "content": []map[string]string{{"type": "text", "text": "earlier run"}}},
		{"id": "evt_old_1", "type": "session.status_idle", "stop_reason": map[string]string{"type": "end_turn"}},
	}
	// No github_repository mounted on the prior session.
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		return "ghs_fresh", 424242, nil
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	run := h.b.state.first()
	if h.creates != 1 || run.Continued || len(h.tokenLog) != 0 {
		t.Fatalf("unmounted repository not refused: creates=%d continued=%v updates=%v", h.creates, run.Continued, h.tokenLog)
	}
	if len(h.sentTo) != 1 || h.sentTo[0] != "sess_new_1" {
		t.Fatalf("fresh session did not take over: %v", h.sentTo)
	}
}

// Once the workspace installation no longer covers the repository, the
// pre-run credential re-resolution rejects the follow-up instead of reusing
// the idle session with a stale credential.
func TestFollowUpBlockedWhenInstallationRevoked(t *testing.T) {
	task := newTask("task-1", "sess_prev")
	task.Agent.RuntimeConfig = json.RawMessage(`{"qoder_agent_id":"agent_1","repository":{"url":"https://github.com/acme/app","ref":"main"}}`)
	h := setupMulti(t, 1, task)
	h.sessionStatus["sess_prev"] = "idle"
	h.metadata["sess_prev"] = map[string]string{resourcesMetadataKey: resourceFingerprint([]fingerprintEntry{{key: "https://github.com/acme/app", branch: "main", credential: "github_app:424242"}})}
	h.b.cfg.GitHubAppToken = func(context.Context, string, string) (string, int64, error) {
		return "", 0, ErrGitHubAppNotCovered
	}
	if err := h.b.step(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.failed["task-1"] == nil || h.failed["task-1"]["error"] != "no GitHub credential configured for project repository" {
		t.Fatalf("revoked installation not blocked: %#v", h.failed["task-1"])
	}
	if h.creates != 0 || len(h.sentTo) != 0 {
		t.Fatalf("revoked installation still ran: creates=%d sentTo=%v", h.creates, h.sentTo)
	}
}
