package daemon

import (
	"encoding/json"
	"strings"
)

const issueTaskContextType = "issue_task"

type issueTaskContext struct {
	Type          string `json:"type"`
	CodexRepoPath string `json:"codex_repo_path,omitempty"`
}

func parseIssueTaskContext(raw json.RawMessage) issueTaskContext {
	if len(raw) == 0 {
		return issueTaskContext{}
	}
	var out issueTaskContext
	if err := json.Unmarshal(raw, &out); err != nil {
		return issueTaskContext{}
	}
	if out.Type != issueTaskContextType {
		return issueTaskContext{}
	}
	out.CodexRepoPath = strings.TrimSpace(out.CodexRepoPath)
	return out
}

func repoNativeCodexPath(task Task, provider string) string {
	if provider != "codex" {
		return ""
	}
	return parseIssueTaskContext(task.Context).CodexRepoPath
}
