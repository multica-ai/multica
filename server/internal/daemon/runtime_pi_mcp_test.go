package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func piInventoryHome(t *testing.T, version string) (string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("PI_MCP_CONFIG_MODE", "")
	dir := filepath.Join(home, ".pi", "agent")
	writePiInventoryFile(t, filepath.Join(dir, "settings.json"), `{"packages":["npm:pi-mcp-adapter"]}`)
	root := filepath.Join(dir, "npm", "node_modules", "pi-mcp-adapter")
	writePiInventoryFile(t, filepath.Join(root, "package.json"), `{"name":"pi-mcp-adapter","version":"`+version+`","pi":{"extensions":["./index.ts"]}}`)
	writePiInventoryFile(t, filepath.Join(root, "index.ts"), `export function createMcpAdapter() { throw new Error("inventory must not execute extensions"); }`)
	return home, dir
}

func writePiInventoryFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestListRuntimeLocalMcpServersPiMatchesSummaryContract(t *testing.T) {
	for _, tc := range []struct{ version, file string }{{"2.37.0", "mcp.json"}, {"3.1.0", "mcp-adapter.json"}} {
		t.Run(tc.version, func(t *testing.T) {
			_, dir := piInventoryHome(t, tc.version)
			raw := `{"mcpServers":{"Zulu":{"command":"do-not-run","args":["--key","secret-argument"],"env":{"TOKEN":"secret-env"}},"alpha":{"url":"https://secret.example/mcp","headers":{"Authorization":"secret-header"},"disabled":true},"ignored":42}}`
			path := filepath.Join(dir, tc.file)
			writePiInventoryFile(t, path, raw)
			rows, supported, err := listRuntimeLocalMcpServers("pi")
			if err != nil {
				t.Fatal(err)
			}
			want := []runtimeLocalMcpServerSummary{{Name: "alpha", Transport: "http", Source: "User config", Enabled: false}, {Name: "Zulu", Transport: "stdio", Source: "User config", Enabled: true}}
			if !supported || !reflect.DeepEqual(rows, want) {
				t.Fatalf("supported=%v summaries=%#v", supported, rows)
			}
			wire, _ := json.Marshal(rows)
			if string(wire) != `[{"name":"alpha","transport":"http","source":"User config","enabled":false},{"name":"Zulu","transport":"stdio","source":"User config","enabled":true}]` {
				t.Fatalf("unexpected wire contract: %s", wire)
			}
			after, _ := os.ReadFile(path)
			if string(after) != raw {
				t.Fatal("inventory rewrote Pi config")
			}
		})
	}
}

func TestListRuntimeLocalMcpServersPiOverridesAndIsolation(t *testing.T) {
	home, dir := piInventoryHome(t, "2.37.0")
	writePiInventoryFile(t, filepath.Join(home, ".config/mcp/mcp.json"), `{"mcpServers":{"shared":{"command":"do-not-run"},"switched":{"command":"old"}}}`)
	writePiInventoryFile(t, filepath.Join(home, ".agents/mcp.json"), `{"mcpServers":{"agents":{"command":"a","enabled":false}}}`)
	writePiInventoryFile(t, filepath.Join(home, ".agents/mcp/mcp.json"), `{"mcpServers":{"nested":{"url":"https://nested.invalid"}}}`)
	writePiInventoryFile(t, filepath.Join(dir, "mcp.json"), `{// supported JSONC
 "mcpServers":{"shared":{"disabled":true},"switched":{"url":"https://private.invalid"}},}`)
	rows, supported, err := listRuntimeLocalMcpServers("pi")
	if err != nil || !supported {
		t.Fatalf("supported=%v err=%v", supported, err)
	}
	want := []runtimeLocalMcpServerSummary{{Name: "agents", Transport: "stdio", Source: "User config", Enabled: true}, {Name: "nested", Transport: "http", Source: "User config", Enabled: true}, {Name: "shared", Transport: "stdio", Source: "User config", Enabled: false}, {Name: "switched", Transport: "http", Source: "User config", Enabled: true}}
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("summaries=%#v", rows)
	}
	for _, raw := range []json.RawMessage{nil, json.RawMessage(`null`), json.RawMessage(`{}`), json.RawMessage(`{"mcpServers":{}}`), json.RawMessage(`{"mcpServers":{"assigned":{"command":"assigned"}}}`)} {
		got, err := mergeRuntimeAndAgentMcpConfig("pi", raw)
		if err != nil || string(got) != string(raw) {
			t.Fatalf("inventory changed managed isolation: %s, %v", got, err)
		}
	}
	t.Setenv("PI_MCP_CONFIG_MODE", "exclusive")
	rows, supported, err = listRuntimeLocalMcpServers("pi")
	if err != nil || !supported || len(rows) != 2 {
		t.Fatalf("exclusive summaries=%#v err=%v", rows, err)
	}
}

