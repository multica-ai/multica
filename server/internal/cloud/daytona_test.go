//go:build integration

package cloud

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daytonaio/daytona/libs/sdk-go/pkg/daytona"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/options"
	"github.com/daytonaio/daytona/libs/sdk-go/pkg/types"
)

// TestDeniedToolsEnforcement diagnoses whether denied_tools settings.json is loaded by OH inside a sandbox.
func TestDeniedToolsEnforcement(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set")
	}
	fallbackKey := os.Getenv("GOOGLE_AI_API_KEY")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// Same image as production
	image := daytona.DebianSlim(nil).
		AptGet([]string{"nodejs", "npm", "curl"}).
		Run("npm install -g modelrelay").
		PipInstall([]string{"openharness-ai==0.1.6"}).
		Env("TERM", "dumb")

	envVars := map[string]string{
		"TERM":                   "dumb",
		"OPENHARNESS_CONFIG_DIR": "/etc/multica-agent",
	}
	if fallbackKey != "" {
		envVars["FALLBACK_API_KEY"] = fallbackKey
	}

	t.Log("Creating sandbox...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		SandboxBaseParams: types.SandboxBaseParams{EnvVars: envVars},
		Image:             image,
	}, options.WithTimeout(8*time.Minute))
	if err != nil {
		t.Fatalf("Create sandbox: %v", err)
	}
	t.Logf("Sandbox created: id=%s", sandbox.ID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		sandbox.Delete(cleanupCtx)
	})

	run := func(label, cmd string) string {
		t.Logf("--- %s ---", label)
		result, err := sandbox.Process.ExecuteCommand(ctx, cmd)
		if err != nil {
			t.Logf("  ERROR: %v", err)
			return ""
		}
		out := strings.TrimSpace(result.Result)
		t.Logf("  exit=%d output=%q", result.ExitCode, out[:min(len(out), 200)])
		return out
	}

	// Step 1: Upload denied_tools settings.json to BOTH paths (mirrors production sandbox.go)
	settingsJSON := `{"permission":{"mode":"full_auto","denied_tools":["bash","file_edit","file_read","glob","grep"]}}`
	sandbox.Process.ExecuteCommand(ctx, "mkdir -p /home/daytona/.openharness")
	t.Log("Uploading settings.json to both config paths...")
	if err := sandbox.FileSystem.UploadFile(ctx, []byte(settingsJSON), "/home/daytona/.openharness/settings.json"); err != nil {
		t.Fatalf("Upload to ~/.openharness/ failed: %v", err)
	}
	sandbox.FileSystem.UploadFile(ctx, []byte(settingsJSON), "/etc/multica-agent/settings.json")

	// Step 2: Verify the file exists and content is correct
	content := run("Verify settings.json", "cat /etc/multica-agent/settings.json")
	if !strings.Contains(content, "denied_tools") {
		t.Fatal("settings.json missing denied_tools")
	}

	// Step 3: Verify OPENHARNESS_CONFIG_DIR is set in sandbox env
	run("Check env var in sandbox", "echo $OPENHARNESS_CONFIG_DIR")

	// Step 4: Verify OH can read the config
	run("OH config check", "OPENHARNESS_CONFIG_DIR=/etc/multica-agent python3 -c \"from openharness.config.settings import load_settings; s=load_settings(); print('denied_tools:', s.permission.denied_tools); print('mode:', s.permission.mode)\"")

	// Step 5: Start ModelRelay and run OH with a bash-triggering prompt
	run("Start ModelRelay", "modelrelay > /dev/null 2>&1 & sleep 5 && curl -sf http://localhost:7352/v1/models > /dev/null && echo MR_OK || echo MR_FAIL")

	// Step 6: Run OH and check if bash is denied
	ohOutput := run("Run OH with denied_tools",
		`export OPENHARNESS_CONFIG_DIR=/etc/multica-agent && `+
			`export OPENAI_API_KEY=dummy && `+
			`oh -p "Run the command echo BASH_TEST using the bash tool" `+
			`--output-format stream-json `+
			`--api-format openai `+
			`--base-url http://localhost:7352/v1 `+
			`--model auto-fastest `+
			`--max-turns 3 `+
			`--permission-mode full_auto `+
			`2>/dev/null`)

	t.Logf("OH output length: %d", len(ohOutput))

	// Check results — look for denial in tool_completed output, not tool_input
	hasDenied := strings.Contains(strings.ToLower(ohOutput), "denied") ||
		strings.Contains(strings.ToLower(ohOutput), "not allowed")
	// Check if bash actually executed (output contains the echo result, not just the command in tool_input)
	bashSucceeded := strings.Contains(ohOutput, `"output": "BASH_TEST"`) ||
		strings.Contains(ohOutput, `"output":"BASH_TEST"`)

	if bashSucceeded {
		t.Error("FAIL: bash executed successfully — denied_tools NOT enforced")
	}
	if hasDenied {
		t.Log("PASS: bash was explicitly denied")
	}
	if strings.Contains(ohOutput, "assistant_delta") || strings.Contains(ohOutput, "assistant_complete") {
		t.Log("OH produced output (agent responded)")
	}
	if ohOutput == "" {
		t.Error("OH produced no output — may have crashed")
	}
}

