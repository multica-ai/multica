package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMcpToolsServeE2E exercises the full agent-time seam with the real compiled
// binary: the daemon writes a workspace steps file, the agent CLI spawns
// `multica mcp-tools serve`, and the agent calls an authored tool over MCP. serve
// loads the steps file, registers each step, and runs it in a re-exec'd child
// process — so this one test covers daemon(file) → serve(register) → child(run).
func TestMcpToolsServeE2E(t *testing.T) {
	if testing.Short() {
		t.Skip("e2e builds the multica binary; skipped in -short")
	}
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("go toolchain not available")
	}

	bin := buildMulticaBinary(t)

	// The daemon writes enabled steps here; serve is pointed at the file via
	// MULTICA_DETTOOLS_STEPS_FILE, exactly as dettools_inject does.
	workDir := t.TempDir()
	stepsDir := filepath.Join(workDir, ".multica", "dettools")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	stepsFile := filepath.Join(stepsDir, "steps.json")

	type stepDef struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Source      string `json:"source"`
	}
	steps := []stepDef{{
		Name:        "greet",
		Description: "greets the input name",
		Source: `package step
import "strings"
func Run(input map[string]any) map[string]any {
	name, _ := input["name"].(string)
	return map[string]any{"status": "ok", "summary": "Hello, " + strings.ToUpper(name)}
}`,
	}}
	stepsJSON, err := json.Marshal(steps)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(stepsFile, stepsJSON, 0o600); err != nil {
		t.Fatal(err)
	}

	// Newline-delimited JSON-RPC: initialize, the initialized notification (no
	// reply), tools/list, then a tools/call on the authored step.
	frames := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"greet","arguments":{"name":"world"}}}`,
	}, "\n") + "\n"

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "mcp-tools", "serve")
	cmd.Env = append(os.Environ(),
		"MULTICA_DETTOOLS_WORKDIR="+workDir,
		"MULTICA_DETTOOLS_STEPS_FILE="+stepsFile,
		// Built-ins are allowlisted to confirm authored + built-in coexist;
		// authored steps are added regardless of the allowlist.
		"MULTICA_DETTOOLS_ALLOWED=repo_facts,policy_check",
	)
	cmd.Stdin = strings.NewReader(frames)
	out, err := cmd.Output() // stdin EOF makes serve exit; stdout is the JSON-RPC stream
	if err != nil {
		stderr := ""
		if ee, ok := err.(*exec.ExitError); ok {
			stderr = string(ee.Stderr)
		}
		t.Fatalf("mcp-tools serve: %v\nstderr:\n%s", err, stderr)
	}

	byID := decodeRPCResponses(t, string(out))

	// tools/list (id 2) must contain the authored tool and a built-in.
	names := toolNamesFromList(t, byID[2])
	if !names["greet"] {
		t.Errorf("tools/list missing authored tool 'greet'; got %v", keys(names))
	}
	if !names["repo_facts"] {
		t.Errorf("tools/list missing built-in 'repo_facts'; got %v", keys(names))
	}

	// tools/call (id 3) on the authored step must run it (in a re-exec'd child)
	// and return the Result envelope.
	callResult, ok := byID[3]["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call has no result: %v", byID[3])
	}
	if callResult["isError"] == true {
		t.Errorf("tools/call isError = true, want false: %v", callResult)
	}
	structured, ok := callResult["structuredContent"].(map[string]any)
	if !ok {
		t.Fatalf("tools/call missing structuredContent: %v", callResult)
	}
	if structured["status"] != "ok" {
		t.Errorf("authored tool status = %v, want ok", structured["status"])
	}
	if structured["summary"] != "Hello, WORLD" {
		t.Errorf("authored tool summary = %v, want %q", structured["summary"], "Hello, WORLD")
	}
}

func buildMulticaBinary(t *testing.T) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "multica")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, "github.com/multica-ai/multica/server/cmd/multica")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build multica binary: %v\n%s", err, out)
	}
	return bin
}

// decodeRPCResponses parses the newline-delimited JSON-RPC stream and indexes
// responses by their numeric id (notifications carry no id and are skipped).
func decodeRPCResponses(t *testing.T, stream string) map[int]map[string]any {
	t.Helper()
	byID := map[int]map[string]any{}
	for _, line := range strings.Split(strings.TrimSpace(stream), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg map[string]any
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			t.Fatalf("decode rpc line %q: %v", line, err)
		}
		if idF, ok := msg["id"].(float64); ok {
			byID[int(idF)] = msg
		}
	}
	return byID
}

func toolNamesFromList(t *testing.T, resp map[string]any) map[string]bool {
	t.Helper()
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list has no result: %v", resp)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools/list result has no tools array: %v", result)
	}
	names := map[string]bool{}
	for _, tl := range tools {
		if m, ok := tl.(map[string]any); ok {
			if name, ok := m["name"].(string); ok {
				names[name] = true
			}
		}
	}
	return names
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
