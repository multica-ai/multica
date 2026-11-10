package agentcli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Install / upgrade errors. Callers match these with errors.Is.
var (
	// ErrNoInstallSource means the CLI is managed but npmPackages has no
	// package for it, so install and upgrade are not available.
	ErrNoInstallSource = errors.New("agent cli has no registry package")
	// ErrUnknownCLI means the name is outside the managed set.
	ErrUnknownCLI = errors.New("unknown managed agent cli")
)

// Upgrade actions reported on Result.
const (
	ActionUpgraded       = "upgraded"
	ActionAlreadyPresent = "already_present"
)

// DefaultNPMGlobalPrefix is the user-writable install root for one-click
// upgrades: "$HOME/.local" (bins land in "$HOME/.local/bin").
func DefaultNPMGlobalPrefix(home string) string {
	home = strings.TrimSpace(home)
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local")
}

// CommandSpec is one argv the upgrader will run (or that tests assert).
type CommandSpec struct {
	Name string
	Args []string
}

// CommandRunner runs an external command. Production uses RealCommandRunner;
// tests inject a fake so unit tests never hit the npm registry.
type CommandRunner interface {
	Run(ctx context.Context, name string, args ...string) (output string, err error)
}

// RealCommandRunner shells out with exec.CommandContext.
type RealCommandRunner struct{}

// Run implements CommandRunner.
func (RealCommandRunner) Run(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// NPMInstallLatestSpec builds the exact upgrade argv for a managed npm CLI:
//
//	npm install -g --prefix <prefix> <package>@latest
//
// prefix defaults to DefaultNPMGlobalPrefix(home). home defaults to
// os.UserHomeDir. A managed name with no npmPackages row returns
// ErrNoInstallSource. Anything outside the managed set returns ErrUnknownCLI.
func NPMInstallLatestSpec(cli, home, prefix string) (CommandSpec, error) {
	cli = strings.ToLower(strings.TrimSpace(cli))
	if !IsManaged(cli) {
		return CommandSpec{}, fmt.Errorf("%w: %q", ErrUnknownCLI, cli)
	}
	pkg, ok := NPMPackage(cli)
	if !ok {
		return CommandSpec{}, fmt.Errorf("%w: %s", ErrNoInstallSource, cli)
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		if strings.TrimSpace(home) == "" {
			var err error
			home, err = os.UserHomeDir()
			if err != nil {
				return CommandSpec{}, fmt.Errorf("resolve home for npm prefix: %w", err)
			}
		}
		prefix = DefaultNPMGlobalPrefix(home)
	}
	if prefix == "" {
		return CommandSpec{}, errors.New("npm global prefix is empty")
	}
	return CommandSpec{
		Name: "npm",
		Args: []string{"install", "-g", "--prefix", prefix, pkg + "@latest"},
	}, nil
}

// Result is the outcome of Ensure or Upgrade.
type Result struct {
	CLI    string
	Action string
	Output string
	Probe  Probe
}

// Upgrader installs or upgrades managed agent CLIs. Zero value is usable:
// real runner, os.UserHomeDir, exec.LookPath, os.Getenv.
type Upgrader struct {
	Runner   CommandRunner
	HomeDir  string
	Prefix   string
	LookPath func(string) (string, error)
	Getenv   func(string) string
	Stat     func(string) (os.FileInfo, error)
	// ProbeOne, when set, re-observes the CLI after a successful install or
	// an already-present Ensure. Nil leaves Result.Probe empty.
	ProbeOne func(ctx context.Context, cli string) (Probe, error)
	Now      func() time.Time
}

func (u *Upgrader) runner() CommandRunner {
	if u != nil && u.Runner != nil {
		return u.Runner
	}
	return RealCommandRunner{}
}

func (u *Upgrader) lookPath(name string) (string, error) {
	if u != nil && u.LookPath != nil {
		return u.LookPath(name)
	}
	return exec.LookPath(name)
}

func (u *Upgrader) getenv(key string) string {
	if u != nil && u.Getenv != nil {
		return u.Getenv(key)
	}
	return os.Getenv(key)
}

func (u *Upgrader) stat(path string) (os.FileInfo, error) {
	if u != nil && u.Stat != nil {
		return u.Stat(path)
	}
	return os.Stat(path)
}

func (u *Upgrader) home() string {
	if u != nil && strings.TrimSpace(u.HomeDir) != "" {
		return u.HomeDir
	}
	home, _ := os.UserHomeDir()
	return home
}

func (u *Upgrader) prefix() string {
	if u != nil && strings.TrimSpace(u.Prefix) != "" {
		return u.Prefix
	}
	return DefaultNPMGlobalPrefix(u.home())
}

func (u *Upgrader) now() time.Time {
	if u != nil && u.Now != nil {
		return u.Now()
	}
	return time.Now()
}

// Upgrade always runs the install channel for cli (npm @latest when
// npmPackages has a row). A managed CLI with no package returns
// ErrNoInstallSource even when a binary is already on PATH.
func (u *Upgrader) Upgrade(ctx context.Context, cli string) (Result, error) {
	cli = strings.ToLower(strings.TrimSpace(cli))
	if !IsManaged(cli) {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownCLI, cli)
	}
	spec, err := NPMInstallLatestSpec(cli, u.home(), u.prefix())
	if err != nil {
		return Result{}, err
	}
	return u.runInstall(ctx, cli, spec)
}