// TestModelRelayInSandbox diagnoses why ModelRelay may not work inside a Daytona sandbox.
// Checks: binary on PATH, Node version, npm install, startup, network egress.
func TestModelRelayInSandbox(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{APIKey: apiKey})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// Build the same image as sandbox.go
	image := daytona.DebianSlim(nil).
		AptGet([]string{"nodejs", "npm", "curl"}).
		Run("npm install -g modelrelay").
		PipInstall([]string{"openharness-ai==0.1.6"}).
		Env("TERM", "dumb")

	logChan := make(chan string, 200)
	go func() {
		for line := range logChan {
			t.Logf("[build] %s", line)
		}
	}()

	t.Log("Creating sandbox with ModelRelay image (may take several minutes on first build)...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		SandboxBaseParams: types.SandboxBaseParams{
			EnvVars: map[string]string{"TERM": "dumb"},
		},
		Image: image,
	}, options.WithTimeout(8*time.Minute), options.WithLogChannel(logChan))
	if err != nil {
		t.Fatalf("Create sandbox: %v", err)
	}
	t.Logf("Sandbox created: id=%s", sandbox.ID)

	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cleanupCancel()
		if err := sandbox.Delete(cleanupCtx); err != nil {
			t.Logf("WARNING: failed to delete sandbox: %v", err)
		} else {
			t.Log("Sandbox deleted")
		}
	})

	run := func(label, cmd string) string {
		t.Logf("--- %s ---", label)
		result, err := sandbox.Process.ExecuteCommand(ctx, cmd)
		if err != nil {
			t.Logf("  ERROR: %v", err)
			return ""
		}
		t.Logf("  exit=%d output=%q", result.ExitCode, strings.TrimSpace(result.Result))
		return strings.TrimSpace(result.Result)
	}

	// Check 1: Node.js version
	nodeVer := run("Node.js version", "node --version")
	if nodeVer == "" {
		t.Error("DIAGNOSIS: Node.js not installed")
	} else {
		t.Logf("  → Node %s installed", nodeVer)
	}

	// Check 2: npm version
	run("npm version", "npm --version")

	// Check 3: Is modelrelay binary installed?
	whichMR := run("which modelrelay", "which modelrelay 2>/dev/null || echo 'NOT_ON_PATH'")
	if strings.Contains(whichMR, "NOT_ON_PATH") {
		t.Error("DIAGNOSIS: modelrelay not on PATH")
		// Check where npm puts global bins
		run("npm global bin", "npm bin -g")
		run("ls npm global", "ls -la $(npm bin -g)/modelrelay 2>/dev/null || echo 'NOT_FOUND'")
		run("find modelrelay", "find / -name modelrelay -type f 2>/dev/null | head -5")
	}

	// Check 4: Can modelrelay start?
	run("modelrelay startup test", "timeout 15 modelrelay 2>&1 &\nsleep 5\ncurl -sf http://localhost:7352/v1/models 2>&1 | head -100 || echo 'HEALTH_CHECK_FAILED'\nkill %1 2>/dev/null")

	// Check 5: Network egress to free providers
	run("network: Groq", "curl -sf -o /dev/null -w '%{http_code}' https://api.groq.com/ 2>&1 || echo 'BLOCKED'")
	run("network: OpenRouter", "curl -sf -o /dev/null -w '%{http_code}' https://openrouter.ai/api/v1/models 2>&1 || echo 'BLOCKED'")
	run("network: Google", "curl -sf -o /dev/null -w '%{http_code}' https://www.google.com 2>&1 || echo 'BLOCKED'")

	// Check 6: PATH in the sandbox
	run("PATH", "echo $PATH")

	// Check 7: oh version (verify OH is installed too)
	run("oh version", "oh --version 2>&1")

	// Check 8: Can DuckDuckGo be reached? (OH's web_search uses DDG)
	run("DuckDuckGo reachable", "curl -sf -o /dev/null -w '%{http_code}' 'https://html.duckduckgo.com/html/?q=test' 2>&1 || echo 'DDG_BLOCKED'")

	// Check 9: Run OH with web_search and capture FULL output (including errors)
	run("OH web_search e2e",
		"modelrelay > /dev/null 2>&1 & "+
			"MR_PID=$! && "+
			"sleep 5 && "+
			"export OPENAI_API_KEY=dummy && "+
			"oh -p 'Search the web for Vercel pricing and tell me the plans' "+
			"--output-format stream-json "+
			"--api-format openai "+
			"--base-url http://localhost:7352/v1 "+
			"--model auto-fastest "+
			"--max-turns 3 "+
			"--permission-mode full_auto "+
			"2>&1 | head -30; "+
			"kill $MR_PID 2>/dev/null")
}

