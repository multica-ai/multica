package agentcli

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	calls []CommandSpec
	err   error
	out   string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, CommandSpec{Name: name, Args: append([]string(nil), args...)})
	return f.out, f.err
}

func TestNPMInstallLatestSpecTable(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		cli     string
		prefix  string
		wantPkg string
		wantErr error
	}{
		{
			name:    "codex",
			cli:     "codex",
			prefix:  "/tmp/home/.local",
			wantPkg: "@openai/codex@latest",
		},
		{
			name:    "claude",
			cli:     "Claude",
			prefix:  "/tmp/home/.local",
			wantPkg: "@anthropic-ai/claude-code@latest",
		},
		{
			name:    "grok unsupported",
			cli:     "grok",
			prefix:  "/tmp/home/.local",
			wantErr: ErrNoInstallSource,
		},
		{
			name:    "unknown",
			cli:     "zcode",
			prefix:  "/tmp/home/.local",
			wantErr: ErrUnknownCLI,
		},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec, err := NPMInstallLatestSpec(tc.cli, "/tmp/home", tc.prefix)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if spec.Name != "" {
					t.Fatalf("spec = %#v, want empty on error", spec)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if spec.Name != "npm" {
				t.Fatalf("name = %q", spec.Name)
			}
			wantArgs := []string{"install", "-g", "--prefix", tc.prefix, tc.wantPkg}
			if strings.Join(spec.Args, "\x00") != strings.Join(wantArgs, "\x00") {
				t.Fatalf("args = %#v, want %#v", spec.Args, wantArgs)
			}
		})
	}
}

func TestNPMInstallLatestSpecDefaultPrefix(t *testing.T) {
	t.Parallel()
	spec, err := NPMInstallLatestSpec(CLICodex, "/Users/me", "")
	if err != nil {
		t.Fatal(err)
	}
	if got := spec.Args[3]; got != filepath.Join("/Users/me", ".local") {
		t.Fatalf("prefix arg = %q", got)
	}
}

func TestUpgradeRunsNPMForCodexAndClaude(t *testing.T) {
	t.Parallel()
	for _, cli := range []string{CLICodex, CLIClaude} {
		cli := cli
		t.Run(cli, func(t *testing.T) {
			t.Parallel()
			runner := &fakeRunner{out: "ok"}
			u := &Upgrader{
				Runner:  runner,
				HomeDir: "/home/t",
				Prefix:  "/home/t/.local",
				LookPath: func(string) (string, error) {
					return "", errors.New("not on path")
				},
				Stat: func(string) (os.FileInfo, error) {
					return nil, os.ErrNotExist
				},
				Now: func() time.Time { return time.Date(2026, 11, 2, 8, 0, 0, 0, time.UTC) },
			}
			res, err := u.Upgrade(context.Background(), cli)
			if err != nil {
				t.Fatalf("Upgrade: %v", err)
			}
			if res.Action != ActionUpgraded {
				t.Fatalf("action = %q", res.Action)
			}
			if len(runner.calls) != 1 {
				t.Fatalf("calls = %#v", runner.calls)
			}
			pkg, _ := NPMPackage(cli)
			want := CommandSpec{
				Name: "npm",
				Args: []string{"install", "-g", "--prefix", "/home/t/.local", pkg + "@latest"},
			}
			if runner.calls[0].Name != want.Name || strings.Join(runner.calls[0].Args, "\x00") != strings.Join(want.Args, "\x00") {
				t.Fatalf("call = %#v, want %#v", runner.calls[0], want)
			}
		})
	}
}

