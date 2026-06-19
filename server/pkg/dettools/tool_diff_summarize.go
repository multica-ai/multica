package dettools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// diffSummarizeInput optionally pins a base ref. When empty, the tool
// summarizes uncommitted working-tree changes against HEAD.
type diffSummarizeInput struct {
	Base string `json:"base"`
}

func diffSummarizeTool() Tool {
	return Tool{
		Name:        "diff_summarize",
		Description: "Produce a stable, machine-readable diff summary (path, change type, additions, deletions). USE instead of raw 'git diff' — raw diffs are verbose, hard to parse correctly, and differ semantically from a structured change list. With no base, summarizes uncommitted changes vs HEAD; with a base ref, summarizes base...HEAD. Read-only.",
		InputSchema: json.RawMessage(`{
  "type": "object",
  "properties": {
    "base": {"type": "string", "description": "Optional base ref (e.g. origin/main). When set, summarizes base...HEAD."}
  },
  "additionalProperties": false
}`),
		Handler: diffSummarizeHandler,
	}
}

func diffSummarizeHandler(ctx context.Context, args json.RawMessage, env ToolEnv) Result {
	var in diffSummarizeInput
	if err := strictUnmarshal(args, &in); err != nil {
		return Errf(CodeInvalidInput, "invalid diff_summarize input: %v", err)
	}
	if !gitAvailable() {
		return Errf(CodeMissingDependency, "git not found on PATH")
	}
	if !isGitRepo(ctx, env.WorkDir) {
		return Errf(CodeInvalidInput, "working directory is not a git repository: %s", env.WorkDir)
	}

	// --no-renames keeps paths plain (renames become delete+add) so numstat and
	// name-status rows align by path without rename-arrow parsing.
	var rangeArg []string
	scope := "working-tree"
	if strings.TrimSpace(in.Base) != "" {
		rangeArg = []string{in.Base + "...HEAD"}
		scope = in.Base + "...HEAD"
	} else {
		rangeArg = []string{"HEAD"}
	}

	statusByPath := map[string]string{}
	nameStatus, err := gitOutputRaw(ctx, env.WorkDir, append([]string{"diff", "--name-status", "--no-renames"}, rangeArg...)...)
	if err != nil {
		return Errf(CodeInvalidInput, "git diff --name-status failed (bad base ref?): %v", err)
	}
	for _, line := range strings.Split(nameStatus, "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 2)
		if len(fields) == 2 && fields[1] != "" {
			statusByPath[fields[1]] = changeStatusLabel(fields[0])
		}
	}

	numstat, err := gitOutputRaw(ctx, env.WorkDir, append([]string{"diff", "--numstat", "--no-renames"}, rangeArg...)...)
	if err != nil {
		return Errf(CodeInvalidInput, "git diff --numstat failed: %v", err)
	}

	var files []map[string]any
	totalAdd, totalDel := 0, 0
	for _, line := range strings.Split(numstat, "\n") {
		fields := strings.SplitN(strings.TrimRight(line, "\r"), "\t", 3)
		if len(fields) != 3 || fields[2] == "" {
			continue
		}
		binary := fields[0] == "-" || fields[1] == "-"
		add, _ := strconv.Atoi(fields[0])
		del, _ := strconv.Atoi(fields[1])
		totalAdd += add
		totalDel += del
		files = append(files, map[string]any{
			"path":      fields[2],
			"status":    statusByPath[fields[2]],
			"additions": add,
			"deletions": del,
			"binary":    binary,
		})
	}

	data := map[string]any{
		"scope": scope,
		"files": files,
		"totals": map[string]any{
			"files":     len(files),
			"additions": totalAdd,
			"deletions": totalDel,
		},
	}
	return OK(fmt.Sprintf("%d file(s) changed, +%d/-%d", len(files), totalAdd, totalDel), data)
}

// changeStatusLabel maps a git name-status letter to a stable word.
func changeStatusLabel(code string) string {
	if code == "" {
		return ""
	}
	switch code[0] {
	case 'A':
		return "added"
	case 'M':
		return "modified"
	case 'D':
		return "deleted"
	case 'C':
		return "copied"
	case 'T':
		return "typechange"
	default:
		return strings.ToLower(code)
	}
}
