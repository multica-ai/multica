package piharness

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestPrepareWritesPrivateManagedExtension(t *testing.T) {
	path, err := Prepare(t.TempDir())
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if filepath.Base(path) != "multica-harness.ts" {
		t.Fatalf("unexpected extension path %q", path)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat extension: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("extension mode = %o, want 600", got)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read extension: %v", err)
	}
	for _, contract := range []string{`["mcp", "serve"]`, "registerTool", `pi.on("tool_call"`, "MULTICA_DAEMON_PORT"} {
		if !strings.Contains(string(body), contract) {
			t.Errorf("managed extension missing %q", contract)
		}
	}
}

func TestManagedArgsOwnsExtensionAndToolRegistry(t *testing.T) {
	got, err := ManagedArgs([]string{
		"--theme", "dark",
		"-e", "/tmp/untrusted.ts",
		"--extension=/tmp/also-untrusted.ts",
		"--tools", "read,bash",
		"--no-tools",
		"--no-extensions",
	}, "/managed/multica-harness.ts")
	if err != nil {
		t.Fatalf("ManagedArgs: %v", err)
	}
	want := []string{"--theme", "dark", "--no-extensions", "-e", "/managed/multica-harness.ts"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ManagedArgs = %#v, want %#v", got, want)
	}
}

func TestManagedArgsRejectsMissingExtension(t *testing.T) {
	if _, err := ManagedArgs(nil, ""); err == nil {
		t.Fatal("ManagedArgs accepted an empty managed extension path")
	}
}

func TestPrepareConnectionsValidatesAndWritesPrivateConfig(t *testing.T) {
	path, err := PrepareConnections(t.TempDir(), []byte(`{"mcpServers":{"finance":{"type":"http","url":"https://example.test/mcp","headers":{"Authorization":"Bearer secret"}}}}`))
	if err != nil {
		t.Fatalf("PrepareConnections: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("config mode = %o, want 600", got)
	}
	if _, err := PrepareConnections(t.TempDir(), []byte(`{"mcpServers":{"broken":"not-an-object"}}`)); err == nil {
		t.Fatal("malformed MCP server config was accepted")
	}
}
