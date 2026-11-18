//go:build !windows

package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/kagent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const wiringAgentID = "12345678-1234-1234-1234-123456789abc"
const wiringSessionID = "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"

func kagentClaim() Task {
	return Task{StartClaimSupported: true, ID: "task-1", AgentID: wiringAgentID, WorkspaceID: "workspace", RuntimeID: "runtime", DispatchedAt: "2026-11-18T00:00:00.123456Z",
		ChatSessionID: "chat", ChatMessage: "hello", Agent: &AgentData{ID: wiringAgentID, Name: "Native DEV", Instructions: "Be concise.", RuntimeConfig: json.RawMessage(`{"mode":"native-text"}`)}}
}

func TestKagentTaskRejectsUnsupportedBeforeDispatch(t *testing.T) {
	for name, change := range map[string]func(*Task){
		"credentials":      func(t *Task) { t.AuthToken = "mat_secret" },
		"mcp":              func(t *Task) { t.Agent.McpConfig = json.RawMessage(`{"servers":{}}`) },
		"repository":       func(t *Task) { t.Repos = []RepoData{{URL: "https://repo"}} },
		"skills":           func(t *Task) { t.Agent.SkillRefs = []SkillRefData{{ID: "skill"}} },
		"model":            func(t *Task) { t.Agent.Model = "override" },
		"environment":      func(t *Task) { t.Agent.CustomEnv = map[string]string{"TOKEN": "secret"} },
		"issue":            func(t *Task) { t.IssueID = "issue" },
		"claim generation": func(t *Task) { t.DispatchedAt = "" },
		"attachment":       func(t *Task) { t.ChatMessageAttachments = []ChatAttachmentMeta{{ID: "attachment"}} },
	} {
		t.Run(name, func(t *testing.T) {
			task := kagentClaim()
			change(&task)
			// No client/config: passing preflight would attempt I/O or panic.
			if _, err := (&Daemon{}).runTask(context.Background(), task, "kagent", 0, slog.Default()); err == nil {
				t.Fatal("unsupported input accepted")
			}
		})
	}
	if err := validateKagentTask(kagentClaim()); err != nil {
		t.Fatal(err)
	}
}

type kagentWiringTransport func(*http.Request) (*http.Response, error)

func (f kagentWiringTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func configureWiringKagent(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("test-caller"), 0600); err != nil {
		t.Fatal(err)
	}
	c := kagentDaemonConfig{JournalDir: filepath.Join(dir, "journal"), KubernetesEndpoint: "https://management.test", A2AEndpoint: "https://a2a.test", KubernetesTokenFile: token, A2ATokenFile: token,
		Namespace: "test", Harness: "dedicated-readonly", ModelConfig: "test-model", Agents: []kagentEnrollment{{AgentID: wiringAgentID, WorkspaceID: "workspace"}}}
	raw, _ := json.Marshal(c)
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "1")
	t.Setenv("MULTICA_KAGENT_CONFIG", path)
}

