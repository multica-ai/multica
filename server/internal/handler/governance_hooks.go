package handler

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/governance"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func governanceConfigFromHandler(h *Handler) governance.Config {
	timeout := h.cfg.GovernanceHookTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return governance.Config{
		Root:    strings.TrimSpace(h.cfg.GovernanceRoot),
		Timeout: timeout,
	}
}

func (h *Handler) workspaceGovernanceHooks(ctx context.Context, workspaceID pgtype.UUID) (governance.WorkspaceHooks, error) {
	ws, err := h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		return governance.WorkspaceHooks{}, err
	}
	return governance.ParseWorkspaceHooks(ws.Settings, governanceConfigFromHandler(h)), nil
}

func writeGovernanceHookError(w http.ResponseWriter, err error) bool {
	var denied *governance.HookDeniedError
	if errors.As(err, &denied) {
		writeErrorCode(w, http.StatusForbidden, "governance_hook_denied", denied.Error())
		return true
	}
	var failed *governance.HookFailedError
	if errors.As(err, &failed) {
		writeErrorCode(w, http.StatusForbidden, "governance_hook_failed", failed.Error())
		return true
	}
	return false
}

func (h *Handler) invokePreCommentHook(ctx context.Context, workspaceID pgtype.UUID, issue db.Issue, authorType, authorID, content string) error {
	hooks, err := h.workspaceGovernanceHooks(ctx, workspaceID)
	if err != nil {
		return &governance.HookFailedError{Hook: "pre_comment", Err: err}
	}
	if hooks.PreComment == "" {
		return nil
	}

	dir, err := os.MkdirTemp("", "multica-pre-comment-*")
	if err != nil {
		return &governance.HookFailedError{Hook: "pre_comment", Err: err}
	}
	defer os.RemoveAll(dir)

	commentFile := filepath.Join(dir, "comment.md")
	if err := os.WriteFile(commentFile, []byte(content), 0o600); err != nil {
		return &governance.HookFailedError{Hook: "pre_comment", Err: err}
	}

	env := map[string]string{
		"MULTICA_COMMENT_FILE": commentFile,
		"MULTICA_AUTHOR_ID":    authorID,
		"ISSUE_ID":             uuidToString(issue.ID),
		"ISSUE_TITLE":          issue.Title,
		"ISSUE_STATUS":         issue.Status,
		"AUTHOR_TYPE":          authorType,
	}
	if issue.ParentIssueID.Valid {
		env["PARENT_ISSUE_ID"] = uuidToString(issue.ParentIssueID)
	}
	return governance.RunPreComment(ctx, hooks, env, commentFile)
}

func (h *Handler) invokePreStatusHook(ctx context.Context, workspaceID pgtype.UUID, issue db.Issue, newStatus, authorType, authorID string) error {
	hooks, err := h.workspaceGovernanceHooks(ctx, workspaceID)
	if err != nil {
		return &governance.HookFailedError{Hook: "pre_status", Err: err}
	}
	if hooks.PreStatus == "" {
		return nil
	}

	env := map[string]string{
		"ISSUE_ID":          uuidToString(issue.ID),
		"STATUS":            newStatus,
		"MULTICA_AUTHOR_ID": authorID,
		"ISSUE_TITLE":       issue.Title,
		"AUTHOR_TYPE":       authorType,
	}
	if issue.ParentIssueID.Valid {
		env["PARENT_ISSUE_ID"] = uuidToString(issue.ParentIssueID)
	}

	args := []string{
		"--issue-id", uuidToString(issue.ID),
		"--status", newStatus,
		"--author-id", authorID,
		"--issue-title", issue.Title,
	}
	if issue.ParentIssueID.Valid {
		args = append(args, "--parent-id", uuidToString(issue.ParentIssueID))
	}
	return governance.RunPreStatus(ctx, hooks, env, args)
}
