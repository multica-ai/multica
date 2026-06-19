package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
)

// buildToolchains maps a detected toolchain to the binary that builds it and the
// arguments that print its version. Detection is by manifest presence in the
// repo root; the version probe is read-only (no network, no build side effects).
var buildToolchains = []struct {
	name        string
	manifests   []string // plain filenames checked in the repo root
	globs       []string // glob patterns checked in the repo root
	command     string
	versionArgs []string
}{
	{name: "go", manifests: []string{"go.mod"}, command: "go", versionArgs: []string{"version"}},
	{name: "node", manifests: []string{"package.json"}, command: "node", versionArgs: []string{"--version"}},
	{name: "pnpm", manifests: []string{"pnpm-lock.yaml"}, command: "pnpm", versionArgs: []string{"--version"}},
	{name: "rust", manifests: []string{"Cargo.toml"}, command: "cargo", versionArgs: []string{"--version"}},
	{name: "dotnet", globs: []string{"*.sln", "*.csproj"}, command: "dotnet", versionArgs: []string{"--version"}},
	{name: "maven", manifests: []string{"pom.xml"}, command: "mvn", versionArgs: []string{"-v"}},
	{name: "gradle", manifests: []string{"build.gradle", "build.gradle.kts"}, command: "gradle", versionArgs: []string{"-v"}},
	{name: "python", manifests: []string{"pyproject.toml", "requirements.txt", "setup.py"}, command: "python3", versionArgs: []string{"--version"}},
}

func buildProbeTool() Tool {
	return Tool{
		Name:        "build_probe",
		Description: "Detect toolchains and run non-destructive build probes. Reports which build tools are installed (Go, Node, pnpm, Rust, .NET, Maven, Gradle, Python) with versions. USE instead of raw make/npm/cargo — exit codes and output formats vary across toolchains and the model can misinterpret them. Read-only: runs only '--version'-style probes, never a build.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Handler:     buildProbeHandler,
	}
}

func buildProbeHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	if err := strictUnmarshal(args, &struct{}{}); err != nil {
		return Errf(CodeInvalidInput, "build_probe takes no arguments: %v", err)
	}

	var detected []map[string]any
	missing := 0
	for _, tc := range buildToolchains {
		if !manifestPresent(env.WorkDir, tc.manifests, tc.globs) {
			continue
		}
		version, present := commandVersion(ctx, tc.command, tc.versionArgs...)
		if !present {
			missing++
		}
		detected = append(detected, map[string]any{
			"toolchain": tc.name,
			"command":   tc.command,
			"available": present,
			"version":   version,
		})
	}

	data := map[string]any{
		"toolchains":    detected,
		"detected":      len(detected),
		"missing_tools": missing,
		"work_dir":      env.WorkDir,
	}
	summary := fmt.Sprintf("%d toolchain(s) detected, %d with a missing build tool", len(detected), missing)
	return OK(summary, data)
}

// manifestPresent reports whether any of the named manifest files, or any file
// matching one of the globs, exists in the repo root.
func manifestPresent(workDir string, manifests, globs []string) bool {
	for _, m := range manifests {
		if fileExists(filepath.Join(workDir, m)) {
			return true
		}
	}
	for _, g := range globs {
		if matches, _ := filepath.Glob(filepath.Join(workDir, g)); len(matches) > 0 {
			return true
		}
	}
	return false
}
