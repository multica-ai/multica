package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

// policyCheckInput is the (all-optional) policy specification. Each populated
// field adds a check; an empty input passes trivially.
type policyCheckInput struct {
	// AllowedBranchPrefixes: the current branch must start with one of these.
	AllowedBranchPrefixes []string `json:"allowed_branch_prefixes"`
	// ForbiddenPaths: no changed file may start with one of these path prefixes.
	ForbiddenPaths []string `json:"forbidden_paths"`
	// RequiredFiles: each must exist in the working directory.
	RequiredFiles []string `json:"required_files"`
}

func policyCheckTool() Tool {
	return Tool{
		Name:        "policy_check",
		Description: "Check the working repository against a deterministic policy: allowed branch prefixes, forbidden changed paths, and required files. Returns POLICY_FAILURE with a list of violations when any rule is broken. Read-only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "allowed_branch_prefixes": {"type": "array", "items": {"type": "string"}, "description": "Current branch must start with one of these prefixes."},
    "forbidden_paths": {"type": "array", "items": {"type": "string"}, "description": "No changed file may start with one of these path prefixes."},
    "required_files": {"type": "array", "items": {"type": "string"}, "description": "Each path must exist in the working directory."}
  },
  "additionalProperties": false
}`),
		Handler: policyCheckHandler,
	}
}

func policyCheckHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	var in policyCheckInput
	if err := strictUnmarshal(args, &in); err != nil {
		return Errf(CodeInvalidInput, "invalid policy_check input: %v", err)
	}
	if !gitAvailable() {
		return Errf(CodeMissingDependency, "git not found on PATH")
	}
	if !isGitRepo(ctx, env.WorkDir) {
		return Errf(CodeInvalidInput, "working directory is not a git repository: %s", env.WorkDir)
	}

	branch, err := currentBranch(ctx, env.WorkDir)
	if err != nil {
		return Errf(CodeInternal, "resolve current branch: %v", err)
	}
	changed, err := changedFiles(ctx, env.WorkDir)
	if err != nil {
		return Errf(CodeInternal, "list changed files: %v", err)
	}

	var violations []string

	if len(in.AllowedBranchPrefixes) > 0 && !hasAnyPrefix(branch, in.AllowedBranchPrefixes) {
		violations = append(violations, fmt.Sprintf("branch %q does not match any allowed prefix %v", branch, in.AllowedBranchPrefixes))
	}

	for _, file := range changed {
		for _, forbidden := range in.ForbiddenPaths {
			if forbidden != "" && strings.HasPrefix(file, forbidden) {
				violations = append(violations, fmt.Sprintf("changed file %q is under forbidden path %q", file, forbidden))
			}
		}
	}

	for _, required := range in.RequiredFiles {
		if required == "" {
			continue
		}
		if !fileExists(filepath.Join(env.WorkDir, required)) {
			violations = append(violations, fmt.Sprintf("required file %q is missing", required))
		}
	}

	data := map[string]any{
		"branch":      branch,
		"dirty_files": len(changed),
		"violations":  violations,
		"checks_run":  len(in.AllowedBranchPrefixes) > 0 || len(in.ForbiddenPaths) > 0 || len(in.RequiredFiles) > 0,
		"passed":      len(violations) == 0,
	}

	if len(violations) > 0 {
		return Result{
			Status:      StatusError,
			ErrorCode:   CodePolicyFailure,
			Summary:     fmt.Sprintf("%d policy violation(s)", len(violations)),
			MachineData: data,
			Retryable:   false,
		}
	}
	return OK("Repository policy check passed", data)
}

func hasAnyPrefix(s string, prefixes []string) bool {
	for _, p := range prefixes {
		if p != "" && strings.HasPrefix(s, p) {
			return true
		}
	}
	return false
}
