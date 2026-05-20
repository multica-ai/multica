package notifications

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const commentExcerptMaxRunes = 200

type Event struct {
	WorkspaceID   pgtype.UUID
	RecipientID   pgtype.UUID
	RecipientType string
	Type          string
	ReferenceID   pgtype.UUID
	ReferenceType string
	Metadata      map[string]any
}

func Create(ctx context.Context, q *db.Queries, e Event) error {
	metadata, err := json.Marshal(e.Metadata)
	if err != nil {
		return err
	}
	_, err = q.CreateNotification(ctx, db.CreateNotificationParams{
		WorkspaceID:   e.WorkspaceID,
		RecipientID:   e.RecipientID,
		RecipientType: e.RecipientType,
		Type:          e.Type,
		ReferenceID:   e.ReferenceID,
		ReferenceType: e.ReferenceType,
		Metadata:      metadata,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	return err
}

func CreateForRecipients(ctx context.Context, q *db.Queries, base Event, recipients []Recipient) error {
	for _, recipient := range recipients {
		if !recipient.ID.Valid || (recipient.Type != "member" && recipient.Type != "agent") {
			continue
		}
		base.RecipientType = recipient.Type
		base.RecipientID = recipient.ID
		if err := Create(ctx, q, base); err != nil {
			return err
		}
	}
	return nil
}

type Recipient struct {
	Type string
	ID   pgtype.UUID
}

func RecipientsFromMentions(mentions []util.Mention) []Recipient {
	recipients := make([]Recipient, 0, len(mentions))
	for _, mention := range mentions {
		if mention.Type != "member" && mention.Type != "agent" {
			continue
		}
		id, err := util.ParseUUID(mention.ID)
		if err != nil {
			continue
		}
		recipients = append(recipients, Recipient{Type: mention.Type, ID: id})
	}
	return recipients
}

func RecipientsFromSubscribers(subs []db.IssueSubscriber) []Recipient {
	recipients := make([]Recipient, 0, len(subs))
	for _, sub := range subs {
		recipients = append(recipients, Recipient{Type: sub.UserType, ID: sub.UserID})
	}
	return recipients
}

func CommentExcerpt(content string) string {
	compact := strings.Join(strings.Fields(content), " ")
	runes := []rune(compact)
	if len(runes) <= commentExcerptMaxRunes {
		return compact
	}
	return string(runes[:commentExcerptMaxRunes])
}

func IssueMetadata(issue db.Issue, extra map[string]any) map[string]any {
	metadata := map[string]any{"issue_title": issue.Title}
	for k, v := range extra {
		metadata[k] = v
	}
	return metadata
}
