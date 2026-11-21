package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/issuestatus"
	"github.com/multica-ai/multica/server/internal/projectaccess"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChatWakeupCondition is a fact the platform can look up on its own. A chat
// wakeup is not anchored to an issue, so every condition names what it
// watches. Fields marked "display" are filled in by the server.
type ChatWakeupCondition struct {
	// issue | children_done | pull_request | run | issue_reply
	Type string `json:"type"`
	// issue, children_done, issue_reply
	IssueID    string `json:"issue_id,omitempty"`
	Identifier string `json:"identifier,omitempty"` // display
	// issue: done | ended | in_review | blocked, or Status for one status key.
	State  string `json:"state,omitempty"`
	Status string `json:"status,omitempty"`
	// children_done: this stage and earlier ones; every sub-issue when omitted.
	Stage *int32 `json:"stage,omitempty"`
	// pull_request: owner/name, number and merged | closed | checks_finished.
	Repo   string `json:"repo,omitempty"`
	Number int32  `json:"number,omitempty"`
	Event  string `json:"event,omitempty"`
	// run: one agent run reaching a terminal state.
	TaskID string `json:"task_id,omitempty"`
	// issue_reply: a comment by AuthorID (any member when empty) after Since.
	AuthorID   string `json:"author_id,omitempty"`
	AuthorName string `json:"author_name,omitempty"` // display
	Since      string `json:"since,omitempty"`       // display
}

func badChatCondition(msg string) error {
	return fmt.Errorf("%w: %s", ErrChatWakeupInput, msg)
}

// errChatConditionPending explains a wait the platform cannot resolve yet; the
// rule keeps polling and shows the reason.
type errChatConditionPending struct{ msg string }

func (e errChatConditionPending) Error() string { return e.msg }

// errChatConditionHidden means the condition reads an issue or run in a
// private project the wakeup's requester or the agent's owner cannot see.
var errChatConditionHidden = errors.New("chat wakeup condition is in a private project")

