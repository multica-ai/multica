package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/multica-ai/multica/server/internal/localconversation/computer"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/contextmcp"
)

// fakeComputerHost stands in for Desktop's main process.
type fakeComputerHost struct {
	capability *ComputerCapability
	control    func(ctx context.Context, version string, envelope map[string]any) (json.RawMessage, error)
}

func (f *fakeComputerHost) Computer() (ComputerCapability, bool) {
	if f.capability == nil {
		return ComputerCapability{}, false
	}
	return *f.capability, true
}

func (f *fakeComputerHost) Control(ctx context.Context, version string, envelope map[string]any) (json.RawMessage, error) {
	return f.control(ctx, version, envelope)
}

func testComputerCapability() *ComputerCapability {
	return &ComputerCapability{Version: computer.CapabilityVersion, UserID: "u",
		Prompts: ComputerPrompts{Conversation: "conversation guide", Task: "task guide", TaskBrief: "[Computer Use] brief"}}
}

func TestComputerToolsBindHostIdentityAndDoNotReplayRequests(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "c", "claude")
	conv, err := f.svc.store.Conversation(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.svc.SetComputerHost(&fakeComputerHost{capability: testComputerCapability(), control: func(_ context.Context, version string, p map[string]any) (json.RawMessage, error) {
		calls++
		assertContractEnvelope(t, p)
		scope, _ := p["scope"].(map[string]string)
		if version != computer.CapabilityVersion || p["conversationId"] != "c" || p["turnId"] != "turn-1" || p["op"] != "begin" || scope["userId"] != "u" || p["origin"] != nil {
			t.Errorf("wrong host identity: version=%s %v", version, p)
		}
		return nil, errors.New("app_not_allowed")
	}})
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
	capability := testComputerCapability()
	capability.Runtime = computer.Command{Executable: exe, Entry: entry}
	conv, err := f.svc.store.Conversation(context.Background(), "c")
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	f.svc.SetComputerHost(&fakeComputerHost{capability: capability, control: func(_ context.Context, _ string, p map[string]any) (json.RawMessage, error) {
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
	}})
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
	f.svc.SetComputerHost(&fakeComputerHost{capability: testComputerCapability(), control: func(ctx context.Context, _ string, p map[string]any) (json.RawMessage, error) {
		if p["op"] != "release" {
			t.Error("wrong host operation")
		}
		close(entered)
		select {
		case <-release:
			return json.RawMessage(`{}`), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}})
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

func TestBindTaskComputerRequiresDesktopSignedInAsTheTaskAccount(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	var got map[string]any
	host := &fakeComputerHost{control: func(_ context.Context, _ string, p map[string]any) (json.RawMessage, error) {
		got = p
		return json.RawMessage(`{"available":true}`), nil
	}}
	f.svc.SetComputerHost(host)
	if _, _, ok, err := f.svc.BindTaskComputer("t1", "w1", "u"); ok || err != nil {
		t.Fatalf("bound without a Desktop capability: ok=%v err=%v", ok, err)
	}
	host.capability = testComputerCapability()
	for _, account := range []string{"", "someone-else"} {
		if _, _, ok, _ := f.svc.BindTaskComputer("t1", "w1", account); ok {
			t.Fatalf("bound for daemon account %q while Desktop is signed in as u", account)
		}
	}
	bound, release, ok, err := f.svc.BindTaskComputer("t1", "w1", "u")
	if !ok || err != nil || bound.Brief != "[Computer Use] brief" {
		t.Fatalf("bound=%+v ok=%v err=%v", bound, ok, err)
	}
	call := func(body string) (int, string) {
		res, err := http.Post(bound.URL, "application/json", bytes.NewBufferString(body))
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out bytes.Buffer
		_, _ = out.ReadFrom(res.Body)
		return res.StatusCode, out.String()
	}
	if _, body := call(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`); !strings.Contains(body, "task guide") {
		t.Fatalf("task route lacks Desktop's task instructions: %s", body)
	}
	if status, _ := call(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"computer_status","arguments":{}}}`); status != http.StatusOK {
		t.Fatalf("status call = %d", status)
	}
	assertContractEnvelope(t, got)
	scope, _ := got["scope"].(map[string]string)
	if got["op"] != "status" || got["origin"] != "task" || got["conversationId"] != "task:t1" || got["turnId"] != "t1" || scope["workspaceId"] != "w1" || scope["userId"] != "u" {
		t.Fatalf("task envelope = %v", got)
	}
	release()
	if status, _ := call(`{"jsonrpc":"2.0","id":3,"method":"tools/list"}`); status != http.StatusUnauthorized {
		t.Fatalf("released route answered %d", status)
	}
}

func TestSystemPromptCarriesDesktopComputerTextOnlyWhenOffered(t *testing.T) {
	t.Parallel()
	if prompt := systemPrompt("ws-1", true, ""); strings.Contains(prompt, "computer_execute") || strings.Contains(prompt, "\n\n\n\n") {
		t.Fatalf("Computer Use text without a Desktop capability:\n%s", prompt)
	}
	if !strings.Contains(systemPrompt("ws-1", true, "desktop computer text"), "desktop computer text") {
		t.Fatal("Desktop's Computer Use text missing")
	}
}

// assertContractEnvelope checks that an envelope only uses fields the shared
// Computer contract defines, which Desktop's broker validates strictly.
func assertContractEnvelope(t *testing.T, envelope map[string]any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "packages", "computer-use", "contract", "computer.json"))
	if err != nil {
		t.Fatal(err)
	}
	var contract struct {
		Control struct {
			Envelope []string `json:"envelope"`
			Scope    []string `json:"scope"`
		} `json:"control"`
	}
	if err := json.Unmarshal(raw, &contract); err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{}
	for _, key := range contract.Control.Envelope {
		allowed[key] = true
	}
	for key := range envelope {
		if !allowed[key] {
			t.Errorf("envelope field %q is not in the Computer contract", key)
		}
	}
	scope, _ := envelope["scope"].(map[string]string)
	if len(scope) != len(contract.Control.Scope) {
		t.Errorf("scope %v does not carry the contract's fields %v", scope, contract.Control.Scope)
	}
}
