//go:build integration

package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// Run: E2B_API_KEY=xxx go test ./internal/sandbox/ -tags integration -run TestE2BPollDebug -v -timeout 5m
func TestE2BPollDebug(t *testing.T) {
	apiKey := os.Getenv("E2B_API_KEY")
	if apiKey == "" {
		t.Skip("E2B_API_KEY not set")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	provider := NewE2BProvider(apiKey)

	// Create sandbox
	t.Log("Creating sandbox...")
	sb, err := provider.CreateOrConnect(ctx, "", CreateOpts{
		TemplateID: "9q4awrmowr11d4qpxuu3",
		Timeout:    5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("Create sandbox: %v", err)
	}
	t.Logf("Sandbox: %s", sb.ID)
	defer provider.Destroy(context.Background(), sb.ID)

	// Ensure opencode serve
	t.Log("Starting opencode serve...")
	provider.Exec(ctx, sb, []string{"sh", "-c", "nohup opencode serve --port 4096 > /tmp/oc-serve.log 2>&1 &"})
	time.Sleep(3 * time.Second)

	// Test 1: Can we list sessions via Exec?
	t.Log("=== Test: List sessions via Exec ===")
	stdout, err := provider.Exec(ctx, sb, []string{"curl", "-s", "http://localhost:4096/session"})
	t.Logf("Exec stdout (len=%d): %q", len(stdout), stdout)
	if err != nil {
		t.Fatalf("List sessions: %v", err)
	}

	var sessions []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal([]byte(stdout), &sessions); err != nil {
		t.Fatalf("Parse sessions: %v (raw: %q)", err, stdout)
	}
	t.Logf("Found %d sessions", len(sessions))

	// Test 2: Write prompt and launch opencode run
	t.Log("=== Test: Launch opencode run ===")
	provider.WriteFile(ctx, sb, "/tmp/prompt.txt", []byte("Say hello world"))
	provider.Exec(ctx, sb, []string{"sh", "-c", "mkdir -p /workspace/debug && nohup opencode run --attach http://localhost:4096 --dir /workspace/debug --format json < /tmp/prompt.txt > /tmp/oc-run.log 2>&1 &"})

	// Test 3: Discover session
	t.Log("=== Test: Discover session ===")
	var sessionID string
	for i := 0; i < 10; i++ {
		time.Sleep(2 * time.Second)
		stdout, err := provider.Exec(ctx, sb, []string{"curl", "-s", "http://localhost:4096/session"})
		if err != nil {
			t.Logf("  poll %d: exec error: %v", i, err)
			continue
		}
		t.Logf("  poll %d: stdout len=%d", i, len(stdout))

		var sess []struct {
			ID        string `json:"id"`
			Directory string `json:"directory"`
		}
		if err := json.Unmarshal([]byte(stdout), &sess); err != nil {
			t.Logf("  poll %d: parse error: %v", i, err)
			continue
		}
		for _, s := range sess {
			if s.Directory == "/workspace/debug" {
				sessionID = s.ID
				break
			}
		}
		if sessionID != "" {
			break
		}
	}
	if sessionID == "" {
		t.Fatal("Session not found after 20s")
	}
	t.Logf("Session ID: %s", sessionID)

	// Test 4: Poll messages
	t.Log("=== Test: Poll messages ===")
	for i := 0; i < 10; i++ {
		time.Sleep(3 * time.Second)
		stdout, err := provider.Exec(ctx, sb, []string{"curl", "-s", fmt.Sprintf("http://localhost:4096/session/%s/message", sessionID)})
		if err != nil {
			t.Logf("  msg poll %d: exec error: %v", i, err)
			continue
		}
		t.Logf("  msg poll %d: stdout len=%d, first 200: %q", i, len(stdout), truncate(stdout, 200))

		// Parse with our poller
		poller := NewSessionPoller(provider, sb)
		status, err := poller.parseResponse(stdout)
		if err != nil {
			t.Logf("  msg poll %d: parse error: %v", i, err)
			continue
		}
		t.Logf("  msg poll %d: state=%s, messages=%d, usage={in:%d,out:%d}", i, status.State, len(status.Messages), status.Usage.InputTokens, status.Usage.OutputTokens)

		if status.State == SessionIdle {
			t.Log("=== Session completed! ===")
			return
		}
	}
	t.Fatal("Session did not complete within timeout")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
