package engine

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	IssueQueuedAckText   = "👀 On it — I'm turning that into an issue. I'll post the result here when it's ready."
	IssueUsageText       = "Tell me what to file, e.g. `/issue the login button does nothing on Safari`."
	IssueQueueFailedText = "⚠️ Something went wrong creating the issue. Please try again."
)

// quickCreatePrompt removes the command token from this turn's own content.
// Unlike the synchronous direct-create path, a bare /issue never falls back to
// an earlier message: the user gets a usage hint instead of filing stale text.
func quickCreatePrompt(msg channel.InboundMessage) string {
	commandText := msg.CommandText
	if commandText == "" {
		commandText = msg.Text
	}
	rest, _ := stripIssuePrefix(commandText)
	return strings.TrimSpace(rest)
}

func stripIssuePrefix(s string) (string, bool) {
	lines := strings.Split(s, "\n")
	first := -1
	for i, line := range lines {
		if strings.TrimSpace(line) != "" {
			first = i
			break
		}
	}
	if first == -1 {
		return s, false
	}
	trimmed := strings.TrimLeft(lines[first], " \t")
	if !strings.HasPrefix(trimmed, issueCommandPrefix) {
		return s, false
	}
	rest := trimmed[len(issueCommandPrefix):]
	if rest != "" {
		if r0 := rest[0]; r0 != ' ' && r0 != '\t' && r0 != '\n' {
			return s, false
		}
	}
	lines[first] = rest
	return strings.Join(lines[first:], "\n"), true
}

// handleQuickCreate queues the background issue-authoring task after the user
// turn is durable. When media is pending, the task is created deferred and the
// existing media pipeline promotes it after task-owned attachments are bound.
func (r *Router) handleQuickCreate(ctx context.Context, set ResolverSet, inst ResolvedInstallation, requesterID, sessionID, messageID pgtype.UUID, prompt string, mediaPending bool, res *Result) (db.AgentTaskQueue, bool) {
	if prompt == "" {
		r.appendAssistantNote(ctx, sessionID, IssueUsageText)
		res.IssueUsage = true
		return db.AgentTaskQueue{}, false
	}
	task, err := set.QuickCreate.EnqueueQuickCreateChatTask(ctx, inst.WorkspaceID, requesterID, inst.AgentID, prompt, sessionID, messageID, mediaPending)
	if err != nil {
		r.logger.Error("channel router: quick-create enqueue failed",
			"chat_session_id", uuidString(sessionID), "err", err.Error())
		if mediaPending {
			// No resolver will run now, so clear the durable marker immediately.
			if clearErr := set.Session.BindMedia(ctx, BindMediaParams{
				MessageID:   messageID,
				SessionID:   sessionID,
				WorkspaceID: inst.WorkspaceID,
				Sender:      requesterID,
			}); clearErr != nil {
				r.logger.Warn("channel router: quick-create enqueue failure marker clear failed",
					"chat_session_id", uuidString(sessionID), "err", clearErr)
			}
		}
		r.appendAssistantNote(ctx, sessionID, IssueQueueFailedText)
		res.IssueQueueFailed = true
		return db.AgentTaskQueue{}, false
	}
	r.appendAssistantNote(ctx, sessionID, IssueQueuedAckText)
	res.IssueQueued = true
	return task, true
}

func (r *Router) appendAssistantNote(ctx context.Context, sessionID pgtype.UUID, text string) {
	if r.messages == nil {
		return
	}
	if _, err := r.messages.CreateChatMessage(ctx, db.CreateChatMessageParams{
		ChatSessionID: sessionID,
		Role:          "assistant",
		Content:       text,
	}); err != nil {
		r.logger.Warn("channel router: assistant note append failed",
			"chat_session_id", uuidString(sessionID), "err", err.Error())
	}
}
