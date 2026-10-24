//go:build agentintegration

package execenv

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAntigravityLiveDualTrackE2E(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("set MULTICA_RUN_REAL_AGENT_SMOKE=1 to authorize real agent execution")
	}
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		t.Fatal("authorized live test requires agy on PATH")
	}
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		t.Fatal("live stdio fixture requires python3")
	}
	metadataCtx, metadataCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer metadataCancel()
	revision, err := exec.CommandContext(metadataCtx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	version, err := exec.CommandContext(metadataCtx, agyPath, "--version").Output()
	if err != nil {
		t.Fatal(err)
	}
	changes, err := exec.CommandContext(metadataCtx, "git", "status", "--porcelain").Output()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("revision=%s dirty=%t platform=%s/%s agy=%s", strings.TrimSpace(string(revision)), len(changes) != 0, runtime.GOOS, runtime.GOARCH, strings.TrimSpace(string(version)))

	testRoot := t.TempDir()
	stdioAuditPath := filepath.Join(testRoot, "stdio-audit.jsonl")
	stdioScript := filepath.Join(testRoot, "stdio_server.py")
	stdioContent := `import sys, json
for line in sys.stdin:
    if not line.strip():
        continue
    msg = json.loads(line)
    method = msg.get("method")
    event = {"method": method}
    result = {}
    if method == "initialize":
        result = {"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"stdio-test","version":"1.0"}}
    elif method == "tools/list":
        result = {"tools":[{"name":"calc_square","description":"Squares an integer via stdio","inputSchema":{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}}]}
    elif method == "tools/call":
        params = msg.get("params", {})
        n = params.get("arguments", {}).get("n")
        event.update(name=params.get("name"), argument=n)
        if params.get("name") == "calc_square" and type(n) is int:
            text = f"SQUARE_SUCCESS:{n*n}"
            event["result"] = text
            result = {"content":[{"type":"text","text":text}]}
        else:
            result = {"isError":True,"content":[{"type":"text","text":"invalid tool call"}]}
    with open(sys.argv[1], "a", encoding="utf-8") as audit:
        audit.write(json.dumps(event)+"\n")
    if "id" in msg:
        sys.stdout.write(json.dumps({"jsonrpc":"2.0","id":msg["id"],"result":result})+"\n")
        sys.stdout.flush()
`
	if err := os.WriteFile(stdioScript, []byte(stdioContent), 0o600); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var httpEvents []antigravityMCPEvent
	const sessionToken = "session-test-e2e-888"
	httpServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Name      string `json:"name"`
				Arguments struct {
					Number int `json:"n"`
				} `json:"arguments"`
			} `json:"params"`
		}
		if err := json.NewDecoder(io.LimitReader(request.Body, 1<<20)).Decode(&message); err != nil {
			http.Error(writer, "invalid JSON", http.StatusBadRequest)
			return
		}
		event := antigravityMCPEvent{Method: message.Method, Session: request.Header.Get("Mcp-Session-Id")}
		result := map[string]any{}
		switch message.Method {
		case "server/discover", "initialize":
			result = map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "http-test", "version": "1.0"}}
		case "tools/list":
			result["tools"] = []any{map[string]any{
				"name": "calc_cube", "description": "Cubes an integer via Streamable HTTP",
				"inputSchema": map[string]any{"type": "object", "properties": map[string]any{"n": map[string]string{"type": "integer"}}, "required": []string{"n"}},
			}}
		case "tools/call":
			event.Name, event.Argument = message.Params.Name, message.Params.Arguments.Number
			if event.Name == "calc_cube" {
				event.Result = fmt.Sprintf("CUBE_SUCCESS:%d", event.Argument*event.Argument*event.Argument)
			} else {
				result["isError"] = true
			}
			result["content"] = []any{map[string]string{"type": "text", "text": event.Result}}
		}
		mu.Lock()
		httpEvents = append(httpEvents, event)
		mu.Unlock()
		if message.Method == "initialize" || event.Session == sessionToken {
			writer.Header().Set("Mcp-Session-Id", sessionToken)
		}
		if len(message.ID) == 0 {
			writer.WriteHeader(http.StatusAccepted)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(writer).Encode(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": result}); err != nil {
			t.Errorf("write HTTP fixture response: %v", err)
		}
	}))
	t.Cleanup(func() {
		httpServer.CloseClientConnections()
		httpServer.Close()
	})

	raw, err := json.Marshal(map[string]any{"mcpServers": map[string]any{
		"local_stdio": map[string]any{"command": pythonPath, "args": []string{stdioScript, stdioAuditPath}},
		"remote_http": map[string]any{"url": httpServer.URL + "/mcp"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	params := PrepareParams{WorkspacesRoot: testRoot, WorkspaceID: "ws", TaskID: "task", Provider: "antigravity", McpConfig: raw}
	env, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := env.Cleanup(true); err != nil {
			t.Error(err)
		}
	})
	configPath := filepath.Join(env.WorkDir, ".agents", "plugins", "multica", "mcp_config.json")
	t.Logf("Prepare task workdir=%s", env.WorkDir)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	output, runErr := runAntigravityProbe(ctx, agyPath, env.WorkDir, "Use calc_square with n=12, and use calc_cube with n=7. Output exactly the tool result text strings.")
	cancel()
	if runErr != nil {
		t.Fatalf("managed task launch: %v", runErr)
	}
	if !strings.Contains(output, "SQUARE_SUCCESS:144") || !strings.Contains(output, "CUBE_SUCCESS:343") {
		t.Fatal("agent output missing expected tool results")
	}
	stdioFile, err := os.Open(stdioAuditPath)
	if err != nil {
		t.Fatalf("stdio server audit missing: %v", err)
	}
	defer stdioFile.Close()
	var stdioEvents []antigravityMCPEvent
	scanner := bufio.NewScanner(stdioFile)
	for scanner.Scan() {
		var event antigravityMCPEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatal(err)
		}
		stdioEvents = append(stdioEvents, event)
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	firstHTTPEvents := append([]antigravityMCPEvent(nil), httpEvents...)
	mu.Unlock()
	for _, audit := range []struct {
		name, session, result string
		argument              int
		events                []antigravityMCPEvent
	}{
		{"calc_square", "", "SQUARE_SUCCESS:144", 12, stdioEvents},
		{"calc_cube", sessionToken, "CUBE_SUCCESS:343", 7, firstHTTPEvents},
	} {
		data, err := json.Marshal(audit.events)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("%s server-side audit: %s", audit.name, data)
		if err := validateAntigravityMCPAudit(audit.events, audit.session, audit.name, audit.argument, audit.result); err != nil {
			t.Fatal(err)
		}
	}

	params.TaskID, params.McpConfig = "isolated", nil
	isolated, err := Prepare(params, testLogger())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := isolated.Cleanup(true); err != nil {
			t.Error(err)
		}
	})
	isolationCtx, isolationCancel := context.WithTimeout(context.Background(), 60*time.Second)
	isolatedOutput, isolationErr := runAntigravityProbe(isolationCtx, agyPath, isolated.WorkDir, "List available MCP tools named calc_square or calc_cube. If neither is available, reply exactly NONE. Do not read other task directories.")
	isolationCancel()
	if isolationErr != nil {
		t.Fatalf("isolated task launch: %v", isolationErr)
	}
	if strings.TrimSpace(isolatedOutput) != "NONE" {
		t.Fatal("isolated task did not explicitly confirm absence of fixture tools")
	}
	mu.Lock()
	requestCount := len(httpEvents)
	mu.Unlock()
	stdioAfterIsolation, err := os.ReadFile(stdioAuditPath)
	if err != nil {
		t.Fatal(err)
	}
	if requestCount != len(firstHTTPEvents) || len(strings.Split(strings.TrimSpace(string(stdioAfterIsolation)), "\n")) != len(stdioEvents) {
		t.Fatal("isolated task accessed the first task's MCP fixture")
	}

	env.ReleaseLock()
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("managed config must remain available for reuse: %v", err)
	}
	for _, replacement := range []json.RawMessage{json.RawMessage(`{"mcpServers":{"replacement":{"command":"unused-fixture-command"}}}`), nil} {
		reused := Reuse(ReuseParams{WorkDir: env.WorkDir, Provider: "antigravity", McpConfig: replacement}, testLogger())
		if reused == nil {
			t.Fatal("Reuse failed")
		}
		reused.ReleaseLock()
		if replacement == nil {
			if _, err := os.Stat(configPath); !os.IsNotExist(err) {
				t.Fatalf("Reuse did not remove cancelled MCP config: %v", err)
			}
		} else {
			var config struct {
				Servers map[string]any `json:"mcpServers"`
			}
			readAntigravityJSON(t, configPath, &config)
			if len(config.Servers) != 1 || config.Servers["replacement"] == nil {
				t.Fatal("Reuse did not replace the MCP server set")
			}
		}
	}
	if err := env.Cleanup(true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(env.RootDir); !os.IsNotExist(err) {
		t.Fatalf("full environment cleanup failed: %v", err)
	}
	t.Log("PASS: backend launch, audited tools/list and tools/call on both transports, independent-task isolation, managed retention, Reuse refresh/removal and explicit full cleanup; daemon task orchestration and scheduled GC are not exercised")
}