// Ensure installs cli when it is missing and npmPackages has a row. If a
// binary is already resolvable, Ensure is a no-op (ActionAlreadyPresent)
// and does not shell out to npm. A missing CLI with no package returns
// ErrNoInstallSource.
func (u *Upgrader) Ensure(ctx context.Context, cli string) (Result, error) {
	cli = strings.ToLower(strings.TrimSpace(cli))
	if !IsManaged(cli) {
		return Result{}, fmt.Errorf("%w: %q", ErrUnknownCLI, cli)
	}
	if path, ok := u.locate(cli); ok {
		res := Result{CLI: cli, Action: ActionAlreadyPresent}
		res.Probe = u.probeOrPath(ctx, cli, path)
		return res, nil
	}
	spec, err := NPMInstallLatestSpec(cli, u.home(), u.prefix())
	if err != nil {
		return Result{}, err
	}
	return u.runInstall(ctx, cli, spec)
}

func (u *Upgrader) runInstall(ctx context.Context, cli string, spec CommandSpec) (Result, error) {
	out, err := u.runner().Run(ctx, spec.Name, spec.Args...)
	if err != nil {
		return Result{CLI: cli, Output: out}, fmt.Errorf("install %s: %w\n%s", cli, err, strings.TrimSpace(out))
	}
	res := Result{CLI: cli, Action: ActionUpgraded, Output: out}
	// Prefer the just-installed npm --prefix bin over any older brew/custom
	// binary still visible via locate/LookPath. Otherwise Upgrade succeeds
	// but the re-probe reports the pre-upgrade path/version.
	path := ""
	candidate := filepath.Join(u.prefix(), "bin", cli)
	if u.fileOK(candidate) {
		path = candidate
	} else if p, ok := u.locate(cli); ok {
		path = p
	}
	res.Probe = u.probeOrPath(ctx, cli, path)
	return res, nil
}

func (u *Upgrader) probeOrPath(ctx context.Context, cli, path string) Probe {
	if u != nil && u.ProbeOne != nil {
		probe, err := u.ProbeOne(ctx, cli)
		if err == nil {
			return probe
		}
	}
	probedAt := u.now().UTC().Format(time.RFC3339)
	channel := ChannelUnknown
	if path != "" {
		if ch := ChannelFromPath(path); ch != "" {
			channel = ch
		} else if HasNPMPrefix(path, u.stat) {
			channel = ChannelNPM
		} else {
			channel = ChannelBinary
		}
	}
	return Probe{
		CLI:      cli,
		Path:     path,
		Channel:  channel,
		ProbedAt: probedAt,
	}
}

// locate finds an already-installed binary for cli without installing.
func (u *Upgrader) locate(cli string) (string, bool) {
	cli = strings.ToLower(strings.TrimSpace(cli))
	if envKey := managedCLIPathEnv[cli]; envKey != "" {
		if p := strings.TrimSpace(u.getenv(envKey)); p != "" && u.fileOK(p) {
			return p, true
		}
	}
	if p, err := u.lookPath(cli); err == nil && u.fileOK(p) {
		return p, true
	}
	// npm --prefix default layout, even when PATH has not been refreshed.
	candidate := filepath.Join(u.prefix(), "bin", cli)
	if u.fileOK(candidate) {
		return candidate, true
	}
	return "", false
}

func (u *Upgrader) fileOK(path string) bool {
	path = strings.TrimSpace(path)
	if path == "" {
		return false
	}
	info, err := u.stat(path)
	return err == nil && info != nil && !info.IsDir()
}