func TestDaytonaSandboxRoundtrip(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	// 1. Initialize client
	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// 2. Create sandbox from a stock image
	t.Log("Creating sandbox from ubuntu:22.04...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		Image: "ubuntu:22.04",
	})
	if err != nil {
		t.Fatalf("Create sandbox: %v", err)
	}
	t.Logf("Sandbox created: id=%s", sandbox.ID)

	// Ensure teardown even on failure
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

	// 3. Execute `echo hello` in the sandbox
	result, err := sandbox.Process.ExecuteCommand(ctx, "echo hello")
	if err != nil {
		t.Fatalf("ExecuteCommand: %v", err)
	}

	// 4. Assert stdout contains "hello"
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Result, "hello") {
		t.Errorf("expected output to contain %q, got %q", "hello", result.Result)
	}

	t.Logf("Command output: %q (exit code %d)", result.Result, result.ExitCode)
}

func TestDaytonaStopStartPreservesState(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// 1. Create sandbox
	t.Log("Creating sandbox from ubuntu:22.04...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		Image: "ubuntu:22.04",
	})
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

	// 2. Write a file inside the sandbox
	t.Log("Writing /workspace/state.txt...")
	writeResult, err := sandbox.Process.ExecuteCommand(ctx, `echo "persist-test" > /workspace/state.txt`)
	if err != nil {
		t.Fatalf("ExecuteCommand (write): %v", err)
	}
	if writeResult.ExitCode != 0 {
		t.Fatalf("write command failed: exit=%d output=%q", writeResult.ExitCode, writeResult.Result)
	}

	// Verify the file exists before stop
	verifyResult, err := sandbox.Process.ExecuteCommand(ctx, "cat /workspace/state.txt")
	if err != nil {
		t.Fatalf("ExecuteCommand (verify before stop): %v", err)
	}
	t.Logf("Before stop: cat /workspace/state.txt => %q", strings.TrimSpace(verifyResult.Result))

	// 3. Stop the sandbox
	t.Log("Stopping sandbox...")
	stopStart := time.Now()
	if err := sandbox.Stop(ctx); err != nil {
		t.Fatalf("Stop sandbox: %v", err)
	}
	stopDuration := time.Since(stopStart)
	t.Logf("Sandbox stopped in %v", stopDuration)

	// 4. Start the sandbox
	t.Log("Starting sandbox...")
	startStart := time.Now()
	if err := sandbox.Start(ctx); err != nil {
		t.Fatalf("Start sandbox: %v", err)
	}
	startDuration := time.Since(startStart)
	t.Logf("Sandbox started in %v", startDuration)

	// 5. Read the file back
	t.Log("Reading /workspace/state.txt after restart...")
	readResult, err := sandbox.Process.ExecuteCommand(ctx, "cat /workspace/state.txt")
	if err != nil {
		t.Fatalf("ExecuteCommand (read after restart): %v", err)
	}
	if readResult.ExitCode != 0 {
		t.Fatalf("read command failed: exit=%d output=%q", readResult.ExitCode, readResult.Result)
	}

	// 6. Assert the content survived the stop/start cycle
	got := strings.TrimSpace(readResult.Result)
	t.Logf("After restart: cat /workspace/state.txt => %q", got)

	if got != "persist-test" {
		t.Errorf("filesystem state NOT preserved: expected %q, got %q", "persist-test", got)
	} else {
		t.Log("SUCCESS: filesystem state preserved across stop/start")
	}

	t.Logf("Summary: Stop=%v, Start=%v", stopDuration, startDuration)
}

