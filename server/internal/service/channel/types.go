// Package channel implements the business logic for the multi-participant
// channels and DMs feature. It deliberately depends only on the generated
// db package and the standard library so that it can be lifted wholesale
// into a sidecar binary if upstream rejects the integrated path (see the
// channels feature spec, Deliverable B).
//
// Handlers are responsible for HTTP concerns, authentication, WebSocket
// publishing, task enqueuement, and inbox writes. This package returns
// plain data and typed errors and never reaches outside the database.
package channel

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// TxStarter abstracts transaction creation. pgxpool.Pool satisfies it.
// Defined locally rather than imported from the parent service package so
// this package has no Multica-internal dependencies beyond db.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Sentinel errors. Handlers translate these to HTTP statuses; tests assert
// against errors.Is().
var (
	ErrNotFound      = errors.New("channel: not found")
	ErrForbidden     = errors.New("channel: forbidden")
	ErrConflict      = errors.New("channel: conflict")
	ErrInvalid       = errors.New("channel: invalid argument")
	ErrChannelClosed = errors.New("channel: archived")
)

// Actor types. Stored as text columns because membership is polymorphic.
const (
	ActorMember = "member"
	ActorAgent  = "agent"
)

// Channel kinds.
const (
	KindChannel = "channel"
	KindDM      = "dm"
)

// Channel visibilities.
const (
	VisibilityPublic  = "public"
	VisibilityPrivate = "private"
)

// Membership roles.
const (
	RoleMember = "member"
	RoleAdmin  = "admin"
)

// Membership notification levels.
const (
	NotifyAll      = "all"
	NotifyMentions = "mentions"
	NotifyNone     = "none"
)

// Deletion reasons recorded on channel_message.deleted_at writes.
const (
	DeletedByUser       = "user"
	DeletedByAdmin      = "admin"
	DeletedByRetention  = "retention"
	DeletedByModeration = "moderation"
)

// Actor identifies a polymorphic actor (workspace member or agent).
type Actor struct {
	Type string
	ID   pgtype.UUID
}

// IsMember reports whether the actor refers to a workspace member.
func (a Actor) IsMember() bool { return a.Type == ActorMember }

// IsAgent reports whether the actor refers to an agent.
func (a Actor) IsAgent() bool { return a.Type == ActorAgent }

// Equal reports whether two actors refer to the same entity.
func (a Actor) Equal(b Actor) bool {
	return a.Type == b.Type && a.ID.Valid && b.ID.Valid && a.ID.Bytes == b.ID.Bytes
}

// CreateChannelParams is the input to ChannelService.Create.
type CreateChannelParams struct {
	WorkspaceID   pgtype.UUID
	Name          string
	DisplayName   string
	Description   string
	Kind          string
	Visibility    string
	CreatedBy     Actor
	RetentionDays *int32
	Metadata      []byte // raw JSON, may be nil
}

// UpdateChannelParams is the input to ChannelService.Update. Nil fields are
// left unchanged. RetentionDaysSet=true with RetentionDays=nil clears the
// override (NULL in SQL); RetentionDaysSet=false leaves it untouched.
type UpdateChannelParams struct {
	DisplayName       *string
	Description       *string
	Visibility        *string
	RetentionDays     *int32
	RetentionDaysSet  bool
	Metadata          []byte
}

// AddMemberParams is the input to ChannelService.AddMember.
type AddMemberParams struct {
	Member             Actor
	Role               string // empty defaults to RoleMember
	AddedBy            *Actor // nil for self-join
	NotificationLevel  string // empty defaults to NotifyAll
}

// CreateMessageParams is the input to MessageService.Create.
type CreateMessageParams struct {
	ChannelID       pgtype.UUID
	Author          Actor
	Content         string
	ParentMessageID *pgtype.UUID
	Metadata        []byte
}

// ListMessagesParams is the input to MessageService.List.
type ListMessagesParams struct {
	ChannelID        pgtype.UUID
	BeforeCreatedAt  *pgtype.Timestamptz // nil = newest page
	Limit            int32               // capped to 200 by the service
	IncludeThreaded  bool                // true returns the full stream incl. replies
}
