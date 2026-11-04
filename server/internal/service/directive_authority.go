package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"
	proposalkinds "github.com/multica-ai/multica/server/internal/service/proposal_kinds"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Directive authority (spec P4, G2; step 13.4). A directive is an issue the
// Secretary creates for a leader from the boss's own chat words. It is accepted
// only inside a live run of the workspace Secretary, whose originator is an
// owner or admin, and whose chat trigger was written by that same person.
// The handler checks that the caller is the Secretary; this function checks the
// run, the trigger, the target and the per-message cap.

var (
	ErrDirectiveRunNotLive         = errors.New("a directive needs a live run: this run has already finished")
	ErrDirectiveNotChatTriggered   = errors.New("a directive needs a run triggered by a chat message from the boss; this run was not")
	ErrDirectiveSessionNotLive     = errors.New("a directive needs a live chat session: this chat is archived or gone")
	ErrDirectiveOriginatorNotAdmin = errors.New("a directive needs a run started by an owner or admin of this workspace")
	ErrDirectiveNotOriginatorMsg   = errors.New("a directive needs the chat message to come from the run's originator")
	ErrDirectiveMessageMismatch    = errors.New("origin_id must be the chat message that triggered this run, or omitted")
	ErrDirectiveTargetNotFound     = errors.New("the parent issue of a directive was not found in this workspace")
	ErrDirectiveProposal           = errors.New("a directive cannot target a proposal: approvals keep their own decision flow, so ask the boss in chat instead")
	ErrDirectiveStageGate          = errors.New("a directive cannot target a stage gate (including Launch): gates keep their own decision flow, so ask the boss in chat instead")
)

// maxDirectiveQuoteRunes bounds the stored quote so one long message cannot
// bloat the issue's metadata.
const maxDirectiveQuoteRunes = 4000

// DirectiveAuthorityInput is what the handler knows about the request.
type DirectiveAuthorityInput struct {
	WorkspaceID pgtype.UUID
	// AgentID is the calling agent. The handler has already checked it is the
	// workspace Secretary.
	AgentID pgtype.UUID
	// TaskID is the server-trusted run (X-Task-ID).
	TaskID pgtype.UUID
	// ParentIssueID is the issue the directive sits under, if any.
	ParentIssueID pgtype.UUID
	// RequestedOriginID is the origin_id the caller sent, if any. When set it
	// must name the trigger message.
	RequestedOriginID pgtype.UUID
}

// DirectiveGrant is the verified trigger of a directive.
type DirectiveGrant struct {
	// MessageID is the trigger message, stored as the issue's origin_id.
	MessageID        pgtype.UUID
	ChatSessionID    pgtype.UUID
	OriginatorUserID pgtype.UUID
	// Quote is the boss's own words from the trigger batch.
	Quote string
}

