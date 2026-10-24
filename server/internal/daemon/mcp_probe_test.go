package daemon

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func writeFakeMCP(t *testing.T, script string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "mcp-server")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

const fakeMCPStdioOK = `#!/usr/bin/env python3
import json, sys

def read_msg():
    line = sys.stdin.buffer.readline()
    if not line:
        raise SystemExit(0)
    return json.loads(line)

def write_msg(obj):
    raw = json.dumps(obj).encode()
    sys.stdout.buffer.write(raw + b"\n")
    sys.stdout.buffer.flush()

while True:
    msg = read_msg()
    method = msg.get("method")
    if method == "initialize":
        write_msg({"jsonrpc":"2.0","id":msg["id"],"result":{"protocolVersion":"2024-11-05","capabilities":{"tools":{}},"serverInfo":{"name":"fake","version":"0"}}})
    elif method == "notifications/initialized":
        continue
    elif method == "tools/list":
        write_msg({"jsonrpc":"2.0","id":msg["id"],"result":{"tools":[{"name":"search"},{"name":"fetch"}]}})
        break
`

const fakeMCPStdioEmpty = `#!/usr/bin/env python3
import json, sys

def read_msg():
    line = sys.stdin.buffer.readline()
    if not line:
        raise SystemExit(0)
    return json.loads(line)

def write_msg(obj):
    raw = json.dumps(obj).encode()
    sys.stdout.buffer.write(raw + b"\n")
    sys.stdout.buffer.flush()

while True:
    msg = read_msg()
    method = msg.get("method")
    if method == "initialize":
        write_msg({"jsonrpc":"2.0","id":msg["id"],"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"fake","version":"0"}}})
    elif method == "tools/list":
        write_msg({"jsonrpc":"2.0","id":msg["id"],"result":{"tools":[]}})
        break
`

func TestProbeWorkspaceMcp_StdioListsTools(t *testing.T) {
	path := writeFakeMCP(t, fakeMCPStdioOK)
	cfg, _ := json.Marshal(map[string]any{"command": path, "env": map[string]string{"TOKEN": "sk-live-secret"}})
	tools, err := probeWorkspaceMcp(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tools, ",") != "search,fetch" {
		t.Fatalf("tools = %v", tools)
	}
}

