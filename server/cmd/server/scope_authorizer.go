package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// scopeAuthQuerier is the narrow subset of db.Queries used by the scope
// authorizer. Declared as an interface so the authorizer can be unit tested
// with an in-memory fake (no DB required).
type scopeAuthQuerier interface {
	GetAgentTask(ctx context.Context, id pgtype.UUID) (db.AgentTaskQueue, error)
	GetIssue(ctx context.Context, id pgtype.UUID) (db.Issue, error)
	GetChatSession(ctx context.Context, id pgtype.UUID) (db.ChatSession, error)
}

// dbScopeAuthorizer implements realtime.ScopeAuthorizer for the per-task and
// per-chat scopes (workspace/user scopes are validated by the hub itself
// against the connection identity). It returns true only when the requested
// resource exists, belongs to the caller's workspace, and — for chat
// resources — was created by the caller (mirroring the HTTP creator-only
// access model).
type dbScopeAuthorizer struct{ q scopeAuthQuerier }

func newScopeAuthorizer(q scopeAuthQuerier) *dbScopeAuthorizer { return &dbScopeAuthorizer{q: q} }

func (a *dbScopeAuthorizer) AuthorizeScope(ctx context.Context, userID, workspaceID, scopeType, scopeID string) (bool, error) {
	if workspaceID == "" || scopeID == "" {
		return false, nil
	}
	wsUUID, err := util.ParseUUID(workspaceID)
	if err != nil {
		return false, nil
	}
	idUUID, err := util.ParseUUID(scopeID)
	if err != nil {
		return false, nil
	}
	switch scopeType {
	case realtime.ScopeTask:
		task, err := a.q.GetAgentTask(ctx, idUUID)
		if err != nil {
			return false, nil
		}
		// Issue tasks: visible to any workspace member.
		if task.IssueID.Valid {
			issue, err := a.q.GetIssue(ctx, task.IssueID)
			if err != nil {
				return false, nil
			}
			return issue.WorkspaceID == wsUUID, nil
		}
		// Chat tasks: only the chat session's creator may subscribe, mirroring
		// the HTTP layer's creator-only access on chat resources.
		if task.ChatSessionID.Valid {
			sess, err := a.q.GetChatSession(ctx, task.ChatSessionID)
			if err != nil {
				return false, nil
			}
			if sess.WorkspaceID != wsUUID {
				return false, nil
			}
			uidUUID, err := util.ParseUUID(userID)
			if err != nil || sess.CreatorID != uidUUID {
				return false, nil
			}
			return true, nil
		}
		return false, nil
	case realtime.ScopeChat:
		sess, err := a.q.GetChatSession(ctx, idUUID)
		if err != nil {
			return false, nil
		}
		if sess.WorkspaceID != wsUUID {
			return false, nil
		}
		// Chat sessions are private to their creator (see handler/chat.go:
		// GetChatSession / SendChatMessage / MarkChatSessionRead all enforce
		// CreatorID == userID). The realtime layer must not weaken this:
		// otherwise any workspace member who learns a session_id could
		// subscribe to chat:message / chat:done / chat:session_read for a
		// peer's private chat.
		uidUUID, err := util.ParseUUID(userID)
		if err != nil || sess.CreatorID != uidUUID {
			return false, nil
		}
		return true, nil
	default:
		return false, nil
	}
}
