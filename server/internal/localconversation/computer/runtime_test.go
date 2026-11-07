package computer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Only a test-owned process is launched; no installed agent or desktop API.
func TestRuntimeFixtureProcess(t *testing.T) {
	if os.Getenv("MULTICA_COMPUTER_TEST_PROCESS") != "1" {
		return
	}
	encoder := json.NewEncoder(os.Stdout)
	_ = encoder.Encode(map[string]any{"type": "ready"})
	scanner := bufio.NewScanner(os.Stdin)
	count := 0
	for scanner.Scan() {
		var message map[string]any
		_ = json.Unmarshal(scanner.Bytes(), &message)
		if message["type"] != "execute" {
			continue
		}
		if message["code"] == "hang" {
			select {}
		}
		count++
		if message["code"] == "action" {
			_ = encoder.Encode(map[string]any{"type": "action", "id": "a", "executionId": message["id"], "action": map[string]any{"op": "observe", "app": "fixture"}})
			if !scanner.Scan() {
				break
			}
		}
		_ = encoder.Encode(map[string]any{"type": "result", "id": message["id"], "result": map[string]any{"value": fmt.Sprint(count)}})
	}
	os.Exit(0)
}
func runtimeFixture(t *testing.T) *Manager {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestRuntimeFixtureProcess$")
	cmd.Env = []string{"MULTICA_COMPUTER_TEST_PROCESS=1"}
	p, err := start(cmd)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.frames:
	case <-time.After(5 * time.Second):
		t.Fatal("fixture did not start")
	}
	p.version = CapabilityVersion
	m := &Manager{sessions: map[string]*process{"c": p}}
	t.Cleanup(m.Close)
	return m
}
func TestRuntimePersistsAndRoutesActions(t *testing.T) {
	m := runtimeFixture(t)
	called := false
	for n, code := range []string{"one", "action"} {
		r, restarted, err := m.Execute(context.Background(), "c", fixtureRuntime, fmt.Sprint(n), code, func(_ context.Context, raw json.RawMessage) (json.RawMessage, error) {
			called = true
			if !strings.Contains(string(raw), "fixture") {
				t.Fatal(string(raw))
			}
			return json.RawMessage(`{"ok":true}`), nil
		})
		if err != nil || restarted || r.Value != fmt.Sprint(n+1) {
			t.Fatalf("result=%+v err=%v", r, err)
		}
	}
	if !called {
		t.Fatal("desktop action not routed")
	}
}

var fixtureRuntime = Runtime{Version: CapabilityVersion}

func TestRuntimeKeepsPatchVersionsAndReplacesIncompatibleOnes(t *testing.T) {
	m := runtimeFixture(t)
	p := m.sessions["c"]
	patch := Runtime{Version: "1.0.9"}
	if _, restarted, err := m.Execute(context.Background(), "c", patch, "one", "one", nil); err != nil || restarted {
		t.Fatalf("patch difference replaced the REPL: restarted=%v err=%v", restarted, err)
	}
	_, restarted, err := m.Execute(context.Background(), "c", Runtime{Version: "1.1.0"}, "two", "one", nil)
	if !restarted || err == nil {
		t.Fatalf("minor difference kept the REPL: restarted=%v err=%v", restarted, err)
	}
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		t.Fatal("incompatible runtime survived")
	}
}

func TestCompatibleVersions(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"1.0.0", "1.0.7", true}, {"1.0.0", "1.1.0", false}, {"1.2.3", "2.2.3", false}, {"1.0", "1.0.0", false}, {"", "1.0.0", false}, {"1.0.0-rc", "1.0.0", false}} {
		if got := Compatible(c.a, c.b); got != c.want {
			t.Errorf("Compatible(%q, %q) = %v", c.a, c.b, got)
		}
	}
}

func TestRuntimeCancellationKillsTheProcess(t *testing.T) {
	m := runtimeFixture(t)
	p := m.sessions["c"]
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, _, err := m.Execute(ctx, "c", fixtureRuntime, "one", "hang", nil)
	if err == nil || !strings.Contains(err.Error(), "outcome_unknown") {
		t.Fatalf("cancel=%v", err)
	}
	select {
	case <-p.done:
	case <-time.After(3 * time.Second):
		t.Fatal("cancelled runtime survived")
	}
}
func TestSandboxDoesNotGrantUserFilesOrNetworking(t *testing.T) {
	p := SandboxProfile(Command{Executable: "/Applications/Multica.app/Contents/MacOS/Multica", Entry: "/Applications/Multica.app/Contents/Resources/host.js"})
	if !strings.Contains(p, "(deny default)") || strings.Contains(p, "allow network") || strings.Contains(p, "process-fork") || strings.Contains(p, "/Users/") {
		t.Fatal(p)
	}
}

func TestManagerAliveFollowsTheSession(t *testing.T) {
	t.Parallel()

	exited := make(chan struct{})
	close(exited)
	m := &Manager{sessions: map[string]*process{"1/conv": {done: make(chan struct{})}, "1/exited": {done: exited}}}
	if !m.Alive("1/conv") {
		t.Fatal("a running session reads as gone")
	}
	if m.Alive("1/other") || m.Alive("1/exited") {
		t.Fatal("a session that is not running reads as alive")
	}
}
