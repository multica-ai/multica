package handler

import (
	"context"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// chatDecisionsLimit is how many pending decisions /decisions names. The rest
// are counted, with a link to the Office.
const chatDecisionsLimit = 5

const (
	chatMsgNoBrief     = "There is no brief yet."
	chatMsgNoDecisions = "No decisions are waiting for you."
)

// Command answers /brief and /decisions for a linked member (spec 2.7, plan
// 9.11). The sender's binding names the workspace, and a member sees what the
// app shows a member: the briefs and the pending proposals are readable by any
// member, and deciding stays with owners and admins, as the app enforces. handled
// is false when the sender is not a member of a workspace, so the message takes
// the ordinary path and the binding prompt.
func (a *ChannelActions) Command(ctx context.Context, c channel.Command) (channel.Message, bool, error) {
	// A bot or agent sender is consumed without a reply: its command is never
	// answered, and it does not fall through to the binding prompt either.
	if c.IsBot {
		return channel.Message{}, true, nil
	}
	binding, ok, err := a.binding(ctx, c.Sender)
	if err != nil || !ok {
		return channel.Message{}, false, err
	}
	if _, err := a.h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: binding.MulticaUserID, WorkspaceID: binding.WorkspaceID,
	}); err != nil {
		if isNotFound(err) {
			return channel.Message{}, false, nil
		}
		return channel.Message{}, false, fmt.Errorf("load member: %w", err)
	}

	switch c.Name {
	case channel.CommandBrief:
		msg, err := a.latestBrief(ctx, binding.WorkspaceID)
		return msg, err == nil, err
	case channel.CommandDecisions:
		msg, err := a.pendingDecisions(ctx, binding.WorkspaceID)
		return msg, err == nil, err
	}
	return channel.Message{}, false, nil
}

// latestBrief replies with the headline of the newest brief and a link to it.
func (a *ChannelActions) latestBrief(ctx context.Context, workspaceID pgtype.UUID) (channel.Message, error) {
	briefs, err := a.h.Queries.ListBriefs(ctx, db.ListBriefsParams{WorkspaceID: workspaceID, RowLimit: 1})
	if err != nil {
		return channel.Message{}, fmt.Errorf("list briefs: %w", err)
	}
	if len(briefs) == 0 {
		return channel.Message{Text: chatMsgNoBrief}, nil
	}
	brief := briefs[0]
	headline := service.BriefHeadline(brief.Body)
	if headline == "" {
		headline = brief.Title
	}
	text := headline
	if link := a.officeLinkQuery(ctx, workspaceID, url.Values{"brief": {uuidToString(brief.ID)}}); link != "" {
		text += "\n\nBrief: " + link
	}
	return channel.Message{Text: text}, nil
}

// pendingDecisions replies with up to chatDecisionsLimit pending proposals, each
// with its title and its Office link. A snoozed proposal is left out until its
// snooze ends, as the brief push leaves it out.
func (a *ChannelActions) pendingDecisions(ctx context.Context, workspaceID pgtype.UUID) (channel.Message, error) {
	rows, err := a.h.Queries.ListProposals(ctx, db.ListProposalsParams{
		WorkspaceID: workspaceID,
		State:       pgtype.Text{String: service.ProposalStatePending, Valid: true},
	})
	if err != nil {
		return channel.Message{}, fmt.Errorf("list proposals: %w", err)
	}
	now := a.now()
	var pending []db.ListProposalsRow
	for _, row := range rows {
		if row.Proposal.SnoozedUntil.Valid && row.Proposal.SnoozedUntil.Time.After(now) {
			continue
		}
		pending = append(pending, row)
	}
	if len(pending) == 0 {
		return channel.Message{Text: chatMsgNoDecisions}, nil
	}

	var b strings.Builder
	if len(pending) == 1 {
		b.WriteString("1 decision needs you:")
	} else {
		fmt.Fprintf(&b, "%d decisions need you:", len(pending))
	}
	shown := pending
	if len(shown) > chatDecisionsLimit {
		shown = shown[:chatDecisionsLimit]
	}
	for _, row := range shown {
		b.WriteString("\n- " + oneLineChat(row.IssueTitle))
		if link := a.officeLink(ctx, workspaceID, uuidToString(row.Proposal.ID)); link != "" {
			b.WriteString(" " + link)
		}
	}
	if extra := len(pending) - len(shown); extra > 0 {
		b.WriteString(fmt.Sprintf("\n...and %d more in the Office", extra))
		if link := a.officeLink(ctx, workspaceID, ""); link != "" {
			b.WriteString(": " + link)
		}
		b.WriteString(".")
	}
	return channel.Message{Text: b.String()}, nil
}

// oneLineChat keeps a title to one line in a chat reply.
func oneLineChat(s string) string { return strings.Join(strings.Fields(s), " ") }
