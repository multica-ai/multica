package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agentcli"
)

func TestClassifyAgentCLIChannel(t *testing.T) {
	root := t.TempDir()

	npmBin := filepath.Join(root, ".local", "bin", "codex")
	npmTarget := filepath.Join(root, ".local", "lib", "node_modules", "@openai", "codex", "bin", "codex")
	if err := os.MkdirAll(filepath.Dir(npmTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(npmTarget, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(npmBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(npmTarget, npmBin); err != nil {
		t.Fatal(err)
	}
	if got := classifyAgentCLIChannel(npmBin); got != agentcli.ChannelNPM {
		t.Fatalf("npm shim channel = %s", got)
	}

	brewBin := filepath.Join(root, "homebrew", "bin", "claude")
	brewTarget := filepath.Join(root, "homebrew", "Cellar", "claude", "2.1.0", "bin", "claude")
	if err := os.MkdirAll(filepath.Dir(brewTarget), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(brewTarget, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(brewBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(brewTarget, brewBin); err != nil {
		t.Fatal(err)
	}
	if got := classifyAgentCLIChannel(brewBin); got != agentcli.ChannelBrew {
		t.Fatalf("brew shim channel = %s", got)
	}

	grokBin := filepath.Join(root, ".grok", "bin", "grok")
	if err := os.MkdirAll(filepath.Dir(grokBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokBin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := classifyAgentCLIChannel(grokBin); got != agentcli.ChannelBinary {
		t.Fatalf("grok binary channel = %s, want binary", got)
	}
	if got := classifyAgentCLIChannel(""); got != agentcli.ChannelUnknown {
		t.Fatalf("empty path channel = %s", got)
	}
}

func TestBuildManagedCLIProbesUsesRoundAndCache(t *testing.T) {
	grokBin := filepath.Join(t.TempDir(), ".grok", "bin", "grok")
	if err := os.MkdirAll(filepath.Dir(grokBin), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(grokBin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	codexBin := filepath.Join(t.TempDir(), "node_modules", "@openai", "codex", "bin", "codex")
	d := &Daemon{agentVersions: map[string]string{}}
	d.cfg.Agents = map[string]AgentEntry{
		"codex": {Path: codexBin},
		"grok":  {Path: grokBin},
	}
	// A version cached for a CLI this round did not see must not be reported.
	d.setAgentVersion("claude", "should-not-win")
	d.setAgentVersion("grok", "1.0.14")
	probes := d.buildManagedCLIProbes(map[string]managedCLIObservation{
		"codex": {version: "0.98.0", path: codexBin},
		"grok":  {path: grokBin},
	})
	if len(probes) != 3 {
		t.Fatalf("probes = %+v", probes)
	}
	if probes[0].CLI != "codex" || probes[0].Current != "0.98.0" || probes[0].Channel != agentcli.ChannelNPM {
		t.Fatalf("codex = %+v", probes[0])
	}
	if probes[1].CLI != "claude" || probes[1].Current != "" || probes[1].Channel != agentcli.ChannelUnknown {
		t.Fatalf("claude = %+v; a CLI this round did not see is missing, not a stale cache", probes[1])
	}
	if probes[2].CLI != "grok" || probes[2].Current != "1.0.14" || probes[2].Channel != agentcli.ChannelBinary || probes[2].Path != grokBin {
		t.Fatalf("grok = %+v", probes[2])
	}
	if _, err := time.Parse(time.RFC3339, probes[0].ProbedAt); err != nil {
		t.Fatalf("probed_at %q: %v", probes[0].ProbedAt, err)
	}
}

func TestRegistrationAndHeartbeatCarryProbesWithoutReplacingCLIVersion(t *testing.T) {
	var registerBody map[string]any
	var heartbeatBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/daemon/register", "/api/daemon/heartbeat":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if r.URL.Path == "/api/daemon/register" {
				registerBody = body
				_, _ = io.WriteString(w, `{"runtimes":[{"id":"runtime-test","type":"claude"}]}`)
				return
			}
			heartbeatBody = body
			_, _ = io.WriteString(w, `{"status":"ok"}`)
		default:
			// registerRuntimesForWorkspaceBatchLocked GETs runtime-profiles first.
			_, _ = io.WriteString(w, `{"runtime_profiles":[]}`)
		}
	}))
	defer srv.Close()

	d := &Daemon{client: NewClient(srv.URL), logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
	d.cfg.CLIVersion = "v0.7.16"
	d.storeManagedCLIProbes([]agentcli.Probe{
		{CLI: "codex", Current: "1.2.3", Path: "/tmp/codex", Channel: agentcli.ChannelBinary, ProbedAt: "2026-11-02T00:00:00Z"},
		{CLI: "claude", Channel: agentcli.ChannelUnknown, ProbedAt: "2026-11-02T00:00:00Z"},
		{CLI: "grok", Current: "1.0.14", Path: "/home/user/.grok/bin/grok", Channel: agentcli.ChannelBinary, ProbedAt: "2026-11-02T00:00:00Z"},
	})
	builtins := []map[string]string{{"type": "claude", "name": "fixture", "version": "fixture"}}
	if _, _, err := d.registerRuntimesForWorkspaceBatchLocked(context.Background(), "workspace-test", builtins); err != nil {
		t.Fatal(err)
	}
	if registerBody["cli_version"] != "v0.7.16" {
		t.Fatalf("cli_version = %#v, want the daemon version", registerBody["cli_version"])
	}
	probes, _ := registerBody["agent_cli_probes"].([]any)
	if len(probes) != 3 {
		t.Fatalf("register probes = %#v", registerBody["agent_cli_probes"])
	}

	if _, err := d.client.sendHeartbeat(context.Background(), "runtime-test", false, d.managedCLIProbes()); err != nil {
		t.Fatal(err)
	}
	if _, ok := heartbeatBody["cli_version"]; ok {
		t.Fatalf("heartbeat must not send the daemon cli_version, body=%v", heartbeatBody)
	}
	hbProbes, _ := heartbeatBody["agent_cli_probes"].([]any)
	if len(hbProbes) != 3 {
		t.Fatalf("heartbeat probes = %#v", heartbeatBody["agent_cli_probes"])
	}
	first, _ := hbProbes[0].(map[string]any)
	if first["cli"] != "codex" || first["current"] != "1.2.3" || first["channel"] != "binary" {
		t.Fatalf("heartbeat codex probe = %#v", first)
	}
}
