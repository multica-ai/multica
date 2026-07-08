package db

import (
	"github.com/jackc/pgx/v5/pgtype"
)

type Channel struct {
	ID          pgtype.UUID        `json:"id"`
	WorkspaceID pgtype.UUID        `json:"workspace_id"`
	Name        string             `json:"name"`
	Description pgtype.Text        `json:"description"`
	IsPrivate   bool               `json:"is_private"`
	CreatedBy   pgtype.UUID        `json:"created_by"`
	CreatedAt   pgtype.Timestamptz `json:"created_at"`
}

type ChannelMember struct {
	ChannelID pgtype.UUID        `json:"channel_id"`
	MemberID  pgtype.UUID        `json:"member_id"`
	JoinedAt  pgtype.Timestamptz `json:"joined_at"`
}

type ChannelMessage struct {
	ID        pgtype.UUID        `json:"id"`
	ChannelID pgtype.UUID        `json:"channel_id"`
	AuthorID  pgtype.UUID        `json:"author_id"`
	Content   string             `json:"content"`
	ParentID  pgtype.UUID        `json:"parent_id"`
	EditedAt  pgtype.Timestamptz `json:"edited_at"`
	CreatedAt pgtype.Timestamptz `json:"created_at"`
}
