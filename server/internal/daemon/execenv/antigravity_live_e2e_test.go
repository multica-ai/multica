package execenv

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestAntigravityLiveDualTrackE2E(t *testing.T) {
	agyPath, err := exec.LookPath("agy")
	if err != nil {
		t.Skip("agy CLI not found on host, skipping live E2E test")
	}

	// 1. Prepare test directories
	testRoot := t.TempDir()
	taskDir := filepath.Join(testRoot, "task-managed-1")
	if err := os.MkdirAll(taskDir, 0o755); err != nil {
		t.Fatal(err)
	}

	// Stdio server script in testRoot
	stdioScript := filepath.Join(testRoot, "stdio_server.py")
	stdioContent := `import sys, json

for line in sys.stdin:
    line = line.strip()
    if not line: continue
    msg = json.loads(line)
    msg_id = msg.get("id")
    method = msg.get("method")
    if method == "initialize":
        resp = {"jsonrpc":"2.0","id":msg_id,"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"stdio-test","version":"1.0"}}}
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        resp = {"jsonrpc":"2.0","id":msg_id,"result":{"tools":[{"name":"calc_square","description":"Squares an integer via stdio","inputSchema":{"type":"object","properties":{"n":{"type":"integer"}},"required":["n"]}}]}}
    elif method == "tools/call":
        n = msg.get("params",{}).get("arguments",{}).get("n", 0)
        resp = {"jsonrpc":"2.0","id":msg_id,"result":{"content":[{"type":"text","text":f"SQUARE_SUCCESS:{n*n}"}]}}
    else:
        resp = {"jsonrpc":"2.0","id":msg_id,"result":{}}
    sys.stdout.write(json.dumps(resp)+"\n")
    sys.stdout.flush()
`
	if err := os.WriteFile(stdioScript, []byte(stdioContent), 0o755); err != nil {
		t.Fatal(err)
	}

	// 2. Start Streamable HTTP server
	var mu sync.Mutex
	var httpRequests []string
	var recordedSessionID string
	const sessionToken = "session-test-e2e-888"

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serverPort := listener.Addr().(*net.TCPAddr).Port
	httpURL := fmt.Sprintf("http://127.0.0.1:%d/mcp", serverPort)

	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()

		sessHdr := r.Header.Get("Mcp-Session-Id")
		if sessHdr != "" {
			recordedSessionID = sessHdr
		}
		var buf bytes.Buffer
		buf.ReadFrom(r.Body)
		bodyStr := buf.String()
		httpRequests = append(httpRequests, fmt.Sprintf("%s %s (Session: %s): %s", r.Method, r.URL.Path, sessHdr, bodyStr))

		var msg map[string]any
		json.Unmarshal([]byte(bodyStr), &msg)
		method, _ := msg["method"].(string)
		msgID := msg["id"]

		var resp map[string]any
		switch method {
		case "server/discover":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      msgID,
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "http-test", "version": "1.0"},
				},
			}
		case "initialize":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      msgID,
				"result": map[string]any{
					"protocolVersion": "2025-03-26",
					"capabilities":    map[string]any{"tools": map[string]any{}},
					"serverInfo":      map[string]any{"name": "http-test", "version": "1.0"},
				},
			}
		case "notifications/initialized":
			w.Header().Set("Mcp-Session-Id", sessionToken)
			w.WriteHeader(http.StatusOK)
			return
		case "tools/list":
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      msgID,
				"result": map[string]any{
					"tools": []any{
						map[string]any{
							"name":        "calc_cube",
							"description": "Cubes an integer via Streamable HTTP",
							"inputSchema": map[string]any{
								"type":       "object",
								"properties": map[string]any{"n": map[string]any{"type": "integer"}},
								"required":   []any{"n"},
							},
						},
					},
				},
			}
		case "tools/call":
			params, _ := msg["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			nFloat, _ := args["n"].(float64)
			n := int(nFloat)
			cube := n * n * n
			resp = map[string]any{
				"jsonrpc": "2.0",
				"id":      msgID,
				"result": map[string]any{
					"content": []any{
						map[string]any{"type": "text", "text": fmt.Sprintf("CUBE_SUCCESS:%d", cube)},
					},
				},
			}
		default:
			resp = map[string]any{"jsonrpc": "2.0", "id": msgID, "result": map[string]any{}}
		}

		data, _ := json.Marshal(resp)
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Mcp-Session-Id", sessionToken)
		w.WriteHeader(http.StatusOK)
		w.Write(data)
	})

	httpServer := &http.Server{Handler: mux}
	go httpServer.Serve(listener)
	defer httpServer.Shutdown(context.Background())

	// 3. Prepare MCP config via Multica's prepareAntigravityMcpConfig
	manifest := &sidecarManifest{}
	mcpConfigRaw := json.RawMessage(fmt.Sprintf(`{
		"mcpServers": {
			"local_stdio": {
				"command": "python3",
				"args": ["%s"]
			},
			"remote_http": {
				"url": "%s",
				"headers": {"Authorization": "Bearer test-e2e-token"}
			}
		}
	}`, stdioScript, httpURL))

	if err := prepareAntigravityMcpConfig(taskDir, "antigravity", mcpConfigRaw, manifest); err != nil {
		t.Fatalf("prepareAntigravityMcpConfig failed: %v", err)
	}

	// Verify plugin sidecars exist
	pluginManifestPath := filepath.Join(taskDir, ".agents", "plugins", "multica", "plugin.json")
	mcpConfigFile := filepath.Join(taskDir, ".agents", "plugins", "multica", "mcp_config.json")
	if _, err := os.Stat(pluginManifestPath); err != nil {
		t.Fatalf("plugin.json missing: %v", err)
	}
	if _, err := os.Stat(mcpConfigFile); err != nil {
		t.Fatalf("mcp_config.json missing: %v", err)
	}

	// Verify serverUrl and url both exist in mcp_config.json
	configBytes, _ := os.ReadFile(mcpConfigFile)
	var generatedCfg struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if err := json.Unmarshal(configBytes, &generatedCfg); err != nil {
		t.Fatalf("unmarshal generated mcp_config: %v", err)
	}
	remoteCfg := generatedCfg.McpServers["remote_http"]
	if remoteCfg["url"] != httpURL || remoteCfg["serverUrl"] != httpURL {
		t.Fatalf("remote_http url/serverUrl mismatch: %v", remoteCfg)
	}
	t.Logf("Generated mcp_config validated: url=%s, serverUrl=%s", remoteCfg["url"], remoteCfg["serverUrl"])

	// 4. Run real agy CLI in taskDir with dangerously-skip-permissions
	cmdCtx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	prompt := "Use calc_square with n=12, and use calc_cube with n=7. Output exactly the tool result text strings."
	agyCmd := exec.CommandContext(cmdCtx, agyPath, "-p", prompt, "--dangerously-skip-permissions", "--model", "gemini-3.8-flash-low")
	agyCmd.Dir = taskDir
	var outBuf, errBuf bytes.Buffer
	agyCmd.Stdout = &outBuf
	agyCmd.Stderr = &errBuf

	t.Logf("Executing: agy -p %q (dir=%s)", prompt, taskDir)
	runErr := agyCmd.Run()
	output := outBuf.String()
	stderr := errBuf.String()

	t.Logf("agy stdout:\n%s", output)
	if stderr != "" {
		t.Logf("agy stderr:\n%s", stderr)
	}
	if runErr != nil {
		t.Fatalf("agy execution failed: %v", runErr)
	}

	// Verify tool results
	if !strings.Contains(output, "144") || !strings.Contains(output, "SQUARE_SUCCESS") {
		t.Errorf("output missing SQUARE_SUCCESS:144, got: %s", output)
	}
	if !strings.Contains(output, "343") || !strings.Contains(output, "CUBE_SUCCESS") {
		t.Errorf("output missing CUBE_SUCCESS:343, got: %s", output)
	}

	// 5. Verify HTTP handshake and Mcp-Session-Id
	mu.Lock()
	reqCount := len(httpRequests)
	sessID := recordedSessionID
	mu.Unlock()

	if reqCount < 3 {
		t.Errorf("expected at least 3 HTTP requests (init, tools/list, tools/call), got %d", reqCount)
	}
	if sessID != sessionToken {
		t.Errorf("expected agy to pass Mcp-Session-Id %q, got %q", sessionToken, sessID)
	}
	t.Logf("Verified %d HTTP MCP requests with session %s", reqCount, sessID)

	// 6. Test Multi-Task Isolation: Run agy in an isolated directory taskDir2 without MCP
	taskDir2 := filepath.Join(testRoot, "task-isolated-2")
	if err := os.MkdirAll(taskDir2, 0o755); err != nil {
		t.Fatal(err)
	}
	agyCmd2 := exec.CommandContext(cmdCtx, agyPath, "-p", "List available tools from MCP. If none, reply NONE.", "--dangerously-skip-permissions", "--model", "gemini-3.8-flash-low")
	agyCmd2.Dir = taskDir2
	var out2 bytes.Buffer
	agyCmd2.Stdout = &out2
	if err := agyCmd2.Run(); err == nil {
		outStr := out2.String()
		t.Logf("Isolated taskDir2 output: %s", outStr)
		if strings.Contains(outStr, "calc_square") || strings.Contains(outStr, "calc_cube") {
			t.Errorf("isolated taskDir2 leaked MCP tools: %s", outStr)
		}
	}

	// 7. Test Teardown / CleanupSidecars
	envRoot := filepath.Join(testRoot, "env-root")
	if err := os.MkdirAll(envRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := writeSidecarManifest(envRoot, manifest); err != nil {
		t.Fatalf("writeSidecarManifest failed: %v", err)
	}
	if err := CleanupSidecars(envRoot); err != nil {
		t.Fatalf("CleanupSidecars failed: %v", err)
	}
	if _, err := os.Stat(pluginManifestPath); !os.IsNotExist(err) {
		t.Errorf("plugin.json was not cleaned up: %v", err)
	}
	if _, err := os.Stat(mcpConfigFile); !os.IsNotExist(err) {
		t.Errorf("mcp_config.json was not cleaned up: %v", err)
	}
	t.Log("CleanupSidecars successfully purged all managed sidecars")
}
