package service

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/channel"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ChannelService orchestrates agent ↔ channel communication.
type ChannelService struct {
	Queries *db.Queries
	AppURL  string // e.g., "https://app.example.com" for issue links
}

// NewChannelService creates a new ChannelService.
func NewChannelService(q *db.Queries, appURL string) *ChannelService {
	return &ChannelService{Queries: q, AppURL: appURL}
}

// AskQuestion sends a question from an agent to the first assigned channel on an issue.
// Returns the created outbound message ID for polling.
func (s *ChannelService) AskQuestion(ctx context.Context, issueID, agentID, question string) (string, error) {
	ic, err := s.Queries.GetFirstIssueChannel(ctx, util.ParseUUID(issueID))
	if err != nil {
		return "", fmt.Errorf("no channel assigned to issue: %w", err)
	}

	provider, ok := channel.GetProvider(ic.ChannelProvider)
	if !ok {
		return "", fmt.Errorf("unsupported channel provider: %s", ic.ChannelProvider)
	}

	var result channel.SendResult
	if !ic.ThreadRef.Valid || ic.ThreadRef.String == "" {
		// First message — create thread with issue context.
		issueCtx, err := s.buildIssueContext(ctx, issueID)
		if err != nil {
			slog.Warn("channel: failed to build issue context", "error", err)
			issueCtx = channel.IssueContext{Title: "Issue", Identifier: issueID}
		}
		result, err = provider.SendFirstMessage(ctx, ic.ChannelConfig, question, issueCtx)
		if err != nil {
			return "", fmt.Errorf("send first message: %w", err)
		}
		// Store thread reference for future messages.
		if err := s.Queries.UpdateIssueChannelThreadRef(ctx, db.UpdateIssueChannelThreadRefParams{
			ID:        ic.ID,
			ThreadRef: pgtype.Text{String: result.ThreadRef, Valid: true},
		}); err != nil {
			slog.Warn("channel: failed to store thread ref", "error", err)
		}
	} else {
		result, err = provider.SendMessage(ctx, ic.ChannelConfig, channel.Message{
			Text:      question,
			ThreadRef: ic.ThreadRef.String,
		})
		if err != nil {
			return "", fmt.Errorf("send message: %w", err)
		}
	}

	// Store outbound message.
	msg, err := s.Queries.CreateChannelMessage(ctx, db.CreateChannelMessageParams{
		IssueChannelID: ic.ID,
		Direction:      "outbound",
		Content:        question,
		ExternalID:     pgtype.Text{String: result.ExternalID, Valid: result.ExternalID != ""},
		SenderType:     "agent",
		SenderRef:      pgtype.Text{String: agentID, Valid: agentID != ""},
	})
	if err != nil {
		return "", fmt.Errorf("store outbound message: %w", err)
	}

	return util.UUIDToString(msg.ID), nil
}