// chatConditionProjects lists the projects whose content a condition reads.
// children_done reports only counts of sub-issues, so the parent's project is
// the one that matters there.
func chatConditionProjects(ctx context.Context, q *db.Queries, workspaceID pgtype.UUID, c ChatWakeupCondition) ([]pgtype.UUID, error) {
	switch c.Type {
	case "issue", "children_done", "issue_reply":
		id, err := util.ParseUUID(c.IssueID)
		if err != nil {
			return nil, err
		}
		issue, err := q.GetIssue(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && issue.WorkspaceID != workspaceID) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []pgtype.UUID{issue.ProjectID}, nil
	case "run":
		id, err := util.ParseUUID(c.TaskID)
		if err != nil {
			return nil, err
		}
		task, err := q.GetAgentTask(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return projectaccess.TaskProjects(ctx, q, task)
	}
	return nil, nil
}

// checkChatConditionAccess applies the private-project rule every run obeys:
// the requester and the agent's owner must both see what the condition reads.
func checkChatConditionAccess(ctx context.Context, q *db.Queries, workspaceID, requesterID, agentOwnerID pgtype.UUID, raw []byte) error {
	var c ChatWakeupCondition
	if err := json.Unmarshal(raw, &c); err != nil {
		return err
	}
	projects, err := chatConditionProjects(ctx, q, workspaceID, c)
	if err != nil {
		return err
	}
	if !slices.ContainsFunc(projects, func(p pgtype.UUID) bool { return p.Valid }) {
		return nil
	}
	hidden, err := projectaccess.ForPrincipals(ctx, q, workspaceID, requesterID, agentOwnerID)
	if err != nil {
		return fmt.Errorf("resolve project access: %w", err)
	}
	for _, p := range projects {
		if p.Valid && !hidden.CanSeeProject(p) {
			return errChatConditionHidden
		}
	}
	return nil
}

// validateChatCondition normalizes a condition against the session's
// workspace. It rejects anything the platform could never evaluate, so a typo
// fails at creation instead of waiting out the whole deadline, and anything in
// a private project the requester or the agent's owner cannot see.
func (s *ChatWakeupService) validateChatCondition(ctx context.Context, tx pgx.Tx, q *db.Queries, workspaceID, requesterID, agentOwnerID pgtype.UUID, raw json.RawMessage, now time.Time) (json.RawMessage, error) {
	normalized, err := s.normalizeChatCondition(ctx, tx, workspaceID, raw, now)
	if err != nil {
		return nil, err
	}
	if err := checkChatConditionAccess(ctx, q, workspaceID, requesterID, agentOwnerID, normalized); err != nil {
		if errors.Is(err, errChatConditionHidden) {
			return nil, badChatCondition("this is in a private project that the person who asked, or this agent's owner, cannot see")
		}
		return nil, err
	}
	return normalized, nil
}

func (s *ChatWakeupService) normalizeChatCondition(ctx context.Context, tx pgx.Tx, workspaceID pgtype.UUID, raw json.RawMessage, now time.Time) (json.RawMessage, error) {
	var c ChatWakeupCondition
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return nil, badChatCondition("unreadable condition")
	}
	issueRef := func() (pgtype.UUID, string, error) {
		id, err := util.ParseUUID(c.IssueID)
		if err != nil {
			return id, "", badChatCondition("issue_id must be a UUID")
		}
		var identifier string
		err = tx.QueryRow(ctx, "SELECT ws.issue_prefix||'-'||i.number FROM issue i JOIN workspace ws ON ws.id=i.workspace_id WHERE i.workspace_id=$1 AND i.id=$2", workspaceID, id).Scan(&identifier)
		if errors.Is(err, pgx.ErrNoRows) {
			return id, "", badChatCondition("unknown issue")
		}
		return id, identifier, err
	}
	var out ChatWakeupCondition
	switch c.Type {
	case "issue":
		id, identifier, err := issueRef()
		if err != nil {
			return nil, err
		}
		out = ChatWakeupCondition{Type: c.Type, IssueID: util.UUIDToString(id), Identifier: identifier}
		switch {
		case c.Status != "" && c.State != "":
			return nil, badChatCondition("use state or status, not both")
		case c.Status != "":
			if !issuestatus.IsBuiltIn(c.Status) {
				var ok bool
				if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM issue_status WHERE workspace_id=$1 AND key=$2)", workspaceID, c.Status).Scan(&ok); err != nil {
					return nil, err
				}
				if !ok {
					return nil, badChatCondition("unknown status")
				}
			}
			out.Status = c.Status
		case slices.Contains([]string{"done", "ended", "in_review", "blocked"}, c.State):
			out.State = c.State
		default:
			return nil, badChatCondition("state must be done, ended, in_review or blocked, or pass a status key")
		}
	case "children_done":
		id, identifier, err := issueRef()
		if err != nil {
			return nil, err
		}
		if c.Stage != nil && (*c.Stage < 1 || *c.Stage > 1000) {
			return nil, badChatCondition("stage must be a positive number")
		}
		out = ChatWakeupCondition{Type: c.Type, IssueID: util.UUIDToString(id), Identifier: identifier, Stage: c.Stage}
	case "pull_request":
		owner, name, ok := strings.Cut(strings.TrimSpace(c.Repo), "/")
		if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
			return nil, badChatCondition("repo must be owner/name")
		}
		if c.Number <= 0 {
			return nil, badChatCondition("number must be a pull request number")
		}
		switch c.Event {
		case "merged", "closed":
		case "checks_finished":
			if s.Tasks == nil || !s.Tasks.PRSnapshotsEnabled {
				return nil, badChatCondition("PR checks cannot be watched on this server: GitHub check snapshots are not configured")
			}
		default:
			return nil, badChatCondition("event must be merged, closed or checks_finished")
		}
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM github_pull_request WHERE workspace_id=$1 AND lower(repo_owner)=lower($2) AND lower(repo_name)=lower($3) AND pr_number=$4)`,
			workspaceID, owner, name, c.Number).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, badChatCondition(fmt.Sprintf("this workspace has no pull request %s/%s#%d; it must be on a repository connected through the GitHub integration", owner, name, c.Number))
		}
		out = ChatWakeupCondition{Type: c.Type, Repo: owner + "/" + name, Number: c.Number, Event: c.Event}
	case "run":
		id, err := util.ParseUUID(c.TaskID)
		if err != nil {
			return nil, badChatCondition("task_id must be a UUID")
		}
		var exists bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM agent_task_queue t JOIN agent a ON a.id=t.agent_id WHERE t.id=$1 AND a.workspace_id=$2)", id, workspaceID).Scan(&exists); err != nil {
			return nil, err
		}
		if !exists {
			return nil, badChatCondition("unknown run")
		}
		out = ChatWakeupCondition{Type: c.Type, TaskID: util.UUIDToString(id)}
	case "issue_reply":
		id, identifier, err := issueRef()
		if err != nil {
			return nil, err
		}
		out = ChatWakeupCondition{Type: c.Type, IssueID: util.UUIDToString(id), Identifier: identifier, Since: now.UTC().Format(time.RFC3339Nano)}
		if c.AuthorID != "" {
			author, err := util.ParseUUID(c.AuthorID)
			if err != nil {
				return nil, badChatCondition("author_id must be a member's user id")
			}
			var name string
			err = tx.QueryRow(ctx, `SELECT u.name FROM member m JOIN "user" u ON u.id=m.user_id WHERE m.workspace_id=$1 AND m.user_id=$2`, workspaceID, author).Scan(&name)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, badChatCondition("author_id is not a member of this workspace")
			}
			if err != nil {
				return nil, err
			}
			out.AuthorID, out.AuthorName = util.UUIDToString(author), name
		}
	default:
		return nil, badChatCondition("type must be issue, children_done, pull_request, run or issue_reply")
	}
	normalized, _ := json.Marshal(out)
	return normalized, nil
}

// chatConditionFiresOnChange reports whether facts already true at creation
// are ignored. Finished checks usually belong to the head the agent is about
// to replace, so a checks condition waits for a newer result.
func chatConditionFiresOnChange(raw []byte) bool {
	var c ChatWakeupCondition
	_ = json.Unmarshal(raw, &c)
	return c.Type == "pull_request" && c.Event == "checks_finished"
}

// evaluateChatCondition reports whether the condition holds, a fingerprint of
// the satisfying facts and what was observed, for the woken agent.
func (s *ChatWakeupService) evaluateChatCondition(ctx context.Context, tx pgx.Tx, q *db.Queries, w db.ChatWakeup) (bool, string, map[string]any, error) {
	var c ChatWakeupCondition
	if err := json.Unmarshal(w.Condition, &c); err != nil {
		return false, "", nil, err
	}
	switch c.Type {
	case "issue":
		var status string
		err := tx.QueryRow(ctx, "SELECT status FROM issue WHERE id=$1 AND workspace_id=$2", c.IssueID, w.WorkspaceID).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, "deleted", map[string]any{"issue": c.Identifier, "deleted": true}, nil
		}
		if err != nil {
			return false, "", nil, err
		}
		observed := map[string]any{"issue": c.Identifier, "status": status}
		if c.Status != "" {
			return status == c.Status, "status:" + status, observed, nil
		}
		category, err := issuestatus.CategoryWithError(ctx, q, w.WorkspaceID, status)
		if err != nil {
			return false, "", nil, err
		}
		var met bool
		switch c.State {
		case "done":
			met = category == "done"
		case "ended":
			met = category == "done" || category == "closed"
		case "in_review", "blocked":
			met = status == c.State
		}
		return met, "status:" + status, observed, nil
	case "children_done":
		parent, err := util.ParseUUID(c.IssueID)
		if err != nil {
			return false, "", nil, err
		}
		children, err := loadSubIssues(ctx, tx, q, parent, w.WorkspaceID)
		if err != nil {
			return false, "", nil, err
		}
		met, fingerprint, observed := childrenDone(WakeupCondition{Stage: c.Stage}, children)
		observed["issue"] = c.Identifier
		return met, fingerprint, observed, nil
	case "pull_request":
		owner, name, _ := strings.Cut(c.Repo, "/")
		var state, head, snapshotHead, checks, url, title string
		err := tx.QueryRow(ctx, `SELECT state, head_sha, snapshot_head_sha, COALESCE(checks_rollup_state,''), html_url, title
			FROM github_pull_request
			WHERE workspace_id=$1 AND lower(repo_owner)=lower($2) AND lower(repo_name)=lower($3) AND pr_number=$4
			ORDER BY updated_at DESC LIMIT 1`, w.WorkspaceID, owner, name, c.Number).Scan(&state, &head, &snapshotHead, &checks, &url, &title)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil, errChatConditionPending{"The pull request is no longer mirrored in this workspace; check the GitHub integration"}
		}
		if err != nil {
			return false, "", nil, err
		}
		observed := map[string]any{"pull_request": fmt.Sprintf("%s#%d", c.Repo, c.Number), "title": title, "url": url, "state": state}
		switch c.Event {
		case "merged":
			return state == "merged", "pr:" + state, observed, nil
		case "closed":
			return state == "merged" || state == "closed", "pr:" + state, observed, nil
		case "checks_finished":
			if snapshotHead == "" || snapshotHead != head {
				return false, "", observed, errChatConditionPending{"Waiting for a check snapshot of the pull request's current head"}
			}
			observed["head_sha"], observed["checks"] = head, strings.ToLower(checks)
			finished := slices.Contains([]string{"SUCCESS", "FAILURE", "ERROR"}, checks)
			return finished, "checks:" + head + ":" + checks, observed, nil
		}
	case "run":
		var status string
		var issueID pgtype.UUID
		err := tx.QueryRow(ctx, "SELECT status, issue_id FROM agent_task_queue WHERE id=$1", c.TaskID).Scan(&status, &issueID)
		if errors.Is(err, pgx.ErrNoRows) {
			return true, "deleted", map[string]any{"run": c.TaskID, "deleted": true}, nil
		}
		if err != nil {
			return false, "", nil, err
		}
		observed := map[string]any{"run": c.TaskID, "status": status}
		if issueID.Valid {
			observed["issue_id"] = util.UUIDToString(issueID)
		}
		return slices.Contains([]string{"completed", "failed", "cancelled"}, status), "run:" + status, observed, nil
	case "issue_reply":
		since, err := time.Parse(time.RFC3339Nano, c.Since)
		if err != nil {
			return false, "", nil, err
		}
		var commentID pgtype.UUID
		var authorName, content string
		err = tx.QueryRow(ctx, `SELECT c.id, COALESCE(u.name,''), left(c.content, 500)
			FROM comment c LEFT JOIN "user" u ON u.id=c.author_id
			WHERE c.issue_id=$1 AND c.workspace_id=$2 AND c.deleted_at IS NULL
			  AND c.author_type='member' AND c.created_at > $3
			  AND ($4::text = '' OR c.author_id::text = $4::text)
			ORDER BY c.created_at ASC LIMIT 1`, c.IssueID, w.WorkspaceID, since, c.AuthorID).Scan(&commentID, &authorName, &content)
		if errors.Is(err, pgx.ErrNoRows) {
			return false, "", nil, nil
		}
		if err != nil {
			return false, "", nil, err
		}
		id := util.UUIDToString(commentID)
		return true, "comment:" + id, map[string]any{"issue": c.Identifier, "comment_id": id, "author": authorName, "excerpt": content}, nil
	}
	return false, "", nil, fmt.Errorf("unknown chat wakeup condition %q", c.Type)
}

// describeChatCondition renders a condition for the woken agent.
func describeChatCondition(raw []byte) string {
	var c ChatWakeupCondition
	if json.Unmarshal(raw, &c) != nil {
		return ""
	}
	switch c.Type {
	case "issue":
		if c.Status != "" {
			return fmt.Sprintf("%s reaches status %s", c.Identifier, c.Status)
		}
		return fmt.Sprintf("%s is %s", c.Identifier, strings.ReplaceAll(c.State, "_", " "))
	case "children_done":
		if c.Stage != nil {
			return fmt.Sprintf("the sub-issues of %s up to stage %d are closed", c.Identifier, *c.Stage)
		}
		return fmt.Sprintf("every sub-issue of %s is closed", c.Identifier)
	case "pull_request":
		event := map[string]string{"merged": "merges", "closed": "is merged or closed", "checks_finished": "finishes its checks"}[c.Event]
		return fmt.Sprintf("pull request %s#%d %s", c.Repo, c.Number, event)
	case "run":
		return fmt.Sprintf("run %s ends", c.TaskID)
	case "issue_reply":
		if c.AuthorName != "" {
			return fmt.Sprintf("%s replies on %s", c.AuthorName, c.Identifier)
		}
		return fmt.Sprintf("a person replies on %s", c.Identifier)
	}
	return c.Type
}
