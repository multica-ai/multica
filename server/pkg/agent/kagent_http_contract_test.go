//go:build !windows

package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/multica-ai/multica/server/pkg/kagent"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// This consumer fixture exercises Kubernetes and A2A HTTP requests through the
// real clients. The custom transport avoids binding sockets in the sandbox.
type sliceHTTPFixture struct {
	logs                   bytes.Buffer
	journalDir             string
	reconciler             *kagent.Reconciler
	beforeResponse         func(string)
	t                      *testing.T
	mu                     sync.Mutex
	journal                *kagent.Journal
	lifecycle              *kagent.Lifecycle
	client                 *kagent.A2AClient
	agent                  *kagent.Agent
	sends                  []kagent.RemoteMessage
	tasks                  map[string]*kagent.RemoteTask
	nextState, cancelState string
	failSend, failGet      bool
	cancels                int
	unreadyGets, agentGets int
	agentGetError          int
}

const sliceAgentID = "12345678-1234-1234-1234-123456789abc"
const sliceSessionID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func newSliceHTTPFixture(t *testing.T) *sliceHTTPFixture {
	t.Helper()
	f := &sliceHTTPFixture{t: t, tasks: map[string]*kagent.RemoteTask{}, nextState: kagent.StateCompleted, cancelState: kagent.StateCanceled}
	server := newKagentContractServer(http.HandlerFunc(f.serve))
	c := kagent.HTTPConfig{Endpoint: server.URL, Client: server.Client(), Token: func(context.Context) (string, error) { return "test-caller-token", nil }}
	store, err := kagent.NewKubernetesStore(c, []string{"test"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := kagent.New(store, kagent.Config{AllowedNamespaces: []string{"test"}, Logger: slog.New(slog.NewTextHandler(&f.logs, nil))})
	if err != nil {
		t.Fatal(err)
	}
	f.reconciler = r
	f.journalDir = filepath.Join(t.TempDir(), "journal")
	f.journal, err = kagent.NewJournal(f.journalDir)
	if err != nil {
		t.Fatal(err)
	}
	f.lifecycle, err = kagent.NewLifecycle(r, f.journal)
	if err != nil {
		t.Fatal(err)
	}
	f.client, err = kagent.NewA2AClient(c)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.lifecycle.Enroll(kagent.Desired{AgentID: sliceAgentID, Namespace: "test", HarnessRefName: "dedicated-readonly", Inline: &kagent.AgentTemplateSpec{SystemPrompt: "Native harness DEV"}}); err != nil {
		t.Fatal(err)
	}
	return f
}
func (f *sliceHTTPFixture) backend(id string) Backend {
	f.t.Helper()
	b, err := NewKagentBackend(KagentConfig{Enabled: true, Lifecycle: f.lifecycle, Journal: f.journal, Client: f.client, AgentID: sliceAgentID, WorkspaceID: "workspace", ConversationID: "conversation", AttemptID: id, PollInterval: time.Millisecond})
	if err != nil {
		f.t.Fatal(err)
	}
	return b
}
func (f *sliceHTTPFixture) serve(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if r.Header.Get("Authorization") != "Bearer test-caller-token" {
		f.t.Error("missing caller auth")
	}
	if r.URL.Path == "/apis/"+kagent.GroupVersionAPI+"/namespaces/test/agents" && r.Method == "POST" {
		var a kagent.Agent
		json.NewDecoder(r.Body).Decode(&a)
		a.Metadata.UID = "test-uid"
		a.Metadata.ResourceVersion = "1"
		a.Metadata.Generation = 1
		f.agent = &a
		w.WriteHeader(201)
		json.NewEncoder(w).Encode(a)
		return
	}
	name, _ := kagent.ResourceName(sliceAgentID)
	if r.URL.Path == "/apis/"+kagent.GroupVersionAPI+"/namespaces/test/agents/"+name {
		if f.agent == nil {
			w.WriteHeader(404)
			return
		}
		switch r.Method {
		case "GET":
			f.agentGets++
			if f.agentGetError != 0 {
				w.WriteHeader(f.agentGetError)
				return
			}
			if f.agentGets <= f.unreadyGets {
				json.NewEncoder(w).Encode(f.agent)
				return
			}
			f.agent.Status.Conditions = []kagent.Condition{{Type: kagent.ConditionReady, Status: "True", ObservedGeneration: f.agent.Metadata.Generation}}
			json.NewEncoder(w).Encode(f.agent)
		case "DELETE":
			f.agent = nil
			w.Write([]byte(`{}`))
		default:
			f.t.Errorf("unexpected Kubernetes mutation: %s", r.Method)
			w.WriteHeader(500)
		}
		return
	}
	if r.URL.Path != "/agents/test/"+name {
		f.t.Errorf("wrong consumer path: %s", r.URL.Path)
		w.WriteHeader(404)
		return
	}
	var req struct {
		JSONRPC, ID, Method string
		Params              json.RawMessage
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		f.t.Error(err)
	}
	var result any
	switch req.Method {
	case "SendMessage":
		var p struct {
			Message       kagent.RemoteMessage
			Configuration struct{ ReturnImmediately bool }
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			f.t.Error(err)
		}
		if p.Message.Role != "ROLE_USER" || !p.Configuration.ReturnImmediately {
			f.t.Error("non-alpha7 request")
		}
		if len(f.sends) == 0 && p.Message.ContextID != "" {
			f.t.Error("initial context must be empty")
		}
		f.sends = append(f.sends, p.Message)
		sessionID := p.Message.ContextID
		if sessionID == "" {
			sessionID = sliceSessionID
			if len(f.sends) > 1 {
				sessionID = kagent.NewID()
			}
		}
		task := &kagent.RemoteTask{ID: fmt.Sprintf("remote-task-%d", len(f.sends)), ContextID: sessionID}
		task.Status.State = kagent.StateWorking
		f.tasks[task.ID] = task
		result = map[string]any{"task": task}
		if f.failSend {
			w.Write([]byte(`{`))
			return
		}
	case "GetTask", "CancelTask":
		var p struct{ ID string }
		json.Unmarshal(req.Params, &p)
		task := f.tasks[p.ID]
		if task == nil {
			f.t.Errorf("unknown task %s", p.ID)
			w.WriteHeader(404)
			return
		}
		if req.Method == "CancelTask" {
			f.cancels++
			task.Status.State = f.cancelState
		} else {
			if f.failGet {
				w.WriteHeader(503)
				return
			}
			task.Status.State = f.nextState
		}
		task.Status.Message = &kagent.RemoteMessage{MessageID: "reply", Role: "ROLE_AGENT", Parts: []kagent.Part{{Text: "native response"}}}
		result = task
	default:
		f.t.Errorf("wrong method: %s", req.Method)
		w.WriteHeader(400)
		return
	}
	if f.beforeResponse != nil {
		f.beforeResponse(req.Method)
	}
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
}
func executeSliceKagent(t *testing.T, b Backend, ctx context.Context) *Session {
	t.Helper()
	s, err := b.Execute(ctx, "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"})
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func finishSliceKagent(t *testing.T, s *Session) (Result, bool) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for range s.Messages {
		}
		close(done)
	}()
	select {
	case result, ok := <-s.Result:
		<-done
		if !ok {
			t.Fatal("missing Result")
		}
		known := result.Status != "cleanup_unconfirmed"
		if !s.CleanupRequired || s.TerminalObserved() != known {
			t.Fatal("incorrect cleanup/terminal acknowledgement")
		}
		return result, known
	case <-time.After(3 * time.Second):
		t.Fatal("execution did not finish")
		return Result{}, false
	}
}
func TestKagentCallerToHTTPConsumerAndDurableContinuation(t *testing.T) {
	f := newSliceHTTPFixture(t)
	result, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt-1"), context.Background()))
	if !known || result.Status != "completed" || result.SessionID != sliceSessionID || result.Output != "native response" {
		t.Fatalf("unexpected result %+v", result)
	}
	if _, known = finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt-1"), context.Background())); !known {
		t.Fatal("recovery failed")
	}
	followup, err := f.backend("attempt-2").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief", ResumeSessionID: result.SessionID, ResumeExpected: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, known = finishSliceKagent(t, followup); !known {
		t.Fatal("followup failed")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 2 {
		t.Fatalf("duplicate execution: %d sends", len(f.sends))
	}
	if f.sends[1].ContextID != result.SessionID {
		t.Fatal("followup lost context")
	}
	if f.sends[0].Parts[0].Text != "inline runtime brief\n\nharmless question" {
		t.Fatal("brief not delivered")
	}
	if f.sends[0].MessageID == f.sends[1].MessageID {
		t.Fatal("attempts reused message ID")
	}
}

type kagentContractServer struct {
	URL     string
	handler http.Handler
}

func newKagentContractServer(h http.Handler) *kagentContractServer {
	return &kagentContractServer{"https://kagent.test", h}
}
func (s *kagentContractServer) Close() {}
func (s *kagentContractServer) Client() *http.Client {
	return &http.Client{Transport: kagentHandlerTransport{s.handler}}
}

type kagentHandlerTransport struct{ handler http.Handler }

func (t kagentHandlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	w := httptest.NewRecorder()
	t.handler.ServeHTTP(w, r)
	return w.Result(), nil
}
