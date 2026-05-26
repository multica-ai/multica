package autosubscribe

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type Source string

const (
	SourceIssueCreator            Source = "issue_creator"
	SourceIssueAssignee           Source = "issue_assignee"
	SourceCommentAuthor           Source = "comment_author"
	SourceIssueDescriptionMention Source = "issue_description_mention"
	SourceCommentMention          Source = "comment_mention"
	SourceQuickCreateRequester    Source = "quick_create_requester"
)

type Preferences struct {
	IssueCreator            bool
	IssueAssignee           bool
	CommentAuthor           bool
	IssueDescriptionMention bool
	CommentMention          bool
	QuickCreateRequester    bool
}

func NewUserDefaults() Preferences {
	return Preferences{
		IssueCreator:            true,
		IssueAssignee:           true,
		CommentAuthor:           true,
		IssueDescriptionMention: false,
		CommentMention:          false,
		QuickCreateRequester:    true,
	}
}

func FromRow(row db.AutoSubscribePreference) Preferences {
	return Preferences{
		IssueCreator:            row.IssueCreator,
		IssueAssignee:           row.IssueAssignee,
		CommentAuthor:           row.CommentAuthor,
		IssueDescriptionMention: row.IssueDescriptionMention,
		CommentMention:          row.CommentMention,
		QuickCreateRequester:    row.QuickCreateRequester,
	}
}

func LoadPreferences(ctx context.Context, queries *db.Queries, workspaceID, userID pgtype.UUID) (Preferences, error) {
	row, err := queries.GetAutoSubscribePreference(ctx, db.GetAutoSubscribePreferenceParams{
		WorkspaceID: workspaceID,
		UserID:      userID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return NewUserDefaults(), nil
		}
		return Preferences{}, err
	}
	return FromRow(row), nil
}

func (p Preferences) Enabled(source Source) bool {
	switch source {
	case SourceIssueCreator:
		return p.IssueCreator
	case SourceIssueAssignee:
		return p.IssueAssignee
	case SourceCommentAuthor:
		return p.CommentAuthor
	case SourceIssueDescriptionMention:
		return p.IssueDescriptionMention
	case SourceCommentMention:
		return p.CommentMention
	case SourceQuickCreateRequester:
		return p.QuickCreateRequester
	default:
		return false
	}
}

func ShouldSubscribe(ctx context.Context, queries *db.Queries, workspaceID, userType, userID string, source Source) bool {
	if userType != "member" {
		return userType == "agent"
	}

	prefs, err := LoadPreferences(ctx, queries, util.MustParseUUID(workspaceID), util.MustParseUUID(userID))
	if err != nil {
		return false
	}
	return prefs.Enabled(source)
}
