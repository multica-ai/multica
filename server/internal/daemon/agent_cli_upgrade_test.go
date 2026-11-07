package daemon

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
	"github.com/multica-ai/multica/server/pkg/agentcli"
)

func TestUpgradeManagedAgentCLIGrokUnsupported(t *testing.T) {
	orig := managedCLIUpgrader
	t.Cleanup(func() { managedCLIUpgrader = orig })

	runner := &recordingCLIRunner{}
	managedCLIUpgrader = func(d *Daemon) *agentcli.Upgrader {
		return &agentcli.Upgrader{
			Runner:  runner,
			HomeDir: t.TempDir(),
			LookPath: func(string) (string, error) {
				return "", errors.New("missing")
			},
			Stat: func(string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
		}
	}

	d := &Daemon{}
	_, err := d.UpgradeManagedAgentCLI(context.Background(), agentcli.CLIGrok)
	if !errors.Is(err, agentcli.ErrNoInstallSource) {
		t.Fatalf("err = %v, want ErrNoInstallSource", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("unexpected npm calls: %#v", runner.calls)
	}
}

func TestUpgradeManagedAgentCLICodexRunsNPMAndStoresProbe(t *testing.T) {
	orig := managedCLIUpgrader
	t.Cleanup(func() { managedCLIUpgrader = orig })

	runner := &recordingCLIRunner{out: "ok"}
	managedCLIUpgrader = func(d *Daemon) *agentcli.Upgrader {
		return &agentcli.Upgrader{
			Runner:  runner,
			HomeDir: "/home/t",
			Prefix:  "/home/t/.local",
			LookPath: func(string) (string, error) {
				return "", errors.New("missing")
			},
			Stat: func(string) (os.FileInfo, error) {
				return nil, os.ErrNotExist
			},
			ProbeOne: func(ctx context.Context, cli string) (agentcli.Probe, error) {
				return agentcli.Probe{
					CLI:      cli,
					Current:  "1.2.3",
					Path:     "/home/t/.local/bin/codex",
					Channel:  agentcli.ChannelNPM,
					ProbedAt: time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC).Format(time.RFC3339),
				}, nil
			},
			Now: func() time.Time { return time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC) },
		}
	}

	d := &Daemon{}
	res, err := d.UpgradeManagedAgentCLI(context.Background(), agentcli.CLICodex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != agentcli.ActionUpgraded {
		t.Fatalf("action = %q", res.Action)
	}
	if len(runner.calls) != 1 {
		t.Fatalf("calls = %#v", runner.calls)
	}
	wantArgs := []string{"install", "-g", "--prefix", "/home/t/.local", "@openai/codex@latest"}
	if runner.calls[0].name != "npm" || len(runner.calls[0].args) != len(wantArgs) {
		t.Fatalf("call = %#v", runner.calls[0])
	}
	for i := range wantArgs {
		if runner.calls[0].args[i] != wantArgs[i] {
			t.Fatalf("args = %#v, want %#v", runner.calls[0].args, wantArgs)
		}
	}
	probes := d.managedCLIProbes()
	if len(probes) != 1 || probes[0].CLI != agentcli.CLICodex || probes[0].Current != "1.2.3" {
		t.Fatalf("probes = %#v", probes)
	}
}

func TestEnsureManagedAgentCLIGrokPresentNoOp(t *testing.T) {
	orig := managedCLIUpgrader
	t.Cleanup(func() { managedCLIUpgrader = orig })

	runner := &recordingCLIRunner{}
	managedCLIUpgrader = func(d *Daemon) *agentcli.Upgrader {
		return &agentcli.Upgrader{
			Runner:  runner,
			HomeDir: t.TempDir(),
			Getenv: func(k string) string {
				if k == "MULTICA_GROK_PATH" {
					return "/opt/grok/bin/grok"
				}
				return ""
			},
			Stat: func(path string) (os.FileInfo, error) {
				if path == "/opt/grok/bin/grok" {
					return fakeFile{}, nil
				}
				return nil, os.ErrNotExist
			},
			ProbeOne: func(ctx context.Context, cli string) (agentcli.Probe, error) {
				return agentcli.Probe{CLI: cli, Current: "0.1.0", Path: "/opt/grok/bin/grok", Channel: agentcli.ChannelBinary}, nil
			},
		}
	}

	d := &Daemon{}
	res, err := d.EnsureManagedAgentCLI(context.Background(), agentcli.CLIGrok)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != agentcli.ActionAlreadyPresent {
		t.Fatalf("action = %q", res.Action)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestUpgradeManagedAgentCLIPrefersPrefixOverBrew(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("MULTICA_CODEX_PATH", "") // clear any operator pin

	brewDir := filepath.Join(home, "homebrew", "bin")
	prefixBinDir := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(brewDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(prefixBinDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, ".local", "lib", "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	brewPath := filepath.Join(brewDir, "codex")
	prefixPath := filepath.Join(prefixBinDir, "codex")
	writeTestExecutable(t, brewPath, []byte("#!/bin/sh\necho brew\n"))
	writeTestExecutable(t, prefixPath, []byte("#!/bin/sh\necho npm\n"))

	origDetect := detectAgentVersion
	t.Cleanup(func() { detectAgentVersion = origDetect })
	detectAgentVersion = func(_ context.Context, cmd agent.Command) (string, error) {
		switch cmd.Path {
		case brewPath:
			return "0.1.0", nil
		case prefixPath:
			return "2.0.0", nil
		default:
			return "", fmt.Errorf("unexpected path %q", cmd.Path)
		}
	}

	orig := managedCLIUpgrader
	t.Cleanup(func() { managedCLIUpgrader = orig })
	runner := &recordingCLIRunner{out: "ok"}
	managedCLIUpgrader = func(d *Daemon) *agentcli.Upgrader {
		return &agentcli.Upgrader{
			Runner:  runner,
			HomeDir: home,
			Prefix:  filepath.Join(home, ".local"),
			LookPath: func(name string) (string, error) {
				if name == "codex" {
					return brewPath, nil
				}
				return "", errors.New("missing")
			},
			Stat: func(path string) (os.FileInfo, error) {
				return os.Stat(path)
			},
			ProbeOne: d.probeManagedAgentCLI,
		}
	}

	d := &Daemon{}
	d.agentsAvailable.Store(&map[string]AgentEntry{
		agentcli.CLICodex: {Path: brewPath, Command: "codex"},
	})
	d.resolvedPaths = map[string]healedAgent{
		agentcli.CLICodex: {path: brewPath, version: "0.1.0"},
	}

	res, err := d.UpgradeManagedAgentCLI(context.Background(), agentcli.CLICodex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Probe.Path != prefixPath {
		t.Fatalf("probe path = %q, want prefix %q (brew was %q)", res.Probe.Path, prefixPath, brewPath)
	}
	if res.Probe.Current != "2.0.0" {
		t.Fatalf("probe current = %q, want 2.0.0 from prefix bin", res.Probe.Current)
	}
	agents := d.agents()
	if got := agents[agentcli.CLICodex].Path; got != prefixPath {
		t.Fatalf("published path = %q, want prefix %q", got, prefixPath)
	}
	d.resolvedPathsMu.RLock()
	healed := d.resolvedPaths[agentcli.CLICodex]
	d.resolvedPathsMu.RUnlock()
	if healed.path != prefixPath || healed.version != "2.0.0" {
		t.Fatalf("resolvedPaths = %#v, want prefix@2.0.0", healed)
	}
}

type recordingCLIRunner struct {
	calls []struct {
		name string
		args []string
	}
	out string
	err error
}

func (r *recordingCLIRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	r.calls = append(r.calls, struct {
		name string
		args []string
	}{name: name, args: append([]string(nil), args...)})
	return r.out, r.err
}

type fakeFile struct{}

func (fakeFile) Name() string       { return "x" }
func (fakeFile) Size() int64        { return 1 }
func (fakeFile) Mode() os.FileMode  { return 0o755 }
func (fakeFile) ModTime() time.Time { return time.Time{} }
func (fakeFile) IsDir() bool        { return false }
func (fakeFile) Sys() any           { return nil }
