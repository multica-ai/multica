package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/multica-ai/multica/server/internal/localconversation/computer"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
)

func TestComputerToolsBindHostIdentityAndDoNotReplayRequests(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "c", "claude")
	conv, err := f.svc.store.Conversation(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.svc.SetComputerHost(func(_ context.Context, scopeID int64, id, kind string, payload any) (json.RawMessage, error) {
		calls++
		p := payload.(map[string]any)
		if scopeID != f.scopeID || id != "c" || kind != "computer.control" || p["turnId"] != "turn-1" || p["op"] != "begin" {
			t.Errorf("wrong host identity: %v", p)
		}
		return nil, errors.New("app_not_allowed")
	})
	tools := map[string]contextmcp.Tool{}
	for _, tool := range f.svc.computerToolsFor(conv, func() map[string]any { return map[string]any{"turn_id": "turn-1"} }) {
		tools[tool.Name] = tool
	}
	tool := tools["computer_execute"]
	for i := 0; i < 2; i++ {
		_, err := tool.Call(context.Background(), json.RawMessage(`{"code":"doNotRun()","request_id":"one"}`))
		if err == nil {
			t.Fatal("host rejection ignored")
		}
	}
	if calls != 1 {
		t.Fatalf("replayed host request: %d", calls)
	}
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"code":"different()","request_id":"one"}`)); err == nil {
		t.Fatal("conflicting retry accepted")
	}
	if _, err := tool.Call(context.Background(), json.RawMessage(`{"code":"x","request_id":"two","approved":true}`)); err == nil {
		t.Fatal("forged approval accepted")
	}
	// The rejected turn has nothing to release; avoid an asynchronous test callback.
	f.svc.computers.mu.Lock()
	delete(f.svc.computers.active, "c")
	f.svc.computers.mu.Unlock()
}

func TestBuiltComputerToolRoundTrip(t *testing.T) {
	exe, entry := os.Getenv("MULTICA_TEST_COMPUTER_EXECUTABLE"), os.Getenv("MULTICA_TEST_COMPUTER_ENTRY")
	if runtime.GOOS != "darwin" || exe == "" || entry == "" {
		t.Skip("supply built runtime paths; desktop actions remain fake")
	}
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "c", "claude")
	f.svc.computer.Command = computer.Command{Executable: exe, Entry: entry}
	conv, err := f.svc.store.Conversation(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.svc.SetComputerHost(func(_ context.Context, _ int64, _ string, _ string, payload any) (json.RawMessage, error) {
		p := payload.(map[string]any)
		if p["op"] != "action" {
			return json.RawMessage(`{"status":"ready"}`), nil
		}
		calls++
		var action map[string]any
		_ = json.Unmarshal(p["action"].(json.RawMessage), &action)
		switch action["op"] {
		case "get_app":
			return json.RawMessage(`{"id":"fixture.app","name":"Fixture"}`), nil
		case "observe":
			return json.RawMessage(`{"observationId":"fixture-observation","elements":[],"image":{"mimeType":"image/png","data":"aGk="}}`), nil
		}
		return nil, errors.New("unexpected action")
	})
	tools := map[string]contextmcp.Tool{}
	for _, tool := range f.svc.computerToolsFor(conv, func() map[string]any { return map[string]any{"turn_id": "turn"} }) {
		tools[tool.Name] = tool
	}
	input := json.RawMessage(`{"request_id":"observe","code":"let app = await computer.getApp('Fixture'); await app.observe();"}`)
	for n := 0; n < 2; n++ {
		result, err := tools["computer_execute"].Call(context.Background(), input)
		if err != nil || result.Image == nil {
			t.Fatalf("observe=%+v err=%v", result, err)
		}
		value := result.Value.(map[string]any)
		if value["status"] != "completed" || value["actions"] != 2 {
			t.Fatalf("value=%v", value)
		}
	}
	if calls != 2 {
		t.Fatalf("replayed actions=%d", calls)
	}
	result, err := tools["computer_execute"].Call(context.Background(), json.RawMessage(`{"request_id":"persist","code":"app.id"}`))
	if err != nil || !strings.Contains(result.Value.(map[string]any)["value"].(string), "fixture.app") {
		t.Fatalf("variables=%+v err=%v", result, err)
	}
	if _, err := tools["computer_reset"].Call(context.Background(), json.RawMessage(`{"request_id":"reset"}`)); err != nil {
		t.Fatal(err)
	}
}

func TestComputerResetWaitsForReleaseAndStopGuardsTurn(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "c", "claude")
	conv, err := f.svc.store.Conversation(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.svc.computers.active["c"] = &computerExecution{turn: "turn", scopeID: f.scopeID, payload: map[string]any{"op": "begin"}, cancel: cancel}
	f.svc.StopComputer("c", "old-turn")
	if ctx.Err() != nil {
		t.Fatal("late stop cancelled a different turn")
	}
	entered, release := make(chan struct{}), make(chan struct{})
	f.svc.SetComputerHost(func(ctx context.Context, _ int64, _ string, _ string, p any) (json.RawMessage, error) {
		if p.(map[string]any)["op"] != "release" {
			t.Error("wrong host operation")
		}
		close(entered)
		select {
		case <-release:
			return json.RawMessage(`{}`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	var reset contextmcp.Tool
	for _, tool := range f.svc.computerToolsFor(conv, func() map[string]any { return map[string]any{"turn_id": "turn"} }) {
		if tool.Name == "computer_reset" {
			reset = tool
		}
	}
	done := make(chan error, 1)
	go func() {
		_, err := reset.Call(context.Background(), json.RawMessage(`{"request_id":"reset"}`))
		done <- err
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("release not requested")
	}
	select {
	case <-done:
		t.Fatal("reset returned before release")
	default:
	}
	if ctx.Err() == nil {
		t.Fatal("reset did not cancel the old execution")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
