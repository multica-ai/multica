package main

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
)

// issueUpdateForSideEffects accepts the normal HTTP-layer IssueResponse and
// the explicitly marked task-completion status fallback emitted by TaskService.
// Other service-owned map payloads remain realtime-only by design.
func issueUpdateForSideEffects(payload map[string]any) (handler.IssueResponse, bool) {
	if issue, ok := payload["issue"].(handler.IssueResponse); ok {
		return issue, true
	}
	fallback, _ := payload[service.TaskCompletionStatusFallbackField].(bool)
	if !fallback {
		return handler.IssueResponse{}, false
	}
	issueMap, ok := payload["issue"].(map[string]any)
	if !ok {
		return handler.IssueResponse{}, false
	}
	raw, err := json.Marshal(issueMap)
	if err != nil {
		return handler.IssueResponse{}, false
	}
	var issue handler.IssueResponse
	if err := json.Unmarshal(raw, &issue); err != nil || issue.ID == "" || issue.WorkspaceID == "" {
		return handler.IssueResponse{}, false
	}
	return issue, true
}