func TestDaytonaFSDownload(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// 1. Create sandbox
	t.Log("Creating sandbox from ubuntu:22.04...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		Image: "ubuntu:22.04",
	})
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

	// 2. Write 3 test files via ExecuteCommand
	testFiles := map[string]string{
		"/workspace/output/report.md":  "# Test Report\n\nThis is a test report with **markdown** content.\n",
		"/workspace/output/data.csv":   "name,age,city\nAlice,30,NYC\nBob,25,SF\nCharlie,35,LA\n",
		"/workspace/output/config.json": `{"version": 1, "debug": true, "name": "test-config"}` + "\n",
	}

	t.Log("Creating /workspace/output/ directory and writing test files...")
	mkdirResult, err := sandbox.Process.ExecuteCommand(ctx, "mkdir -p /workspace/output")
	if err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if mkdirResult.ExitCode != 0 {
		t.Fatalf("mkdir failed: exit=%d output=%q", mkdirResult.ExitCode, mkdirResult.Result)
	}

	for path, content := range testFiles {
		// Use UploadFile to write exact content (avoids shell escaping issues)
		err := sandbox.FileSystem.UploadFile(ctx, []byte(content), path)
		if err != nil {
			t.Fatalf("UploadFile %s: %v", path, err)
		}
		t.Logf("Uploaded %s (%d bytes)", path, len(content))
	}

	// 3. List files via FileSystem API
	t.Log("Listing /workspace/output/ via FileSystem.ListFiles...")
	files, err := sandbox.FileSystem.ListFiles(ctx, "/workspace/output/")
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}

	t.Logf("ListFiles returned %d entries:", len(files))
	foundFiles := make(map[string]bool)
	for _, f := range files {
		t.Logf("  %s (dir=%v, size=%d, mode=%s)", f.Name, f.IsDirectory, f.Size, f.Mode)
		foundFiles[f.Name] = true
	}

	// Assert we found all 3 files
	expectedNames := []string{"report.md", "data.csv", "config.json"}
	for _, name := range expectedNames {
		if !foundFiles[name] {
			t.Errorf("ListFiles: expected to find %q, but it was missing", name)
		}
	}

	// 4. Download each file via FileSystem API and verify content
	t.Log("Downloading files via FileSystem.DownloadFile...")
	for path, expectedContent := range testFiles {
		data, err := sandbox.FileSystem.DownloadFile(ctx, path, nil)
		if err != nil {
			t.Fatalf("DownloadFile(%s): %v", path, err)
		}
		got := string(data)
		t.Logf("Downloaded %s: %d bytes", path, len(data))

		if got != expectedContent {
			t.Errorf("DownloadFile(%s): content mismatch\n  expected: %q\n  got:      %q", path, expectedContent, got)
		} else {
			t.Logf("  Content matches for %s", path)
		}
	}

	t.Log("SUCCESS: FileSystem ListFiles + DownloadFile round-trip verified")
}

func hasExactLine(out, want string) bool {
	for _, line := range strings.Split(out, "\n") {
		if strings.TrimSpace(line) == want {
			return true
		}
	}
	return false
}