// AuthorizeDirective verifies a directive against the database and returns the
// grant to stamp on the new issue. It returns one of the ErrDirective* values
// (possibly wrapped) or a *GuardrailError when the per-message cap is reached.
func AuthorizeDirective(ctx context.Context, q *db.Queries, in DirectiveAuthorityInput) (DirectiveGrant, error) {
	task, err := q.GetAgentTask(ctx, in.TaskID)
	if err != nil || task.AgentID != in.AgentID {
		return DirectiveGrant{}, ErrDirectiveRunNotLive
	}
	switch task.Status {
	case "completed", "failed", "cancelled":
		return DirectiveGrant{}, ErrDirectiveRunNotLive
	}

	if !task.ChatSessionID.Valid {
		return DirectiveGrant{}, ErrDirectiveNotChatTriggered
	}
	session, err := q.GetChatSessionInWorkspace(ctx, db.GetChatSessionInWorkspaceParams{
		ID: task.ChatSessionID, WorkspaceID: in.WorkspaceID,
	})
	if err != nil {
		return DirectiveGrant{}, ErrDirectiveSessionNotLive
	}
	if session.Status == "archived" {
		return DirectiveGrant{}, ErrDirectiveSessionNotLive
	}

	if !task.OriginatorUserID.Valid {
		return DirectiveGrant{}, ErrDirectiveOriginatorNotAdmin
	}
	member, err := q.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: task.OriginatorUserID, WorkspaceID: in.WorkspaceID,
	})
	if err != nil || (member.Role != "owner" && member.Role != "admin") {
		return DirectiveGrant{}, ErrDirectiveOriginatorNotAdmin
	}
	// chat_message has no author column: a direct chat's user messages belong to
	// the session creator, so the creator must be the run's originator.
	if session.CreatorID != task.OriginatorUserID {
		return DirectiveGrant{}, ErrDirectiveNotOriginatorMsg
	}

	// The trigger is the input batch the run was started for. A retry clone
	// inherits chat_input_task_id, so the batch owner is that root when it is set.
	batchOwner := task.ID
	if task.ChatInputTaskID.Valid {
		batchOwner = task.ChatInputTaskID
	}
	batch, err := q.ListChatInputMessages(ctx, batchOwner)
	if err != nil || len(batch) == 0 {
		return DirectiveGrant{}, ErrDirectiveNotChatTriggered
	}
	trigger := batch[len(batch)-1]
	if trigger.ChatSessionID != task.ChatSessionID {
		return DirectiveGrant{}, ErrDirectiveNotChatTriggered
	}
	if in.RequestedOriginID.Valid && in.RequestedOriginID != trigger.ID {
		return DirectiveGrant{}, ErrDirectiveMessageMismatch
	}

	if in.ParentIssueID.Valid {
		if err := checkDirectiveTarget(ctx, q, in.WorkspaceID, in.ParentIssueID); err != nil {
			return DirectiveGrant{}, err
		}
	}

	filed, err := q.CountDirectiveIssuesForMessage(ctx, db.CountDirectiveIssuesForMessageParams{
		WorkspaceID: in.WorkspaceID, OriginID: trigger.ID,
	})
	if err != nil {
		return DirectiveGrant{}, fmt.Errorf("count directives: %w", err)
	}
	if err := CheckGuardrail(GuardrailDirectives, int(filed)+1); err != nil {
		return DirectiveGrant{}, err
	}

	return DirectiveGrant{
		MessageID:        trigger.ID,
		ChatSessionID:    task.ChatSessionID,
		OriginatorUserID: task.OriginatorUserID,
		Quote:            directiveQuote(batch),
	}, nil
}

// checkDirectiveTarget refuses a parent that is a proposal's issue. A stage gate
// (which includes the Launch gate) and an ordinary approval each get their own
// message, so the Secretary can point the boss to the right place.
func checkDirectiveTarget(ctx context.Context, q *db.Queries, workspaceID, parentID pgtype.UUID) error {
	parent, err := q.GetIssueInWorkspace(ctx, db.GetIssueInWorkspaceParams{ID: parentID, WorkspaceID: workspaceID})
	if err != nil {
		return ErrDirectiveTargetNotFound
	}
	if parent.OriginType.String != "proposal" || !parent.OriginID.Valid {
		return nil
	}
	proposal, err := q.GetProposalInWorkspace(ctx, db.GetProposalInWorkspaceParams{
		ID: parent.OriginID, WorkspaceID: workspaceID,
	})
	if err == nil && proposal.Kind == proposalkinds.KeyStageGate {
		return ErrDirectiveStageGate
	}
	return ErrDirectiveProposal
}

// directiveQuote joins the batch's user messages in order, trimmed and bounded.
func directiveQuote(batch []db.ChatMessage) string {
	parts := make([]string, 0, len(batch))
	for _, m := range batch {
		if s := strings.TrimSpace(m.Content); s != "" {
			parts = append(parts, s)
		}
	}
	quote := strings.Join(parts, "\n\n")
	if utf8.RuneCountInString(quote) > maxDirectiveQuoteRunes {
		runes := []rune(quote)
		quote = string(runes[:maxDirectiveQuoteRunes]) + "…"
	}
	return quote
}

// Metadata is the issue metadata written with a directive: the quote and where
// it came from. It is stored under the "directive" key.
func (g DirectiveGrant) Metadata() ([]byte, error) {
	return json.Marshal(map[string]any{
		"directive": map[string]any{
			"quote":              g.Quote,
			"message_id":         uuidString(g.MessageID),
			"chat_session_id":    uuidString(g.ChatSessionID),
			"originator_user_id": uuidString(g.OriginatorUserID),
		},
	})
}
