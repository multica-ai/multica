package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestFilterStepsByProfile(t *testing.T) {
	steps := []DeterministicToolData{
		{Name: "alpha"}, {Name: "beta"}, {Name: "gamma"},
	}

	// No profile, no daemon denylist → all pass.
	if got := filterStepsByProfile(steps, agentDetToolsProfile{}, nil); len(got) != 3 {
		t.Fatalf("no policy: got %d steps, want 3", len(got))
	}

	// allowed_tools narrows to the named subset.
	got := filterStepsByProfile(steps, agentDetToolsProfile{AllowedTools: []string{"alpha", "gamma"}}, nil)
	if len(got) != 2 || got[0].Name != "alpha" || got[1].Name != "gamma" {
		t.Fatalf("allowed narrowing: got %+v, want alpha+gamma", got)
	}

	// denied_tools (agent) and the daemon denylist both drop.
	got = filterStepsByProfile(steps, agentDetToolsProfile{DeniedTools: []string{"beta"}}, []string{"gamma"})
	if len(got) != 1 || got[0].Name != "alpha" {
		t.Fatalf("deny: got %+v, want only alpha", got)
	}
}

func TestWriteStepsFile_WorkDir(t *testing.T) {
	workDir := t.TempDir()
	steps := []DeterministicToolData{{Name: "a", Description: "d", Source: "package step"}}

	path, err := writeStepsFile(workDir, steps)
	if err != nil {
		t.Fatalf("writeStepsFile: %v", err)
	}
	if want := filepath.Join(workDir, ".multica", "dettools", "steps.json"); path != want {
		t.Fatalf("path = %q, want %q", path, want)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var decoded []detToolsStepFile
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal steps file: %v", err)
	}
	if len(decoded) != 1 || decoded[0].Name != "a" || decoded[0].Source != "package step" {
		t.Fatalf("decoded = %+v, want one step a", decoded)
	}
}

func TestWriteStepsFile_TempFallbackWhenNoWorkDir(t *testing.T) {
	path, err := writeStepsFile("", []DeterministicToolData{{Name: "a", Source: "package step"}})
	if err != nil {
		t.Fatalf("writeStepsFile: %v", err)
	}
	defer os.Remove(path)
	if path == "" {
		t.Fatal("expected a temp file path, got empty")
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("temp steps file not written: %v", err)
	}
}

// With the tool plane enabled and authored steps present, injection must write
// the steps file and point the MCP server at it via MULTICA_DETTOOLS_STEPS_FILE.
func TestInjectExecOptionsTools_WritesStepsEnv(t *testing.T) {
	d := &Daemon{cfg: Config{DetTools: testDetToolsCfg()}}
	d.cfg.DetTools.Enabled = true
	workDir := t.TempDir()
	steps := []DeterministicToolData{{Name: "greet", Source: "package step\nfunc Run(i map[string]any) map[string]any { return nil }"}}

	out := d.injectExecOptionsTools(json.RawMessage(`{}`), "claude", workDir, nil, steps, discardLogger())
	entry, ok := parseServers(t, out)[dettoolsServerName]
	if !ok {
		t.Fatal("multica-tools server was not injected")
	}
	var server struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(entry, &server); err != nil {
		t.Fatal(err)
	}
	stepsFile := server.Env["MULTICA_DETTOOLS_STEPS_FILE"]
	if stepsFile == "" {
		t.Fatal("MULTICA_DETTOOLS_STEPS_FILE not set in server env")
	}
	if _, err := os.Stat(stepsFile); err != nil {
		t.Fatalf("steps file not written at %q: %v", stepsFile, err)
	}
}