// Exercises runTask -> trusted config -> ResolveBackend -> real lifecycle and
// alpha7 caller -> daemon drain, with HTTP handled in process (no live cluster).
func TestKagentDaemonDispatchAndOpaqueContinuation(t *testing.T) {
	configureWiringKagent(t)
	var remoteAgent *kagent.Agent
	var sends []kagent.RemoteMessage
	starts := 0
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Host == "multica.test" {
			if strings.HasSuffix(r.URL.Path, "/kagent/agents") {
				_ = json.NewEncoder(w).Encode([]kagentRuntimeEnrollment{{AgentID: wiringAgentID, Name: "Native DEV", WorkspaceID: "workspace", RuntimeID: "runtime", Revision: 1, Generation: 1, DesiredState: "present", Phase: "ready"}})
				return
			}
			if strings.HasSuffix(r.URL.Path, "/start") {
				starts++
				var body struct{ Capabilities []string }
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				if len(body.Capabilities) != 1 || body.Capabilities[0] != protocol.DaemonCapabilityKagentProcessCleanupV1 {
					t.Errorf("capabilities=%v", body.Capabilities)
				}
				_ = json.NewEncoder(w).Encode(map[string]string{"process_cleanup_capability": protocol.DaemonCapabilityKagentProcessCleanupV1})
			} else {
				_, _ = w.Write([]byte(`{}`))
			}
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-caller" {
			t.Error("missing caller identity")
		}
		if strings.HasPrefix(r.URL.Path, "/apis/") {
			if r.Method == "POST" {
				if err := json.NewDecoder(r.Body).Decode(&remoteAgent); err != nil {
					t.Error(err)
				}
				remoteAgent.Metadata.UID = "resource-uid"
				remoteAgent.Metadata.ResourceVersion = "1"
				remoteAgent.Metadata.Generation = 1
			} else if remoteAgent == nil {
				w.WriteHeader(404)
				return
			}
			remoteAgent.Status.Conditions = []kagent.Condition{{Type: kagent.ConditionReady, Status: "True", ObservedGeneration: 1}}
			_ = json.NewEncoder(w).Encode(remoteAgent)
			return
		}
		var rpc struct {
			ID     string
			Method string
			Params struct{ Message kagent.RemoteMessage }
		}
		if err := json.NewDecoder(r.Body).Decode(&rpc); err != nil {
			t.Error(err)
		}
		if rpc.Method != "SendMessage" {
			t.Errorf("unexpected RPC %s", rpc.Method)
		}
		sends = append(sends, rpc.Params.Message)
		result := &kagent.RemoteTask{ID: "remote-task", ContextID: wiringSessionID}
		result.Status.State = kagent.StateCompleted
		result.Status.Message = &kagent.RemoteMessage{Role: "ROLE_AGENT", Parts: []kagent.Part{{Text: "native answer"}}}
		_ = json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": rpc.ID, "result": map[string]any{"task": result}})
	})
	previous := http.DefaultTransport
	http.DefaultTransport = kagentWiringTransport(func(r *http.Request) (*http.Response, error) {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, r)
		resp := rec.Result()
		resp.Request = r
		return resp, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previous })
	newDaemon := func() *Daemon {
		return &Daemon{client: NewClient("http://multica.test"), logger: slog.Default(),
			workspaces:   map[string]*workspaceState{"workspace": {workspaceID: "workspace", runtimeIDs: []string{"runtime"}}},
			runtimeIndex: map[string]Runtime{"runtime": {ID: "runtime", Provider: "kagent"}}}
	}
	d := newDaemon()
	if err := d.kagentReconcileRound(context.Background()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		task := kagentClaim()
		if i == 1 {
			task.ID = "task-2"
			task.PriorSessionID = wiringSessionID
		}
		// A new daemon instance shares only durable configuration/journal.
		out, err := d.runTask(context.Background(), task, "kagent", 0, slog.Default())
		if err != nil || out.Status != "completed" || out.SessionID != wiringSessionID || out.Comment != "native answer" {
			t.Fatalf("result=%+v err=%v", out, err)
		}
		d = newDaemon()
	}
	if starts != 2 || len(sends) != 2 || sends[0].ContextID != "" || sends[1].ContextID != wiringSessionID {
		t.Fatalf("starts=%d sends=%+v", starts, sends)
	}
	if input := sends[0].Parts[0].Text; !strings.Contains(input, "Be concise.") || !strings.Contains(input, "No checkout") || !strings.HasSuffix(input, "hello") {
		t.Fatalf("missing inline brief: %s", input)
	}
	// Verify a credential is never embedded in the remote user content.
	for _, msg := range sends {
		raw, _ := json.Marshal(msg)
		if strings.Contains(string(raw), "test-caller") {
			t.Fatal("caller credential in prompt")
		}
	}
}

func TestKagentStartRequiresCleanupAcknowledgement(t *testing.T) {
	c := NewClient("http://multica.test")
	c.client.Transport = kagentWiringTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{}`)), Header: make(http.Header)}, nil
	})
	if _, err := c.StartTask(context.Background(), kagentClaim(), protocol.DaemonCapabilityKagentProcessCleanupV1); err == nil {
		t.Fatal("old server accepted remote dispatch")
	}
}

func TestKagentEnrollmentAndDisableGate(t *testing.T) {
	configureWiringKagent(t)
	c, err := loadKagentDaemonConfig()
	if err != nil {
		t.Fatal(err)
	}
	task := kagentClaim()
	task.WorkspaceID = "other-workspace"
	if _, err := c.backendConfig(task); err == nil {
		t.Fatal("cross-workspace enrollment accepted")
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	if _, err := loadKagentDaemonConfig(); err == nil {
		t.Fatal("disabled provider accepted")
	}
}

func TestKagentProbeUsesConfigurationWithoutCLI(t *testing.T) {
	configureWiringKagent(t)
	d := &Daemon{}
	probe := d.probeBuiltinRuntime(context.Background(), "kagent", AgentEntry{Path: "/missing/must-not-execute"})
	if probe.verdict != builtinProbeOK || probe.version != "alpha7-native-text-dev" || probe.reason != "" {
		t.Fatalf("version=%s reason=%s verdict=%v", probe.version, probe.reason, probe.verdict)
	}
	t.Setenv("MULTICA_KAGENT_ENABLED", "")
	if probe := d.probeBuiltinRuntime(context.Background(), "kagent", AgentEntry{}); probe.verdict == builtinProbeOK {
		t.Fatal("disabled runtime advertised")
	}
}
