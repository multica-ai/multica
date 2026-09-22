package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	agentpkg "github.com/multica-ai/multica/server/pkg/agent"
)

// TestOpenCodeV2RefusesSynthesizedMCPWithoutAgentConfig covers the seam between
// what the daemon composes and what the OpenCode backend accepts.
//
// ExecOptions.McpConfig is not agent.mcp_config. The daemon folds several
// sources into it — workspace MCP servers bound to the agent (at claim), the
// task's integration servers and the plugin-hook tool server (at run, via
// mergeTaskRemoteMCPConfig), and the runtime's own servers. An agent with no MCP
// configuration of its own therefore still reaches a 2.x runtime carrying
// servers, and that run is refused.
//
// This is here rather than in pkg/agent because only this side can use the real
// merge helper: pkg/agent cannot import the daemon. It pins both halves — that
// the daemon really does synthesize a config from nothing, and that the refusal
// fires on the result.
func TestOpenCodeV2RefusesSynthesizedMCPWithoutAgentConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake CLI is a shell script")
	}

	// What a task with integration or plugin-hook tools contributes. The agent
	// itself has nothing configured: base is nil.
	overlay := json.RawMessage(`{"mcpServers":{"composio-github":{"type":"remote","url":"https://example.invalid/mcp","headers":{"Authorization":"Bearer FAKE-WIRING-TOKEN"}}}}`)
	composed, err := mergeTaskRemoteMCPConfig(nil, overlay)
	if err != nil {
		t.Fatalf("merge task remote MCP config: %v", err)
	}
	if len(composed) == 0 {
		t.Fatal("expected the daemon to synthesize an MCP config from a nil agent config")
	}

	dir := t.TempDir()
	fakeCLI := filepath.Join(dir, "opencode")
	if err := os.WriteFile(fakeCLI, []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("write fake cli: %v", err)
	}

	backend, err := agentpkg.New("opencode", agentpkg.Config{
		ExecutablePath: fakeCLI,
		CLIVersion:     "opencode v2.0.10",
		BuiltinRuntime: true,
		Logger:         slog.Default(),
	})
	if err != nil {
		t.Fatalf("new opencode backend: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, execErr := backend.Execute(ctx, "prompt-ignored", agentpkg.ExecOptions{
		Cwd:       t.TempDir(),
		McpConfig: composed,
		Timeout:   5 * time.Second,
	})
	if !errors.Is(execErr, agentpkg.ErrOpenCodeV2MCPUnsupported) {
		t.Fatalf("expected the composed MCP config to be refused, got %v", execErr)
	}

	// The operator's way out has to be reachable. Naming agent.mcp_config alone
	// would point at a field that is already empty in exactly this case.
	message := execErr.Error()
	for _, want := range []string{"1.x", "workspace", "integration", "plugin"} {
		if !strings.Contains(message, want) {
			t.Fatalf("refusal should name the %q recovery path for non-agent MCP sources: %s", want, message)
		}
	}
}
