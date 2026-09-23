package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/agent"
)

// Test-only RPC executable. This never resolves Pi or touches an account.
func init() {
	if os.Getenv("MULTICA_TEST_DAEMON_INTERACTIVE") != "1" {
		return
	}
	scanner := bufio.NewScanner(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	prompts := 0
	for scanner.Scan() {
		var c map[string]any
		if json.Unmarshal(scanner.Bytes(), &c) != nil {
			os.Exit(2)
		}
		response := map[string]any{"type": "response", "id": c["id"], "success": true}
		switch c["type"] {
		case "get_state":
			response["data"] = map[string]any{"isStreaming": false}
		case "prompt":
			prompts++
			if prompts == 2 {
				encoder.Encode(map[string]any{"type": "turn_end", "message": map[string]any{"stopReason": "stop"}})
				encoder.Encode(map[string]any{"type": "agent_settled"})
			}
		case "clear_queue":
			response["data"] = map[string]any{"steering": []string{}, "followUp": []string{}}
		case "abort":
			encoder.Encode(map[string]any{"type": "turn_end", "message": map[string]any{"stopReason": "aborted"}})
			encoder.Encode(map[string]any{"type": "agent_settled"})
		default:
			os.Exit(3)
		}
		encoder.Encode(response)
	}
	os.Exit(0)
}

func TestInteractiveBridgeDurableInterruptAndContinuation(t *testing.T) {
	var mu sync.Mutex
	var record interaction.Record
	registrations := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/daemon/runtimes/runtime/tasks/task/interaction" {
			http.NotFound(w, r)
			return
		}
		var request interaction.Sync
		if json.NewDecoder(r.Body).Decode(&request) != nil {
			w.WriteHeader(400)
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if request.Register {
			registrations++
			record = interaction.Record{Owner: request.Owner, Deadline: time.Now().Add(10 * time.Second)}
		}
		if ack := request.Acknowledgement; ack != nil {
			for i := range record.Commands {
				if record.Commands[i].ID == ack.ID {
					record.Commands[i] = *ack
				}
			}
		}
		record.State, record.UpdatedAt = request.State, time.Now()
		if request.Finish && !record.Pending() {
			record.Finishing = true
		}
		json.NewEncoder(w).Encode(record)
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	backend, err := agent.New("pi", agent.Config{ExecutablePath: os.Args[0], CLIVersion: "0.87.0", Env: map[string]string{"MULTICA_TEST_DAEMON_INTERACTIVE": "1"}, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	if err != nil {
		t.Fatal(err)
	}
	d := &Daemon{client: NewClient(server.URL)}
	path := filepath.Join(t.TempDir(), "session.jsonl")
	if err := os.WriteFile(path, []byte("{\"type\":\"session\"}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	session, err := d.startInteractiveExecution(ctx, backend.(agent.InteractiveBackend), "work", agent.ExecOptions{ResumeSessionID: path}, "task", "runtime")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		for range session.Messages {
		}
	}()
	wait := func(predicate func() bool) {
		t.Helper()
		for {
			mu.Lock()
			ok := predicate()
			mu.Unlock()
			if ok {
				return
			}
			select {
			case <-ctx.Done():
				t.Fatal("bridge timeout")
			case <-time.After(5 * time.Millisecond):
			}
		}
	}
	wait(func() bool { return record.State.State == agent.InteractionWorking })
	mu.Lock()
	err = record.Accept(agent.InteractionCommand{ID: "stop", Kind: "interrupt", Activity: 1}, "human", time.Now())
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	wait(func() bool { return record.State.State == agent.InteractionAwaitingInput && !record.Pending() })
	select {
	case <-session.Result:
		t.Fatal("pause ended run")
	default:
	}
	mu.Lock()
	err = record.Accept(agent.InteractionCommand{ID: "continue", Kind: "input", Text: "finish", Activity: 1}, "human", time.Now())
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	wait(func() bool {
		return record.State.Activity == 2 && record.State.State == agent.InteractionAwaitingInput && !record.Pending()
	})
	mu.Lock()
	err = record.Accept(agent.InteractionCommand{ID: "finish-run", Kind: "finish", Activity: 2}, "human", time.Now())
	mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "completed" {
			t.Fatalf("%+v", result)
		}
	case <-ctx.Done():
		t.Fatal("run never finished")
	}
	mu.Lock()
	defer mu.Unlock()
	if registrations != 1 || !record.Finishing || record.Pending() {
		t.Fatalf("unsafe final state: registrations=%d record=%+v", registrations, record)
	}
}

func TestInteractiveModeIsIssueAndProviderScoped(t *testing.T) {
	for _, tc := range []struct {
		name, provider, issue, config string
		want                          bool
	}{
		{"pi", "pi", "issue", `{"interactive_task_sessions":true}`, true},
		{"codex", "codex", "child-issue", `{"interactive_task_sessions":true}`, true},
		{"standalone", "pi", "", `{"interactive_task_sessions":true}`, false},
		{"unsupported", "claude", "issue", `{"interactive_task_sessions":true}`, false},
		{"disabled", "pi", "issue", `{}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := withInteractiveIssueRun(context.Background(), Task{IssueID: tc.issue, RuntimeID: "runtime", Agent: &AgentData{RuntimeConfig: json.RawMessage(tc.config)}}, tc.provider)
			_, enabled := ctx.Value(interactiveRunKey{}).(string)
			if enabled != tc.want {
				t.Fatalf("enabled=%v", enabled)
			}
		})
	}
}
