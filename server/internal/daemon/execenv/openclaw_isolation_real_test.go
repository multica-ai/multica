//go:build agentintegration

package execenv

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicitly opt-in: this calls a real provider and retains its private audit.
func TestOpenclawIsolatedLocalCanary(t *testing.T) {
	if os.Getenv("MULTICA_RUN_REAL_AGENT_SMOKE") != "1" {
		t.Skip("real-agent smoke not authorized")
	}
	root := os.Getenv("MULTICA_OPENCLAW_CANARY_DIR")
	bin := os.Getenv("MULTICA_OPENCLAW_CANARY_BIN")
	if root == "" || bin == "" {
		t.Fatal("explicit private canary directory and CLI path required")
	}
	work := filepath.Join(root, "workdir")
	skills := []string{"github-actions-hardening", "neo-architecture", "neo-quality-gates", "neo-rke2-operations", "multica-platform"}
	for _, name := range skills {
		if _, err := os.Stat(filepath.Join(work, "skills", name, "SKILL.md")); err != nil {
			t.Fatal(err)
		}
	}
	result, err := prepareOpenclawConfig(root, work, OpenclawConfigPrep{OpenclawBin: bin, IsolatedState: true, PreparedSkills: skills})
	if err != nil {
		t.Fatal(err)
	}
	env := append(os.Environ(), "OPENCLAW_CONFIG_PATH="+result.ConfigPath, "OPENCLAW_INCLUDE_ROOTS="+result.IncludeRoot, "OPENCLAW_STATE_DIR="+filepath.Join(root, "openclaw-state"), "OPENCLAW_AGENT_DIR=", "PI_CODING_AGENT_DIR=")
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Second)
	defer cancel()
	sid := "multica-compat-" + time.Now().UTC().Format("20060102T150405.000000000")
	cmd := exec.CommandContext(ctx, bin, "agent", "--local", "--json", "--session-id", sid, "--agent", "main", "--model", "codex-lb/gpt-6-astra", "--timeout", "60", "--message", "Disposable compatibility canary only. Do not invoke ANY tool, skill, MCP, shell, file, repository, platform or API operation. Do not run any standing task. Reply exactly MULTICA_ISOLATED_CANARY_OK.")
	cmd.Env = env
	cmd.Dir = work
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, runErr := cmd.Output()
	if err := os.WriteFile(filepath.Join(root, "stdout.private.json"), out, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "stderr.private.log"), []byte(stderr.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	if runErr != nil {
		t.Fatalf("CLI failed: %v (private stderr retained)", runErr)
	}
	var reply struct {
		Payloads []struct {
			Text string `json:"text"`
		} `json:"payloads"`
		Meta struct {
			AgentMeta struct {
				SessionID       string         `json:"sessionId"`
				Usage           map[string]any `json:"usage"`
				BridgeCalls     map[string]int `json:"bridgeCalls"`
				TerminalReceipt struct {
					SuccessfulToolNames []string `json:"successfulToolNames"`
				} `json:"terminalReceipt"`
			} `json:"agentMeta"`
			Report struct {
				Workspace string `json:"workspaceDir"`
				Skills    struct {
					Entries []struct {
						Name string `json:"name"`
					} `json:"entries"`
				} `json:"skills"`
			} `json:"systemPromptReport"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(out, &reply); err != nil {
		t.Fatal(err)
	}
	if len(reply.Payloads) != 1 || reply.Payloads[0].Text != "MULTICA_ISOLATED_CANARY_OK" {
		t.Fatal("unexpected assistant output")
	}
	if reply.Meta.Report.Workspace != work {
		t.Fatal("wrong workspace")
	}
	if reply.Meta.AgentMeta.SessionID != sid || len(reply.Meta.AgentMeta.Usage) == 0 {
		t.Fatal("missing session/usage")
	}
	found := map[string]bool{}
	for _, s := range reply.Meta.Report.Skills.Entries {
		found[s.Name] = true
	}
	for _, s := range skills {
		if !found[s] {
			t.Errorf("missing task skill %s", s)
		}
	}
	if len(reply.Meta.AgentMeta.TerminalReceipt.SuccessfulToolNames) > 0 {
		t.Fatal("unexpected successful tools")
	}
	for _, n := range reply.Meta.AgentMeta.BridgeCalls {
		if n != 0 {
			t.Fatal("unexpected tool bridge calls")
		}
	}
	t.Logf("session=%s workdir=%s expectedSkills=%d tools=0", sid, work, len(skills))
}
