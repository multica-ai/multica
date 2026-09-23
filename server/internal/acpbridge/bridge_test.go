package acpbridge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/interaction"
	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const ws = "11111111-1111-4111-8111-111111111111"
const issue = "22222222-2222-4222-8222-222222222222"
const worker = "33333333-3333-4333-8333-333333333333"
const runID = "44444444-4444-4444-8444-444444444444"

type fakeAPI struct {
	mu     sync.Mutex
	r      run
	record interaction.Record
	posts  []string
	denied bool
}

func fixture() *fakeAPI {
	return &fakeAPI{r: run{ID: runID, IssueID: issue, AgentID: worker, Status: "running", WorkDir: "/test/worktree", CreatedAt: "2026-09-22T00:00:00Z"}, record: interaction.Record{Owner: "owner", State: agent.InteractionSnapshot{State: agent.InteractionAwaitingInput, Activity: 1}, Deadline: time.Now().Add(time.Hour), UpdatedAt: time.Now(), Commands: []interaction.Input{}}}
}
func copyJSON(in, out any) error {
	if out == nil {
		return nil
	}
	data, err := json.Marshal(in)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}
func (f *fakeAPI) GetJSON(_ context.Context, path string, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denied {
		return fmt.Errorf("access revoked")
	}
	switch {
	case path == "/api/agents":
		return copyJSON([]map[string]string{{"id": worker, "name": "Worker"}}, out)
	case path == "/api/agents/"+worker:
		return copyJSON(map[string]string{"id": worker}, out)
	case path == "/api/agents/"+worker+"/tasks", path == "/api/issues/"+issue+"/task-runs":
		return copyJSON([]run{f.r}, out)
	case path == "/api/issues/"+issue:
		return copyJSON(map[string]string{"title": "Test issue", "identifier": "TEST-1"}, out)
	case strings.HasSuffix(path, "/interaction"):
		return copyJSON(f.record, out)
	case strings.Contains(path, "/messages?"):
		messages := []protocol.TaskMessagePayload{}
		if strings.HasSuffix(path, "since=0") {
			messages = append(messages, protocol.TaskMessagePayload{TaskID: runID, Seq: 1, Type: "text", Content: "existing worker output"})
		}
		return copyJSON(messages, out)
	default:
		return fmt.Errorf("unexpected GET %s", path)
	}
}
func (f *fakeAPI) PostJSON(_ context.Context, path string, body, out any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.denied {
		return fmt.Errorf("access revoked")
	}
	f.posts = append(f.posts, path)
	if strings.HasSuffix(path, "/cancel") {
		f.r.Status = "cancelled"
		return nil
	}
	if strings.HasSuffix(path, "/follow-up") {
		return copyJSON(map[string]string{"run_id": "next"}, out)
	}
	var cmd struct {
		agent.InteractionCommand
		Owner string `json:"owner"`
	}
	if err := copyJSON(body, &cmd); err != nil {
		return err
	}
	if cmd.Owner != f.record.Owner {
		return fmt.Errorf("wrong owner")
	}
	if err := f.record.Accept(cmd.InteractionCommand, "human", time.Now()); err != nil {
		return err
	}
	switch cmd.Kind {
	case "input":
		f.record.State.Activity++
		f.record.State.State = agent.InteractionWorking
	case "interrupt":
		f.record.State.State = agent.InteractionAwaitingInput
	case "finish":
		f.r.Status = "completed"
		f.record.State.State = agent.InteractionFinished
	}
	f.record.Commands[len(f.record.Commands)-1].Receipt = &agent.InteractionReceipt{Outcome: "applied", Snapshot: f.record.State}
	return copyJSON(f.record, out)
}

type harness struct {
	t        *testing.T
	in       *json.Encoder
	messages chan map[string]any
	close    func()
}

func startBridge(t *testing.T, f *fakeAPI) *harness {
	t.Helper()
	input, send := io.Pipe()
	receive, output := io.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	b := New(f, ws)
	b.poll = 5 * time.Millisecond
	done := make(chan error, 1)
	go func() { done <- b.Serve(ctx, input, output); output.Close() }()
	h := &harness{t: t, in: json.NewEncoder(send), messages: make(chan map[string]any, 100)}
	go func() {
		scanner := bufio.NewScanner(receive)
		for scanner.Scan() {
			var v map[string]any
			if json.Unmarshal(scanner.Bytes(), &v) == nil {
				h.messages <- v
			}
		}
		close(h.messages)
	}()
	h.close = func() {
		cancel()
		send.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("bridge failed to detach")
		}
		receive.Close()
	}
	t.Cleanup(h.close)
	return h
}
func (h *harness) send(id int, method string, params any) {
	h.t.Helper()
	if err := h.in.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params}); err != nil {
		h.t.Fatal(err)
	}
}
func (h *harness) response(id int) map[string]any {
	h.t.Helper()
	timeout := time.NewTimer(3 * time.Second)
	defer timeout.Stop()
	for {
		select {
		case v, ok := <-h.messages:
			if !ok {
				h.t.Fatal("connection ended")
			}
			if v["id"] == float64(id) {
				return v
			}
		case <-timeout.C:
			h.t.Fatalf("no response for %d", id)
		}
	}
}
func (h *harness) load() {
	h.send(1, "session/load", map[string]string{"sessionId": conversationID(ws, issue, worker)})
	if v := h.response(1); v["error"] != nil {
		h.t.Fatal(v)
	}
}