func TestDaytonaPtyStreamingIncremental(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	t.Log("Creating sandbox...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		Image: "ubuntu:22.04",
	})
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

	// Create a PTY session for streaming
	sessionID := fmt.Sprintf("test-stream-%d", time.Now().UnixMilli())
	handle, err := sandbox.Process.CreatePty(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreatePty: %v", err)
	}
	t.Cleanup(func() {
		if err := handle.Disconnect(); err != nil {
			t.Logf("Disconnect PTY: %v", err)
		}
	})
	if err := handle.WaitForConnection(ctx); err != nil {
		t.Fatalf("WaitForConnection: %v", err)
	}
	t.Log("PTY session connected")

	// Track when each chunk arrives
	type chunk struct {
		data string
		at   time.Time
	}
	chunks := make([]chunk, 0, 10)
	done := make(chan struct{})

	go func() {
		defer close(done)
		for data := range handle.DataChan() {
			chunks = append(chunks, chunk{data: string(data), at: time.Now()})
		}
	}()

	// Send a command that emits 5 lines, one per second
	cmd := "for i in 1 2 3 4 5; do echo \"line $i\"; sleep 1; done\nexit\n"
	t.Logf("Sending command: %s", strings.TrimSpace(cmd))
	sendStart := time.Now()
	_, err = handle.Write([]byte(cmd))
	if err != nil {
		t.Fatalf("Write to PTY: %v", err)
	}

	// Wait for process to finish (5s of sleeps + buffer)
	select {
	case <-done:
		t.Log("PTY channel closed")
	case <-time.After(30 * time.Second):
		t.Fatal("Timed out waiting for PTY output")
	}
	totalDuration := time.Since(sendStart)

	// Log all chunks with timing
	t.Logf("Received %d chunks over %v:", len(chunks), totalDuration)
	for i, c := range chunks {
		elapsed := c.at.Sub(sendStart)
		t.Logf("  chunk[%d] at +%v: %q", i, elapsed.Round(time.Millisecond), c.data)
	}

	// Assert: we got multiple chunks (not everything batched at the end)
	if len(chunks) < 3 {
		t.Errorf("expected >=3 chunks for incremental delivery, got %d", len(chunks))
	}

	// Assert: all 5 lines appeared somewhere in the output
	combined := ""
	for _, c := range chunks {
		combined += c.data
	}
	for i := 1; i <= 5; i++ {
		needle := fmt.Sprintf("line %d", i)
		if !strings.Contains(combined, needle) {
			t.Errorf("missing %q in combined output", needle)
		}
	}

	// Assert: chunks arrived over time, not all at once
	// The first and last chunk should be at least 2s apart (5 lines × 1s sleep)
	if len(chunks) >= 2 {
		spread := chunks[len(chunks)-1].at.Sub(chunks[0].at)
		t.Logf("Time spread between first and last chunk: %v", spread)
		if spread < 2*time.Second {
			t.Errorf("expected chunks spread over >=2s (incremental), got %v (likely buffered)", spread)
		}
	}
}

