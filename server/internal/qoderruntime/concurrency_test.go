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
}

func newTask(id, prior string) *daemon.Task {
	return &daemon.Task{ID: id, RuntimeID: "runtime-1", WorkspaceID: workspaceID, IssueID: "issue-" + id, IssueIdentifier: "MUL-42", AuthToken: "mat_task", PriorSessionID: prior, Agent: &daemon.AgentData{Instructions: "Run tests", RuntimeConfig: json.RawMessage(`{"qoder_agent_id":"agent_1"}`)}}
}

func setupMulti(t *testing.T, limit int, tasks ...*daemon.Task) *multiHarness {
	t.Helper()
	h := &multiHarness{queue: tasks, status: map[string]string{}, completed: map[string]map[string]any{}, failed: map[string]map[string]any{}, sessions: map[string][]map[string]any{}, sessionStatus: map[string]string{}, prompts: map[string]string{}, metadata: map[string]map[string]string{}, resources: map[string][]map[string]any{}}
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
			io.WriteString(w, `{"title":"Fix the login bug","description":"Use repository context"}`)
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
			fmt.Fprintf(w, `{"id":%q}`, id)
		case len(parts) == 3 && parts[2] == "events" && r.Method == http.MethodPost:
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
			events := h.sessions[parts[1]]
			if after := r.URL.Query().Get("after_id"); after != "" {
				for i, e := range events {
					if e["id"] == after {
						events = events[i+1:]
						break
					}
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": events, "has_more": false})
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
	if got, want := resourceFingerprint(restored.Resources), resourceFingerprint([]map[string]any{release}); got != want {
		t.Fatal("restored resources fingerprint differently")
	}
	if resourceFingerprint(restored.Resources) == resourceFingerprint([]map[string]any{defaultBranch}) {
		t.Fatal("restored branch dropped from fingerprint")
	}
}
