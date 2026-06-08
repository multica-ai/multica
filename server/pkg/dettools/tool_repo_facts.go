package dettools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// repoFactsTool reports deterministic, read-only facts about the working
// repository.
func repoFactsTool() Tool {
	return Tool{
		Name:        "repo_facts",
		Description: "Report deterministic facts about the working repository: current branch, changed (dirty) files, and detected package managers/lockfiles. Read-only; runs git and stats files within the task working directory.",
		InputSchema: json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`),
		Handler:     repoFactsHandler,
	}
}

func repoFactsHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	if err := strictUnmarshal(args, &struct{}{}); err != nil {
		return Errf(CodeInvalidInput, "repo_facts takes no arguments: %v", err)
	}
	if !gitAvailable() {
		return Errf(CodeMissingDependency, "git not found on PATH")
	}
	if !isGitRepo(ctx, env.WorkDir) {
		return OK("Working directory is not a git repository", map[string]any{
			"is_git_repo": false,
			"work_dir":    env.WorkDir,
		})
	}

	branch, err := currentBranch(ctx, env.WorkDir)
	if err != nil {
		return Errf(CodeInternal, "resolve current branch: %v", err)
	}
	changed, err := changedFiles(ctx, env.WorkDir)
	if err != nil {
		return Errf(CodeInternal, "list changed files: %v", err)
	}
	lockfiles, managers := detectPackaging(env.WorkDir)

	data := map[string]any{
		"is_git_repo":      true,
		"branch":           branch,
		"dirty_files":      len(changed),
		"changed_files":    changed,
		"lockfiles":        lockfiles,
		"package_managers": managers,
	}
	return OK(fmt.Sprintf("branch %q, %d changed file(s)", branch, len(changed)), data)
}

// lockfileManagers maps a lockfile/manifest basename to the package manager it
// implies.
var lockfileManagers = []struct {
	file    string
	manager string
}{
	{"pnpm-lock.yaml", "pnpm"},
	{"package-lock.json", "npm"},
	{"yarn.lock", "yarn"},
	{"bun.lockb", "bun"},
	{"go.sum", "go"},
	{"Cargo.lock", "cargo"},
	{"poetry.lock", "poetry"},
	{"uv.lock", "uv"},
	{"requirements.txt", "pip"},
	{"Gemfile.lock", "bundler"},
	{"composer.lock", "composer"},
	{"pom.xml", "maven"},
	{"build.gradle", "gradle"},
}

// detectPackaging returns the lockfiles present in the repo root and the
// distinct package managers they imply, both in stable (declaration) order.
func detectPackaging(workDir string) (lockfiles, managers []string) {
	seen := map[string]bool{}
	for _, lm := range lockfileManagers {
		if fileExists(filepath.Join(workDir, lm.file)) {
			lockfiles = append(lockfiles, lm.file)
			if !seen[lm.manager] {
				seen[lm.manager] = true
				managers = append(managers, lm.manager)
			}
		}
	}
	return lockfiles, managers
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

// strictUnmarshal decodes data into v, rejecting unknown fields so a malformed
// or unexpected argument shape fails closed as INVALID_INPUT rather than being
// silently ignored.
func strictUnmarshal(data json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	return dec.Decode(v)
}