func TestProbeWorkspaceMcp_EmptyToolsIsOK(t *testing.T) {
	path := writeFakeMCP(t, fakeMCPStdioEmpty)
	cfg, _ := json.Marshal(map[string]any{"command": path})
	tools, err := probeWorkspaceMcp(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(tools) != 0 {
		t.Fatalf("tools = %v", tools)
	}
}

func TestProbeWorkspaceMcp_CommandNotFound(t *testing.T) {
	cfg, _ := json.Marshal(map[string]any{"command": filepath.Join(t.TempDir(), "missing-binary")})
	_, err := probeWorkspaceMcp(context.Background(), cfg)
	code, msg := classifyMcpProbeError(err)
	if code != protocol.McpProbeCodeCommandNotFound {
		t.Fatalf("code = %s err=%v", code, err)
	}
	if strings.Contains(msg, "missing-binary") {
		t.Fatalf("error leaked command path: %q", msg)
	}
}

func TestProbeWorkspaceMcp_TimeoutKillsChild(t *testing.T) {
	path := writeFakeMCP(t, "#!/bin/sh\nsleep 5\n")
	cfg, _ := json.Marshal(map[string]any{"command": path})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := probeWorkspaceMcp(ctx, cfg)
	code, _ := classifyMcpProbeError(err)
	if code != protocol.McpProbeCodeTimeout {
		t.Fatalf("code = %s err=%v", code, err)
	}
}

func TestProbeWorkspaceMcp_UnsupportedTransport(t *testing.T) {
	cfg, _ := json.Marshal(map[string]any{"type": "websocket", "url": "wss://secret.example/token"})
	_, err := probeWorkspaceMcp(context.Background(), cfg)
	code, msg := classifyMcpProbeError(err)
	if code != protocol.McpProbeCodeUnsupportedTransport {
		t.Fatalf("code = %s err=%v", code, err)
	}
	if strings.Contains(msg, "secret.example") || strings.Contains(msg, "token") {
		t.Fatalf("error leaked url: %q", msg)
	}
}

func TestProbeWorkspaceMcp_HTTPUnauthorized(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	t.Cleanup(srv.Close)
	cfg, _ := json.Marshal(map[string]any{
		"url":     srv.URL,
		"headers": map[string]string{"Authorization": "Bearer sk-live-secret"},
	})
	_, err := probeWorkspaceMcp(context.Background(), cfg)
	code, msg := classifyMcpProbeError(err)
	if code != protocol.McpProbeCodeUnauthorized {
		t.Fatalf("code = %s err=%v", code, err)
	}
	if strings.Contains(msg, "sk-live-secret") || strings.Contains(msg, srv.URL) {
		t.Fatalf("error leaked secret: %q", msg)
	}
}

func TestProbeWorkspaceMcp_HTTPTools(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		switch body["method"] {
		case "initialize":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": body["id"],
				"result": map[string]any{"protocolVersion": "2024-11-05", "capabilities": map[string]any{}},
			})
		case "tools/list":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jsonrpc": "2.0", "id": body["id"],
				"result": map[string]any{"tools": []map[string]any{{"name": "whoami"}}},
			})
		default:
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	t.Cleanup(srv.Close)
	cfg, _ := json.Marshal(map[string]any{"url": srv.URL})
	tools, err := probeWorkspaceMcp(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tools, ",") != "whoami" {
		t.Fatalf("tools = %v", tools)
	}
}

func TestProbeWorkspaceMcp_HTTPStreamAndCleanup(t *testing.T) {
	deleted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deleted = r.Header.Get("Mcp-Session-Id") == "probe-session"
			w.WriteHeader(http.StatusNoContent)
			return
		}
		var body struct {
			ID     int            `json:"id"`
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if body.Method != "initialize" && r.Header.Get("Mcp-Session-Id") != "probe-session" {
			t.Error("missing session id")
		}
		if body.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		var result any
		switch body.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "probe-session")
			result = map[string]any{"protocolVersion": "2025-03-26"}
		case "tools/list":
			if body.Params["cursor"] == "page-2" {
				result = map[string]any{"tools": []map[string]string{{"name": "fetch"}}}
			} else {
				result = map[string]any{"tools": []map[string]string{{"name": "search"}}, "nextCursor": "page-2"}
			}
		default:
			t.Errorf("unexpected method: %s", body.Method)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte("data: {\"jsonrpc\":\"2.0\",\"method\":\"notifications/message\"}\n\n"))
		raw, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": body.ID, "result": result})
		_, _ = w.Write(append(append([]byte("data: "), raw...), '\n', '\n'))
		w.(http.Flusher).Flush()
		// A response must be consumed before the server closes a long-lived stream.
		<-r.Context().Done()
	}))
	defer srv.Close()
	cfg, _ := json.Marshal(map[string]any{"url": srv.URL})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	tools, err := probeWorkspaceMcp(ctx, cfg)
	if err != nil || strings.Join(tools, ",") != "search,fetch" {
		t.Fatalf("tools=%v error=%v", tools, err)
	}
	if !deleted {
		t.Fatal("probe did not terminate its HTTP session")
	}
}

func TestProbeWorkspaceMcp_TLSFailureIsRedacted(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	cfg, _ := json.Marshal(map[string]any{"url": srv.URL + "/secret-token"})
	_, err := probeWorkspaceMcp(context.Background(), cfg)
	code, message := classifyMcpProbeError(err)
	if code != protocol.McpProbeCodeTLS || strings.Contains(message, "secret-token") {
		t.Fatalf("code=%s message=%q", code, message)
	}
}
