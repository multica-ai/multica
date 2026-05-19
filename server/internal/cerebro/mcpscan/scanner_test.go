package mcpscan

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestMain doubles as the entry point for the fake MCP server used by the
// tests below: when the helper env var is set, run the scripted behavior and
// exit instead of running tests. This is the canonical Go pattern for
// exercising subprocess flows without shelling out to bash.
func TestMain(m *testing.M) {
	if mode := os.Getenv("MCPSCAN_FAKE_MODE"); mode != "" {
		runFakeMCPServer(mode)
		return
	}
	os.Exit(m.Run())
}

// runFakeMCPServer reads one JSON-RPC line at a time and replies based on the
// scripted mode passed via env. The fake supports just enough surface to
// exercise the scanner: initialize → notifications/initialized → tools/list.
func runFakeMCPServer(mode string) {
	in := bufio.NewReader(os.Stdin)
	out := os.Stdout
	for {
		line, err := in.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			os.Exit(0)
		}
		trimmed := strings.TrimSpace(string(line))
		if trimmed == "" {
			if err != nil {
				os.Exit(0)
			}
			continue
		}
		var req map[string]json.RawMessage
		if jerr := json.Unmarshal([]byte(trimmed), &req); jerr != nil {
			continue
		}
		var method string
		_ = json.Unmarshal(req["method"], &method)
		idRaw, hasID := req["id"]
		if !hasID {
			// notification — nothing to do.
			continue
		}
		switch method {
		case "initialize":
			result := json.RawMessage(`{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fake","version":"1"}}`)
			writeResp(out, idRaw, result, nil)
		case "tools/list":
			switch mode {
			case "ok":
				result := json.RawMessage(`{"tools":[{"name":"echo","description":"Echoes input","inputSchema":{"type":"object"}},{"name":"add","description":"Adds two numbers"}]}`)
				writeResp(out, idRaw, result, nil)
			case "error":
				writeResp(out, idRaw, nil, &jsonRPCError{Code: -32601, Message: "method not found"})
			case "garbage_then_ok":
				fmt.Fprintln(out, "this is not json — a stray log line")
				result := json.RawMessage(`{"tools":[{"name":"only","description":""}]}`)
				writeResp(out, idRaw, result, nil)
			default:
				writeResp(out, idRaw, json.RawMessage(`{"tools":[]}`), nil)
			}
		}
		if err != nil {
			os.Exit(0)
		}
	}
}

func writeResp(out *os.File, id json.RawMessage, result json.RawMessage, errVal *jsonRPCError) {
	resp := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
	}
	if errVal != nil {
		resp["error"] = errVal
	} else {
		resp["result"] = result
	}
	encoded, _ := json.Marshal(resp)
	out.Write(append(encoded, '\n'))
}

// buildFakeServer compiles the test binary as a runnable MCP server stand-in
// and returns its absolute path. We reuse `go test -c` so the helper binary
// is identical to the test runner and TestMain dispatches to runFakeMCPServer
// when MCPSCAN_FAKE_MODE is set.
func buildFakeServer(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeserver.test")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	cmd := exec.Command("go", "test", "-c", "-o", bin, ".")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build fake server: %v\nstderr: %s", err, stderr.String())
	}
	return bin
}

func TestScan_HappyPath(t *testing.T) {
	bin := buildFakeServer(t)
	tools, err := Scan(context.Background(), ServerSpec{
		Name:    "fake",
		Command: bin,
		Env:     map[string]string{"MCPSCAN_FAKE_MODE": "ok", "PATH": os.Getenv("PATH")},
	}, 10*time.Second)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if got, want := len(tools), 2; got != want {
		t.Fatalf("tools: got %d, want %d", got, want)
	}
	if tools[0].Name != "echo" || tools[1].Name != "add" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	if !strings.Contains(string(tools[0].InputSchema), "object") {
		t.Fatalf("expected inputSchema preserved, got %q", tools[0].InputSchema)
	}
}

func TestScan_ServerError(t *testing.T) {
	bin := buildFakeServer(t)
	_, err := Scan(context.Background(), ServerSpec{
		Name:    "fake",
		Command: bin,
		Env:     map[string]string{"MCPSCAN_FAKE_MODE": "error", "PATH": os.Getenv("PATH")},
	}, 10*time.Second)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("expected server error message in %q", err)
	}
}

func TestScan_SkipsStdoutNoise(t *testing.T) {
	bin := buildFakeServer(t)
	tools, err := Scan(context.Background(), ServerSpec{
		Name:    "fake",
		Command: bin,
		Env:     map[string]string{"MCPSCAN_FAKE_MODE": "garbage_then_ok", "PATH": os.Getenv("PATH")},
	}, 10*time.Second)
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "only" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
}

func TestScan_BadCommand(t *testing.T) {
	_, err := Scan(context.Background(), ServerSpec{
		Name:    "fake",
		Command: "/no/such/binary/exists/here",
	}, 2*time.Second)
	if err == nil {
		t.Fatal("expected error for missing binary, got nil")
	}
}

func TestScan_EmptyCommand(t *testing.T) {
	_, err := Scan(context.Background(), ServerSpec{Name: "fake"}, 2*time.Second)
	if err == nil {
		t.Fatal("expected error for empty command, got nil")
	}
}