func TestListRuntimeLocalMcpServersPiMissingInvalidAndUnavailable(t *testing.T) {
	_, dir := piInventoryHome(t, "3.1.0")
	// 3.x must not adopt the file that belongs to native Pi MCP.
	writePiInventoryFile(t, filepath.Join(dir, "mcp.json"), `{"mcpServers":{"native":{"command":"no"}}}`)
	rows, supported, err := listRuntimeLocalMcpServers("pi")
	if err != nil || !supported || len(rows) != 0 {
		t.Fatalf("missing config: %#v %v %v", rows, supported, err)
	}
	writePiInventoryFile(t, filepath.Join(dir, "mcp-adapter.json"), `{"mcpServers": SECRET_DO_NOT_LEAK}`)
	_, _, err = listRuntimeLocalMcpServers("pi")
	if err == nil || strings.Contains(err.Error(), "SECRET_DO_NOT_LEAK") {
		t.Fatalf("invalid config should fail without secrets: %v", err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", t.TempDir())
	rows, supported, err = listRuntimeLocalMcpServers("pi")
	if err != nil || supported || len(rows) != 0 {
		t.Fatalf("missing plugin: %#v %v %v", rows, supported, err)
	}
}

func TestListRuntimeLocalMcpServersPiCustomAgentDir(t *testing.T) {
	_, dir := piInventoryHome(t, "2.37.0")
	custom := filepath.Join(t.TempDir(), "pi-settings")
	if err := os.Rename(dir, custom); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PI_CODING_AGENT_DIR", custom)
	writePiInventoryFile(t, filepath.Join(custom, "mcp.json"), `{"mcp-servers":{"custom":{"url":"https://custom.invalid","disabled":true}}}`)
	rows, supported, err := listRuntimeLocalMcpServers("pi")
	if err != nil || !supported || len(rows) != 1 || rows[0].Name != "custom" || rows[0].Enabled {
		t.Fatalf("custom directory: %#v %v %v", rows, supported, err)
	}
}

func TestListRuntimeLocalMcpServersPiKeepsExplicitHTTPTransport(t *testing.T) {
	home, dir := piInventoryHome(t, "2.37.0")
	writePiInventoryFile(t, filepath.Join(home, ".config/mcp/mcp.json"), `{"mcpServers":{"stream":{"url":"https://old.invalid","httpTransport":"sse"}}}`)
	writePiInventoryFile(t, filepath.Join(dir, "mcp.json"), `{"mcpServers":{"stream":{"url":"https://new.invalid"}}}`)
	rows, _, err := listRuntimeLocalMcpServers("pi")
	if err != nil || len(rows) != 1 || rows[0].Transport != "sse" {
		t.Fatalf("transport override lost: %#v %v", rows, err)
	}
}

func TestListRuntimeLocalMcpServersPiNormalizesNativeDisabledFlag(t *testing.T) {
	home, dir := piInventoryHome(t, "2.37.0")
	writePiInventoryFile(t, filepath.Join(home, ".config/mcp/mcp.json"), `{"mcpServers":{"server":{"command":"no-execution","disabled":true}}}`)
	writePiInventoryFile(t, filepath.Join(dir, "mcp.json"), `{"mcpServers":{"server":{"disabled":null,"enabled":false}}}`)
	rows, _, err := listRuntimeLocalMcpServers("pi")
	if err != nil || len(rows) != 1 || !rows[0].Enabled {
		t.Fatalf("native disabled flag mismatch: %#v %v", rows, err)
	}
}