func TestDaytonaStopDuringActiveProcess(t *testing.T) {
	apiKey := os.Getenv("DAYTONA_API_KEY")
	if apiKey == "" {
		t.Skip("DAYTONA_API_KEY not set; skipping integration test")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client, err := daytona.NewClientWithConfig(&types.DaytonaConfig{
		APIKey: apiKey,
	})
	if err != nil {
		t.Fatalf("NewClientWithConfig: %v", err)
	}
	defer client.Close(ctx)

	// 1. Create sandbox
	t.Log("Creating sandbox from ubuntu:22.04...")
	sandbox, err := client.Create(ctx, types.ImageParams{
		Image: "ubuntu:22.04",
	})
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

	// 2. Create a PTY session
	sessionID := fmt.Sprintf("test-stop-%d", time.Now().UnixMilli())
	handle, err := sandbox.Process.CreatePty(ctx, sessionID)
	if err != nil {
		t.Fatalf("CreatePty: %v", err)
	}
	t.Cleanup(func() {
		if err := handle.Disconnect(); err != nil {
			t.Logf("Disconnect PTY: %v", err)
		}
	})
	if err := handle.WaitForConnection(ctx); err != nil {
		t.Fatalf("WaitForConnection: %v", err)
	}
	t.Log("PTY session connected")

	// 3. Collect output in a goroutine
	type chunk struct {
		data string
		at   time.Time
	}
	var chunksMu sync.Mutex
	chunks := make([]chunk, 0, 10)
	dataChanClosed := make(chan struct{})

	go func() {
		defer close(dataChanClosed)
		for data := range handle.DataChan() {
			chunksMu.Lock()
			chunks = append(chunks, chunk{data: string(data), at: time.Now()})
			chunksMu.Unlock()
		}
	}()

	// 4. Start a long-running process (60 ticks, one per second)
	cmd := `for i in $(seq 1 60); do echo "tick $i"; sleep 1; done` + "\n"
	t.Log("Sending long-running command...")
	_, err = handle.Write([]byte(cmd))
	if err != nil {
		t.Fatalf("Write to PTY: %v", err)
	}

	// 5. Wait until we've observed at least 2 ticks (or 10s timeout)
	t.Log("Waiting for at least 2 ticks...")
	deadline := time.After(10 * time.Second)
waitLoop:
	for {
		select {
		case <-deadline:
			t.Log("Timeout waiting for ticks, proceeding with Stop()")
			break waitLoop
		case <-time.After(200 * time.Millisecond):
			chunksMu.Lock()
			combined := ""
			for _, c := range chunks {
				combined += c.data
			}
			chunksMu.Unlock()
			if hasExactLine(combined, "tick 2") {
				t.Log("Observed tick 2, proceeding with Stop()")
				break waitLoop
			}
		}
	}

	// 6. Stop the sandbox while the process is still running
	t.Log("Stopping sandbox while process is active...")
	stopStart := time.Now()
	stopErr := sandbox.Stop(ctx)
	stopDuration := time.Since(stopStart)
	t.Logf("Stop() returned in %v", stopDuration)

	// Assert: Stop() returned without error
	if stopErr != nil {
		t.Fatalf("Stop() returned error: %v", stopErr)
	}
	t.Log("Stop() returned without error")

	// 7. Assert: PTY DataChan() closes (doesn't hang forever -- 10s timeout)
	select {
	case <-dataChanClosed:
		t.Log("PTY DataChan() closed cleanly")
	case <-time.After(10 * time.Second):
		t.Fatal("PTY DataChan() did not close within 10s after Stop() -- goroutine is hanging")
	}

	// 8. Assert: at least 2 ticks were received before stop
	chunksMu.Lock()
	combined := ""
	for _, c := range chunks {
		combined += c.data
	}
	chunksMu.Unlock()
	t.Logf("Combined PTY output (%d chunks): %q", len(chunks), combined)

	tickCount := 0
	for i := 1; i <= 60; i++ {
		if hasExactLine(combined, fmt.Sprintf("tick %d", i)) {
			tickCount++
		}
	}
	t.Logf("Received %d ticks before stop", tickCount)
	if tickCount < 2 {
		t.Errorf("expected at least 2 ticks before stop, got %d", tickCount)
	}

	// 9. Start the sandbox again to verify it isn't corrupted
	t.Log("Starting sandbox after stop...")
	startStart := time.Now()
	if err := sandbox.Start(ctx); err != nil {
		t.Fatalf("Start() after stop failed: %v", err)
	}
	startDuration := time.Since(startStart)
	t.Logf("Start() returned in %v", startDuration)

	// 10. Run a simple command to verify the sandbox works
	t.Log("Running verification command...")
	result, err := sandbox.Process.ExecuteCommand(ctx, "echo ok")
	if err != nil {
		t.Fatalf("ExecuteCommand after restart: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("expected exit code 0, got %d", result.ExitCode)
	}
	if !strings.Contains(result.Result, "ok") {
		t.Errorf("expected output to contain %q, got %q", "ok", result.Result)
	}
	t.Logf("Verification command output: %q (exit code %d)", result.Result, result.ExitCode)

	t.Logf("Summary: Stop=%v, Start=%v, Ticks received=%d", stopDuration, startDuration, tickCount)
	t.Log("SUCCESS: Stop() during active process worked cleanly")
}