func TestDiscoveryAndLoadNeverExecute(t *testing.T) {
	f := fixture()
	h := startBridge(t, f)
	h.send(2, "initialize", map[string]int{"protocolVersion": 1})
	if h.response(2)["error"] != nil {
		t.Fatal("initialize")
	}
	h.send(3, "session/list", map[string]string{"cwd": "/test"})
	v := h.response(3)
	list := v["result"].(map[string]any)["sessions"].([]any)
	if len(list) != 1 || !strings.Contains(list[0].(map[string]any)["title"].(string), "TEST-1") {
		t.Fatal(v)
	}
	h.load()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 0 {
		t.Fatal("opening executed work", f.posts)
	}
}
func TestStopInterruptsSameRunAndPromptEndsWithoutCancellingRun(t *testing.T) {
	f := fixture()
	h := startBridge(t, f)
	h.load()
	id := conversationID(ws, issue, worker)
	h.send(4, "session/prompt", map[string]any{"sessionId": id, "prompt": []map[string]string{{"type": "text", "text": "continue"}}})
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		working := f.record.State.State == agent.InteractionWorking
		f.mu.Unlock()
		if working {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("input not delivered")
		}
		time.Sleep(time.Millisecond)
	}
	h.send(5, "session/cancel", map[string]string{"sessionId": id})
	// Responses may arrive in either order; collect both rather than dropping one.
	found := map[int]map[string]any{}
	for len(found) < 2 {
		select {
		case v := <-h.messages:
			if n, ok := v["id"].(float64); ok && (n == 4 || n == 5) {
				found[int(n)] = v
			}
		case <-time.After(3 * time.Second):
			t.Fatal("cancel timeout")
		}
	}
	if found[4]["error"] != nil || found[4]["result"].(map[string]any)["stopReason"] != "cancelled" {
		t.Fatal(found)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.r.Status != "running" || len(f.record.Commands) != 2 || f.record.Commands[1].Kind != "interrupt" {
		t.Fatal("Stop cancelled run", f.record)
	}
}
func TestRunSlashCommandsAreDirectAndDistinct(t *testing.T) {
	for _, command := range []string{"/finish-run", "/cancel-run", "/status"} {
		t.Run(command, func(t *testing.T) {
			f := fixture()
			h := startBridge(t, f)
			h.load()
			h.send(2, "session/prompt", map[string]any{"sessionId": conversationID(ws, issue, worker), "prompt": []map[string]string{{"type": "text", "text": command}}})
			if v := h.response(2); v["error"] != nil {
				t.Fatal(v)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if command == "/status" && len(f.posts) > 0 {
				t.Fatal("status mutated state")
			}
			for _, c := range f.record.Commands {
				if c.Kind == "input" {
					t.Fatal("slash command reached model")
				}
			}
			if command == "/finish-run" && f.r.Status != "completed" {
				t.Fatal("not finished")
			}
			if command == "/cancel-run" && f.r.Status != "cancelled" {
				t.Fatal("not cancelled")
			}
		})
	}
}
func TestWrongWorkspaceAndUnsupportedContentFailClosed(t *testing.T) {
	f := fixture()
	h := startBridge(t, f)
	h.send(2, "session/load", map[string]string{"sessionId": conversationID(worker, issue, worker)})
	if h.response(2)["error"] == nil {
		t.Fatal("cross workspace allowed")
	}
	h.load()
	h.send(3, "session/prompt", map[string]any{"sessionId": conversationID(ws, issue, worker), "prompt": []map[string]string{{"type": "image", "text": "not text"}}})
	if h.response(3)["error"] == nil {
		t.Fatal("image accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 0 {
		t.Fatal("invalid request executed")
	}
}
func TestTerminalConversationRequiresExplicitFollowup(t *testing.T) {
	f := fixture()
	f.r.Status = "completed"
	h := startBridge(t, f)
	h.load()
	id := conversationID(ws, issue, worker)
	h.send(2, "session/prompt", map[string]any{"sessionId": id, "prompt": []map[string]string{{"type": "text", "text": "hello"}}})
	if h.response(2)["error"] == nil {
		t.Fatal("implicitly started run")
	}
	h.send(3, "session/prompt", map[string]any{"sessionId": id, "prompt": []map[string]string{{"type": "text", "text": "/follow-up hello"}}})
	if v := h.response(3); v["error"] != nil {
		t.Fatal(v)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 1 || !strings.HasSuffix(f.posts[0], "/follow-up") {
		t.Fatal(f.posts)
	}
}

func TestReloadDetachesAndReplaysWithoutMutatingWorker(t *testing.T) {
	f := fixture()
	h := startBridge(t, f)
	h.load()
	h.load()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 0 || f.r.Status != "running" {
		t.Fatal("reload changed worker")
	}
}

func TestRevokedAccessBlocksCommands(t *testing.T) {
	f := fixture()
	h := startBridge(t, f)
	h.load()
	f.mu.Lock()
	f.denied = true
	f.mu.Unlock()
	h.send(2, "session/prompt", map[string]any{"sessionId": conversationID(ws, issue, worker), "prompt": []map[string]string{{"type": "text", "text": "hello"}}})
	if h.response(2)["error"] == nil {
		t.Fatal("revoked access still sent input")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.posts) != 0 {
		t.Fatal("posted after access revoked")
	}
}
