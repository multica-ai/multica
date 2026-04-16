//go:build integration

package cloud

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestOpenHarnessEndToEnd(t *testing.T) {
	daytonaKey := os.Getenv("DAYTONA_API_KEY")
	googleKey := os.Getenv("GOOGLE_AI_API_KEY")
	if daytonaKey == "" || googleKey == "" {
		t.Skip("DAYTONA_API_KEY and GOOGLE_AI_API_KEY required; skipping")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// 1. Initialize Daytona client
	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey: daytonaKey,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// 2. Build image with OpenHarness pre-installed
	image := daytona.DebianSlim(nil).
		PipInstall([]string{"openharness-ai==0.1.6"}).
		Env("TERM", "dumb")

	// Stream build logs for debugging
	logChan := make(chan string, 100)
	go func() {
		for line := range logChan {
			t.Logf("[build] %s", line)
		}
	}()

	t.Log("Creating sandbox with OpenHarness image (this may take a few minutes)...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		SandboxBaseParams: types.SandboxBaseParams{
			EnvVars: map[string]string{
				"OPENAI_API_KEY": googleKey,
				"TERM":           "dumb",
			},
		},
		Image: image,
	}, options.WithTimeout(5*time.Minute), options.WithLogChannel(logChan))
	if err != nil {
		t.Fatalf("Create sandbox: %v", err)
	}
	t.Logf("Sandbox created: id=%s", sandbox.ID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		t.Log("Deleting sandbox...")
		if err := sandbox.Delete(cleanupCtx); err != nil {
			t.Logf("WARNING: failed to delete sandbox %s: %v", sandbox.ID, err)
		} else {
			t.Log("Sandbox deleted")
		}
	})

	// 3. Verify OH is installed
	verifyResult, err := sandbox.Process.ExecuteCommand(ctx, "oh --version")
	if err != nil {
		t.Fatalf("oh --version failed: %v", err)
	}
	t.Logf("OpenHarness version: %s", strings.TrimSpace(verifyResult.Result))

	// 4. Create PTY session and run OH
	sessionID := fmt.Sprintf("v5-smoke-%d", time.Now().UnixMilli())
	handle, err := sandbox.Process.CreatePty(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreatePty: %v", err)
	}
	if err := handle.WaitForConnection(ctx); err != nil {
		t.Fatalf("WaitForConnection: %v", err)
	}
	t.Log("PTY session connected")

	// Collect PTY output
	type chunk struct {
		data string
		at   time.Time
	}
	chunks := make([]chunk, 0, 50)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for data := range handle.DataChan() {
			chunks = append(chunks, chunk{data: string(data), at: time.Now()})
		}
	}()

	// Run OpenHarness with stream-json output
	cmd := strings.Join([]string{
		"oh",
		"-p", `"What is 2+2? Reply with just the number."`,
		"--output-format", "stream-json",
		"--api-format", "openai",
		"--base-url", fallbackBaseURL,
		"--model", fallbackModel,
		"--max-turns", "1",
		"--dangerously-skip-permissions",
		"--bare",
		"2>/dev/null",
	}, " ")

	t.Logf("Running: %s", cmd)
	sendStart := time.Now()
	_, err = handle.Write([]byte(cmd + "\nexit\n"))
	if err != nil {
		t.Fatalf("Write to PTY: %v", err)
	}

	// Wait for completion
	select {
	case <-done:
		t.Log("PTY channel closed")
	case <-time.After(3 * time.Minute):
		t.Fatal("Timed out waiting for OH output")
	}
	handle.Disconnect()

	totalDuration := time.Since(sendStart)
	t.Logf("Received %d chunks over %v", len(chunks), totalDuration)

	// 5. Log raw chunks
	for i, c := range chunks {
		elapsed := c.at.Sub(sendStart)
		t.Logf("  chunk[%d] at +%v: %q", i, elapsed.Round(time.Millisecond), c.data)
	}

	// 6. Parse OH output into agent.Messages
	combined := ""
	for _, c := range chunks {
		combined += c.data
	}

	// Normalize and split into lines
	combined = normalizeLineEndings(combined)
	lines := strings.Split(combined, "\n")

	var messages []agent.Message
	var parseFailures int
	for _, line := range lines {
		trimmed := strings.TrimSpace(stripANSI(line))
		if trimmed == "" || trimmed[0] != '{' {
			continue // skip non-JSON lines (command echo, prompts, etc.)
		}
		msg, ok := ParseOHLine(line)
		if ok {
			messages = append(messages, msg)
			t.Logf("  parsed: type=%s content=%q tool=%q", msg.Type, truncate(msg.Content, 80), msg.Tool)
		} else {
			parseFailures++
			t.Logf("  FAILED to parse JSON line: %q", truncate(trimmed, 120))
		}
	}

	t.Logf("Parsed %d messages (%d JSON lines failed to parse)", len(messages), parseFailures)

	// 7. Assertions
	if len(messages) == 0 {
		t.Fatal("no messages parsed — OH output did not contain valid stream-json")
	}

	hasText := false
	var errorMessages []string
	hasStatus := false
	for _, m := range messages {
		if m.Type == agent.MessageText {
			hasText = true
		}
		if m.Type == agent.MessageError {
			errorMessages = append(errorMessages, truncate(m.Content, 120))
		}
		if m.Type == agent.MessageStatus {
			hasStatus = true
		}
	}
	if !hasText {
		t.Error("expected at least one MessageText, got none")
	}
	if len(errorMessages) > 0 {
		t.Fatalf("received %d MessageError events: %s", len(errorMessages), strings.Join(errorMessages, "; "))
	}

	// MessageStatus is a bonus — log warning if missing but don't fail
	if !hasStatus {
		t.Log("NOTE: no MessageStatus received (expected for trivial prompts)")
	}

	if parseFailures > 0 {
		t.Errorf("%d JSON lines failed to parse — check OH output format", parseFailures)
	}
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
