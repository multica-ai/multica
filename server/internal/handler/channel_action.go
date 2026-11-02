package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// User-facing copy the chat replies speak, English like the rest of the chat
// adapters. Each refusal names what to do instead, and carries the app link.
const (
	chatMsgNotLinked        = "Link your Multica account to decide from chat."
	chatMsgNotOwner         = "Only an owner or admin can decide from chat."
	chatMsgNotMember        = "You are no longer a member of this workspace."
	chatMsgExpired          = "This button has expired."
	chatMsgInvalid          = "This button is not valid."
	chatMsgNotFound         = "This proposal is not in your workspace."
	chatMsgAlreadyDecided   = "Already decided. Nothing changed."
	chatMsgRevising         = "The squad is revising this proposal. Decide it once the revised version comes back."
	chatMsgFailed           = "That did not go through, so nothing changed. Open the app to decide."
	chatMsgAgentRefused     = "Agents cannot decide proposals."
	chatToastNotOwner       = "Only an owner or admin can decide"
	chatToastExpired        = "This button has expired"
	chatToastInvalid        = "This button is not valid"
	chatToastNotFound       = "Not in your workspace"
	chatToastAlreadyDone    = "Already decided"
	chatToastRevising       = "Waiting for the squad"
	chatToastFailed         = "Open the app to decide"
	chatToastAgentRefused   = "Agents cannot decide"
	chatToastNotLinked      = "Link your account first"
	chatToastNotMember      = "Not a member any more"
	chatToastSnoozedAlready = "Already snoozed"
)

// ChannelActions answers presses of the buttons a chat app's brief push carries
// (spec 2.7, plan 9.11). It implements channel.ActionHandler. A press resolves
// the platform user to a member through the binding, requires that member to be
// an owner or admin of the binding's workspace, verifies the signed token, and
// then calls settleProposalDecision, the path the app's decide endpoint uses.
// No decision logic lives here.
type ChannelActions struct {
	h      *Handler
	signer *service.ActionSigner
	now    func() time.Time
}

// NewChannelActions builds the chat-button handler over the server's handler and
// the action signer built from the server secret.
func NewChannelActions(h *Handler, signer *service.ActionSigner) *ChannelActions {
	return &ChannelActions{h: h, signer: signer, now: time.Now}
}

// ActionKey groups buttons by the proposal they act on. It reads the proposal id
// without verifying the token, which is fine for grouping: a press is verified
// before anything changes.
func (a *ChannelActions) ActionKey(data string) string {
	id, _ := service.ActionTokenProposalID(data)
	return id
}

// Press applies one button press. A refusal is a result with a reply; only a
// failure to read the binding or the membership is returned as an error.
func (a *ChannelActions) Press(ctx context.Context, p channel.Press) (channel.PressResult, error) {
	if p.IsBot {
		return channel.PressResult{Toast: chatToastAgentRefused, Reply: chatMsgAgentRefused}, nil
	}
	binding, ok, err := a.binding(ctx, p.Sender)
	if err != nil {
		return channel.PressResult{}, err
	}
	if !ok {
		return a.refuse(chatToastNotLinked, chatMsgNotLinked, a.appRootLink()), nil
	}

	member, err := a.h.Queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID: binding.MulticaUserID, WorkspaceID: binding.WorkspaceID,
	})
	if err != nil {
		if isNotFound(err) {
			return a.refuse(chatToastNotMember, chatMsgNotMember, a.appRootLink()), nil
		}
		return channel.PressResult{}, fmt.Errorf("load member: %w", err)
	}
	if !roleAllowed(member.Role, "owner", "admin") {
		return a.refuse(chatToastNotOwner, chatMsgNotOwner, a.officeLink(ctx, binding.WorkspaceID, "")), nil
	}

	signed, err := a.signer.Verify(p.Data)
	switch {
	case errors.Is(err, service.ErrActionTokenExpired):
		return a.refuse(chatToastExpired, chatMsgExpired,
			a.officeLink(ctx, binding.WorkspaceID, uuidToString(signed.ProposalID))), nil
	case err != nil:
		return a.refuse(chatToastInvalid, chatMsgInvalid, a.officeLink(ctx, binding.WorkspaceID, "")), nil
	}
	return a.apply(ctx, binding, signed), nil
}

