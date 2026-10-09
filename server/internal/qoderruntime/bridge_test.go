package qoderruntime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
)

const workspaceID = "11111111-1111-4111-8111-111111111111"

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
		if strings.HasPrefix(r.URL.Path, "/api/issues/") {
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
		case strings.HasPrefix(r.URL.Path, "/api/issues/"):
			io.WriteString(w, `{"title":"Fix bug","description":"Use repository context"}`)
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
		case strings.HasSuffix(r.URL.Path, "/session"), strings.HasSuffix(r.URL.Path, "/cancel-ack"):
			io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected Multica request %s", r.URL)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(m.Close)
	q := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Header.Get("Authorization") != "Bearer qoder-secret" {
			t.Error("wrong Qoder credential")
		}
		switch {
		case r.URL.Path == "/sessions" && r.Method == "POST":
			h.creates++
			var input struct {
				Agent string `json:"agent"`
			}
			if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
				t.Fatal(err)
			}
			h.sessionAgent = input.Agent
			io.WriteString(w, `{"id":"sess_1","status":"idle"}`)
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == "POST":
			h.sends++
			var req struct {
				Events []event `json:"events"`
			}
			json.NewDecoder(r.Body).Decode(&req)
			h.prompt = req.Events[0].text()
			h.events = append(h.events, map[string]any{"id": "evt_user", "type": "user.message", "content": []map[string]string{{"type": "text", "text": h.prompt}}})
			if h.lostSend {
				http.Error(w, "unknown", 502)
				return
			}
			io.WriteString(w, `{"data":[]}`)
		case r.URL.Path == "/sessions/sess_1/events" && r.Method == "GET":
			if r.URL.Query().Get("page") != "" {
				t.Error("event ID used as page cursor")
			}
			events := h.events
			after := r.URL.Query().Get("after_id")
			if after != "" {
				for i, e := range events {
					if e["id"] == after {
						events = events[i+1:]
						break
					}
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"data": events, "has_more": false})
		case strings.HasSuffix(r.URL.Path, "/cancel"):
			h.cancels++
			io.WriteString(w, `{"status":"canceling"}`)
		case r.URL.Path == "/sessions/sess_1":
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
	if h.creates != 1 || h.sends != 1 || len(h.messages) != 1 || h.b.state.Run != nil {
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
	if j.Run.Phase != "sending" || j.Run.SessionID != "sess_1" {
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
	if h.cancels != 1 || h.b.state.Run != nil || h.complete != nil {
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
	h.b.state.Run = &run{TaskID: "task-1", SessionID: "sess_1", Phase: "final", Outcome: "completed", Output: "Recovered result"}
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
	if h.failure == nil || h.b.state.Run != nil {
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
			if err != nil || restored.Run.AgentID != id {
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