func TestUpgradeGrokUnsupported(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		// Binary present must still not invent an install source.
		Getenv: func(k string) string {
			if k == "MULTICA_GROK_PATH" {
				return "/home/t/.grok/bin/grok"
			}
			return ""
		},
		Stat: func(path string) (os.FileInfo, error) {
			if path == "/home/t/.grok/bin/grok" {
				return fakeFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
	}
	_, err := u.Upgrade(context.Background(), CLIGrok)
	if !errors.Is(err, ErrNoInstallSource) {
		t.Fatalf("err = %v, want ErrNoInstallSource", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("grok must not shell out: %#v", runner.calls)
	}
}

func TestEnsureGrokAlreadyPresentNoOp(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		Getenv: func(k string) string {
			if k == "MULTICA_GROK_PATH" {
				return "/home/t/.grok/bin/grok"
			}
			return ""
		},
		Stat: func(path string) (os.FileInfo, error) {
			if path == "/home/t/.grok/bin/grok" {
				return fakeFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
		ProbeOne: func(ctx context.Context, cli string) (Probe, error) {
			return Probe{CLI: cli, Current: "1.2.3", Path: "/home/t/.grok/bin/grok", Channel: ChannelBinary}, nil
		},
	}
	res, err := u.Ensure(context.Background(), CLIGrok)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionAlreadyPresent {
		t.Fatalf("action = %q", res.Action)
	}
	if res.Probe.Current != "1.2.3" {
		t.Fatalf("probe = %#v", res.Probe)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("ensure present must not install: %#v", runner.calls)
	}
}

func TestEnsureGrokMissingUnsupported(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		Prefix:  "/home/t/.local",
		LookPath: func(string) (string, error) {
			return "", errors.New("missing")
		},
		Stat: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	_, err := u.Ensure(context.Background(), CLIGrok)
	if !errors.Is(err, ErrNoInstallSource) {
		t.Fatalf("err = %v, want ErrNoInstallSource", err)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestEnsureCodexMissingRunsNPM(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{out: "installed"}
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		Prefix:  "/home/t/.local",
		LookPath: func(string) (string, error) {
			return "", errors.New("missing")
		},
		Stat: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
	}
	res, err := u.Ensure(context.Background(), CLICodex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionUpgraded {
		t.Fatalf("action = %q", res.Action)
	}
	if len(runner.calls) != 1 || runner.calls[0].Args[4] != "@openai/codex@latest" {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestEnsureCodexPresentSkipsNPM(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{}
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		LookPath: func(name string) (string, error) {
			if name == "codex" {
				return "/usr/local/bin/codex", nil
			}
			return "", errors.New("missing")
		},
		Stat: func(path string) (os.FileInfo, error) {
			if path == "/usr/local/bin/codex" {
				return fakeFileInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
	}
	res, err := u.Ensure(context.Background(), CLICodex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Action != ActionAlreadyPresent {
		t.Fatalf("action = %q", res.Action)
	}
	if len(runner.calls) != 0 {
		t.Fatalf("calls = %#v", runner.calls)
	}
}

func TestUpgradeReprobesAfterSuccess(t *testing.T) {
	t.Parallel()
	runner := &fakeRunner{out: "ok"}
	var probed string
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		Prefix:  "/home/t/.local",
		LookPath: func(string) (string, error) {
			return "", errors.New("missing")
		},
		Stat: func(string) (os.FileInfo, error) {
			return nil, os.ErrNotExist
		},
		ProbeOne: func(ctx context.Context, cli string) (Probe, error) {
			probed = cli
			return Probe{CLI: cli, Current: "9.9.9", Path: "/home/t/.local/bin/codex", Channel: ChannelNPM}, nil
		},
	}
	res, err := u.Upgrade(context.Background(), CLICodex)
	if err != nil {
		t.Fatal(err)
	}
	if probed != CLICodex {
		t.Fatalf("probed = %q", probed)
	}
	if res.Probe.Current != "9.9.9" || res.Probe.Channel != ChannelNPM {
		t.Fatalf("probe = %#v", res.Probe)
	}
}

func TestUpgradePrefersPrefixBinOverBrewLookPath(t *testing.T) {
	t.Parallel()
	const brew = "/opt/homebrew/bin/codex"
	const prefix = "/home/t/.local"
	prefixBin := filepath.Join(prefix, "bin", "codex")
	runner := &fakeRunner{out: "ok"}
	u := &Upgrader{
		Runner:  runner,
		HomeDir: "/home/t",
		Prefix:  prefix,
		LookPath: func(name string) (string, error) {
			if name == "codex" {
				return brew, nil
			}
			return "", errors.New("missing")
		},
		Stat: func(path string) (os.FileInfo, error) {
			if path == brew || path == prefixBin {
				return fakeFileInfo{}, nil
			}
			// HasNPMPrefix looks for <prefix>/lib/node_modules
			if path == filepath.Join(prefix, "lib", "node_modules") {
				return fakeDirInfo{}, nil
			}
			return nil, os.ErrNotExist
		},
	}
	res, err := u.Upgrade(context.Background(), CLICodex)
	if err != nil {
		t.Fatal(err)
	}
	if res.Probe.Path != prefixBin {
		t.Fatalf("path = %q, want prefix bin %q (must not stick to brew %q)", res.Probe.Path, prefixBin, brew)
	}
	if res.Probe.Channel != ChannelNPM {
		t.Fatalf("channel = %q, want npm", res.Probe.Channel)
	}
}

type fakeDirInfo struct{}

func (fakeDirInfo) Name() string       { return "node_modules" }
func (fakeDirInfo) Size() int64        { return 0 }
func (fakeDirInfo) Mode() os.FileMode  { return os.ModeDir | 0o755 }
func (fakeDirInfo) ModTime() time.Time { return time.Time{} }
func (fakeDirInfo) IsDir() bool        { return true }
func (fakeDirInfo) Sys() any           { return nil }

type fakeFileInfo struct{}

func (fakeFileInfo) Name() string       { return "x" }
func (fakeFileInfo) Size() int64        { return 1 }
func (fakeFileInfo) Mode() os.FileMode  { return 0o755 }
func (fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (fakeFileInfo) IsDir() bool        { return false }
func (fakeFileInfo) Sys() any           { return nil }