// apply makes the decision a verified token names. The decider is the binding's
// member, and the proposal is looked up in the binding's workspace, so a token
// for a proposal elsewhere is "not found" and changes nothing.
func (a *ChannelActions) apply(ctx context.Context, binding db.ChannelUserBinding, signed service.SignedAction) channel.PressResult {
	in := proposalDecision{
		WorkspaceID: binding.WorkspaceID,
		ProposalID:  signed.ProposalID,
		Member:      binding.MulticaUserID,
		Decision:    signed.Action,
	}
	link := a.officeLink(ctx, binding.WorkspaceID, uuidToString(signed.ProposalID))

	if signed.Action == service.ProposalDecisionSnooze {
		row, err := a.h.Queries.GetProposalWithIssue(ctx, db.GetProposalWithIssueParams{
			ID: signed.ProposalID, WorkspaceID: binding.WorkspaceID,
		})
		if err != nil {
			if isNotFound(err) {
				return a.refuse(chatToastNotFound, chatMsgNotFound, "")
			}
			slog.Warn("chat snooze: could not load proposal", "error", err)
			return a.refuse(chatToastFailed, chatMsgFailed, link)
		}
		// A second snooze press while the first still holds changes nothing: the
		// proposal is already put aside, so the press must not move its date.
		if row.Proposal.State == service.ProposalStatePending && row.Proposal.SnoozedUntil.Valid &&
			row.Proposal.SnoozedUntil.Time.After(a.now()) {
			return channel.PressResult{
				Toast:   chatToastSnoozedAlready,
				Reply:   fmt.Sprintf("Already snoozed until %s. Nothing changed. %s", row.Proposal.SnoozedUntil.Time.UTC().Format("Jan 2"), link),
				Settled: true,
			}
		}
		in.Until = a.now().Add(service.ActionSnoozeDays * 24 * time.Hour)
	}

	resp, err := a.h.settleProposalDecision(ctx, uuidToString(binding.MulticaUserID), in)
	if err != nil {
		return a.decisionFailure(err, link)
	}
	return decidedResult(signed.Action, resp, in.Until, link)
}

// decisionFailure turns a refused decision into the reply the chat shows.
// Nothing changed in any of these cases: the service refuses before it writes.
func (a *ChannelActions) decisionFailure(err error, link string) channel.PressResult {
	switch {
	case errors.Is(err, service.ErrProposalNotFound):
		return a.refuse(chatToastNotFound, chatMsgNotFound, "")
	case errors.Is(err, service.ErrProposalAlreadyDecided):
		return channel.PressResult{Toast: chatToastAlreadyDone, Reply: chatMsgAlreadyDecided, Settled: true}
	case errors.Is(err, service.ErrProposalRevising):
		return a.refuse(chatToastRevising, chatMsgRevising, "")
	}
	slog.Warn("chat decision failed", "error", err)
	return a.refuse(chatToastFailed, chatMsgFailed, link)
}

// decidedResult is the reply to a decision that went through.
func decidedResult(action string, resp ProposalResponse, until time.Time, link string) channel.PressResult {
	var text, toast string
	switch action {
	case service.ProposalDecisionApprove:
		toast, text = "Approved", fmt.Sprintf("Approved: %s.", resp.Title)
	case service.ProposalDecisionReject:
		toast, text = "Rejected", fmt.Sprintf("Rejected: %s. The squad leader has been told.", resp.Title)
	case service.ProposalDecisionSnooze:
		toast = "Snoozed for 7 days"
		text = fmt.Sprintf("Snoozed until %s: %s.", until.UTC().Format("Jan 2"), resp.Title)
	}
	if link != "" {
		text += " " + link
	}
	return channel.PressResult{Toast: toast, Reply: text, Settled: true}
}

// refuse is a press that changed nothing. link, when set, is the app link the
// reply offers in place of the button.
func (a *ChannelActions) refuse(toast, text, link string) channel.PressResult {
	if link != "" {
		text += " Open the app: " + link
	}
	return channel.PressResult{Toast: toast, Reply: text}
}

// binding resolves the platform user to the Multica binding in their
// installation. A binding of another chat app does not count.
func (a *ChannelActions) binding(ctx context.Context, s channel.Sender) (db.ChannelUserBinding, bool, error) {
	b, err := a.h.Queries.GetChannelUserBindingByUserID(ctx, db.GetChannelUserBindingByUserIDParams{
		InstallationID: s.InstallationID, ChannelUserID: s.ChannelUserID,
	})
	if err != nil {
		if isNotFound(err) {
			return db.ChannelUserBinding{}, false, nil
		}
		return db.ChannelUserBinding{}, false, fmt.Errorf("load chat binding: %w", err)
	}
	if b.ChannelType != string(s.ChannelType) {
		return db.ChannelUserBinding{}, false, nil
	}
	return b, true, nil
}

// officeLink is the Office page of the workspace, with the decision in it when
// one is named. It is empty when the app URL or the workspace is unknown, and
// the reply then has no link rather than a broken one.
func (a *ChannelActions) officeLink(ctx context.Context, workspaceID pgtype.UUID, decisionID string) string {
	ws, err := a.h.Queries.GetWorkspace(ctx, workspaceID)
	if err != nil {
		slog.Warn("chat link: could not load workspace", "error", err)
		return ""
	}
	link := service.OfficeURL(a.h.cfg.AppURL, ws.Slug)
	if link == "" || decisionID == "" {
		return link
	}
	return link + "?" + url.Values{"decision": {decisionID}}.Encode()
}

// appRootLink is the app's front page, for a sender with no workspace to link to.
func (a *ChannelActions) appRootLink() string { return a.h.cfg.AppURL }
