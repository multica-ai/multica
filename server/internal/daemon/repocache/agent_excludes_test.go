package repocache

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigureAgentGitExcludesPreservesGlobalAndExistingParameters(t *testing.T) {
	repo := createTestRepo(t)
	globalExclude := filepath.Join(t.TempDir(), "global-ignore")
	if err := os.WriteFile(globalExclude, []byte("/user-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	globalConfig := filepath.Join(t.TempDir(), "gitconfig")
	if err := os.WriteFile(globalConfig, []byte("[core]\n\texcludesFile = "+globalExclude+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("GIT_CONFIG_GLOBAL", globalConfig)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_PARAMETERS", "")

	scratch := filepath.Join(t.TempDir(), "task's scratch")
	if err := os.MkdirAll(scratch, 0o700); err != nil {
		t.Fatal(err)
	}
	agentEnv := map[string]string{"GIT_CONFIG_PARAMETERS": "'color.ui=always'"}
	if err := ConfigureAgentGitExcludes(scratch, agentEnv); err != nil {
		t.Fatalf("ConfigureAgentGitExcludes: %v", err)
	}
	for _, path := range []string{"user-secret", "CLAUDE.md", ".claude/settings.local.json"} {
		if !gitCheckIgnored(t, repo, agentEnv, path) {
			t.Errorf("agent Git does not ignore %s", path)
		}
	}
	if gitCheckIgnored(t, repo, agentEnv, "docs/CLAUDE.md") {
		t.Fatal("root-anchored runtime exclusion hides a nested user file")
	}
	if !gitCheckIgnored(t, repo, nil, "user-secret") {
		t.Fatal("test setup: user's global exclude is not effective")
	}
	if gitCheckIgnored(t, repo, nil, "CLAUDE.md") {
		t.Fatal("task-specific exclude leaked to the user's Git")
	}
	cmd := exec.Command("git", "-C", repo, "config", "--get", "color.ui")
	cmd.Env = append(os.Environ(), envEntries(agentEnv)...)
	output, err := cmd.Output()
	if err != nil || strings.TrimSpace(string(output)) != "always" {
		t.Fatalf("prior Git config parameter lost: %q, %v", output, err)
	}
}

func TestConfigureAgentGitExcludesFailsBeforeChangingEnvironment(t *testing.T) {
	agentEnv := map[string]string{}
	err := ConfigureAgentGitExcludes(filepath.Join(t.TempDir(), "missing"), agentEnv)
	if err == nil {
		t.Fatal("expected error for missing task scratch directory")
	}
	if _, exists := agentEnv["GIT_CONFIG_PARAMETERS"]; exists {
		t.Fatal("agent environment was changed despite exclude setup failure")
	}
}