// GetResponse checks if a user response has arrived after a given outbound message.
// First checks DB, then polls the channel provider (Slack API) for new replies.
func (s *ChannelService) GetResponse(ctx context.Context, messageID string) (string, bool, error) {
	msg, err := s.Queries.GetChannelMessage(ctx, util.ParseUUID(messageID))
	if err != nil {
		return "", false, fmt.Errorf("get message: %w", err)
	}

	// Check DB first.
	reply, err := s.Queries.GetLatestInboundAfter(ctx, db.GetLatestInboundAfterParams{
		IssueChannelID: msg.IssueChannelID,
		CreatedAt:      msg.CreatedAt,
	})
	if err == nil {
		return reply.Content, true, nil
	}
	if err != pgx.ErrNoRows {
		return "", false, fmt.Errorf("get inbound: %w", err)
	}

	// Not in DB — poll the channel provider for new replies.
	ic, err := s.Queries.GetIssueChannel(ctx, msg.IssueChannelID)
	if err != nil || !ic.ThreadRef.Valid {
		return "", false, nil
	}

	provider, ok := channel.GetProvider(ic.ChannelProvider)
	if !ok {
		return "", false, nil
	}

	// Use the outbound message's external_id as the "after" marker.
	afterTS := ""
	if msg.ExternalID.Valid {
		afterTS = msg.ExternalID.String
	}

	replies, err := provider.FetchReplies(ctx, ic.ChannelConfig, ic.ThreadRef.String, afterTS)
	if err != nil {
		slog.Warn("channel: fetch replies failed", "error", err)
		return "", false, nil
	}

	if len(replies) == 0 {
		return "", false, nil
	}

	// Store all fetched replies and return the first one.
	for _, r := range replies {
		exists, _ := s.Queries.ChannelMessageExistsByExternalID(ctx, pgtype.Text{String: r.ExternalID, Valid: true})
		if exists {
			continue
		}
		s.Queries.CreateChannelMessage(ctx, db.CreateChannelMessageParams{
			IssueChannelID: msg.IssueChannelID,
			Direction:      "inbound",
			Content:        r.Text,
			ExternalID:     pgtype.Text{String: r.ExternalID, Valid: true},
			SenderType:     "user",
			SenderRef:      pgtype.Text{String: r.SenderRef, Valid: r.SenderRef != ""},
		})
	}

	return replies[0].Text, true, nil
}

// HandleInboundMessage processes a user response received from a channel webhook.
func (s *ChannelService) HandleInboundMessage(ctx context.Context, providerName, externalChannelID, threadRef, text, externalID, senderRef string) error {
	// Find the channel by provider and external channel ID.
	ch, err := s.Queries.FindChannelByProviderAndExternalID(ctx, db.FindChannelByProviderAndExternalIDParams{
		Provider: providerName,
		Config:   []byte(externalChannelID),
	})
	if err != nil {
		return fmt.Errorf("find channel: %w", err)
	}

	// Find the issue-channel by thread reference.
	ic, err := s.Queries.FindIssueChannelByThreadRef(ctx, db.FindIssueChannelByThreadRefParams{
		ChannelID: ch.ID,
		ThreadRef: pgtype.Text{String: threadRef, Valid: true},
	})
	if err != nil {
		return fmt.Errorf("find issue channel by thread: %w", err)
	}

	// Dedup by external ID.
	if externalID != "" {
		exists, _ := s.Queries.ChannelMessageExistsByExternalID(ctx, pgtype.Text{String: externalID, Valid: true})
		if exists {
			return nil
		}
	}

	_, err = s.Queries.CreateChannelMessage(ctx, db.CreateChannelMessageParams{
		IssueChannelID: ic.ID,
		Direction:      "inbound",
		Content:        text,
		ExternalID:     pgtype.Text{String: externalID, Valid: externalID != ""},
		SenderType:     "user",
		SenderRef:      pgtype.Text{String: senderRef, Valid: senderRef != ""},
	})
	return err
}

// GetConversationHistory returns all channel messages for an issue.
func (s *ChannelService) GetConversationHistory(ctx context.Context, issueID string) ([]db.ChannelMessage, error) {
	return s.Queries.ListChannelMessagesByIssue(ctx, util.ParseUUID(issueID))
}

func (s *ChannelService) buildIssueContext(ctx context.Context, issueID string) (channel.IssueContext, error) {
	issue, err := s.Queries.GetIssue(ctx, util.ParseUUID(issueID))
	if err != nil {
		return channel.IssueContext{}, err
	}

	// Build identifier (e.g., "MUL-42").
	identifier := issueID
	if issue.Number > 0 {
		ws, err := s.Queries.GetWorkspace(ctx, issue.WorkspaceID)
		if err == nil && ws.IssuePrefix != "" {
			identifier = fmt.Sprintf("%s-%d", ws.IssuePrefix, issue.Number)
		}
	}

	return channel.IssueContext{
		Title:      issue.Title,
		Identifier: identifier,
		Status:     issue.Status,
		Priority:   issue.Priority,
		URL:        fmt.Sprintf("%s/issues/%s", s.AppURL, issueID),
	}, nil
}
