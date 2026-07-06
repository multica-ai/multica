package lark

import "github.com/jackc/pgx/v5/pgtype"

// Domain parameter types for the channel-backed Feishu store. They replace the
// retired db.*LarkParams shapes generated from queries/lark.sql, using the same
// channel-neutral field names as the domain entities in store.go. The store
// (channel_store.go) maps them onto the channel_* writes, folding the
// feishu-specific identifiers into the JSONB config at the DB boundary.

// GetInstallationInWorkspaceParams scopes an installation lookup to a workspace.
type GetInstallationInWorkspaceParams struct {
	ID          pgtype.UUID
	WorkspaceID pgtype.UUID
}

// UpsertInstallationParams carries the flat feishu installation fields for an
// install / re-install.
type UpsertInstallationParams struct {
	WorkspaceID        pgtype.UUID
	AgentID            pgtype.UUID
	AppID              string
	AppSecretEncrypted []byte
	BotOpenID          string
	InstallerUserID    pgtype.UUID
	TenantKey          pgtype.Text
	BotUnionID         pgtype.Text
	Region             string
}

// SetInstallationStatusParams flips an installation's status (active/revoked).
type SetInstallationStatusParams struct {
	ID     pgtype.UUID
	Status string
}

// SetInstallationBotUnionIDParams records the bot's union_id (backfill).
type SetInstallationBotUnionIDParams struct {
	ID         pgtype.UUID
	BotUnionID pgtype.Text
}

// AcquireWSLeaseParams fences the WS supervisor lease for an installation.
type AcquireWSLeaseParams struct {
	NewToken     pgtype.Text
	NewExpiresAt pgtype.Timestamptz
	ID           pgtype.UUID
}

// ReleaseWSLeaseParams releases a WS supervisor lease the caller still holds.
type ReleaseWSLeaseParams struct {
	ID           pgtype.UUID
	CurrentToken pgtype.Text
}

// GetUserBindingByOpenIDParams looks up a binding by its channel-native user id.
type GetUserBindingByOpenIDParams struct {
	InstallationID pgtype.UUID
	ChannelUserID  string
}

// CreateUserBindingParams binds a workspace member to a channel-native user id.
type CreateUserBindingParams struct {
	WorkspaceID    pgtype.UUID
	MulticaUserID  pgtype.UUID
	InstallationID pgtype.UUID
	ChannelUserID  string
	UnionID        pgtype.Text
}

// GetChatSessionBindingParams looks up a chat binding by its channel chat id.
type GetChatSessionBindingParams struct {
	InstallationID pgtype.UUID
	ChannelChatID  string
}

// CreateChatSessionBindingParams binds a chat_session to a channel chat.
type CreateChatSessionBindingParams struct {
	ChatSessionID  pgtype.UUID
	InstallationID pgtype.UUID
	ChannelChatID  string
	ChatType       string
}

// UpdateChatSessionBindingReplyTargetParams records the latest inbound trigger
// message + thread so the outbound patcher can thread its reply.
type UpdateChatSessionBindingReplyTargetParams struct {
	ChatSessionID pgtype.UUID
	LastMessageID pgtype.Text
	LastThreadID  pgtype.Text
}

// ClaimInboundDedupParams claims the two-phase idempotency row for a message.
type ClaimInboundDedupParams struct {
	InstallationID pgtype.UUID
	MessageID      string
}

// MarkInboundDedupProcessedParams marks a claimed message processed (fenced).
type MarkInboundDedupProcessedParams struct {
	InstallationID pgtype.UUID
	MessageID      string
	ClaimToken     pgtype.UUID
}

// ReleaseInboundDedupParams releases a claim on processing failure (fenced).
type ReleaseInboundDedupParams struct {
	InstallationID pgtype.UUID
	MessageID      string
	ClaimToken     pgtype.UUID
}

// RecordInboundDropParams writes a non-content drop audit row.
type RecordInboundDropParams struct {
	EventType        string
	DropReason       string
	InstallationID   pgtype.UUID
	ChannelChatID    pgtype.Text
	ChannelEventID   pgtype.Text
	ChannelMessageID pgtype.Text
}

// CreateBindingTokenParams mints a short-lived channel binding token.
type CreateBindingTokenParams struct {
	TokenHash      string
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	ChannelUserID  string
	ExpiresAt      pgtype.Timestamptz
}

// CreateOutboundCardMessageParams records an outbound card for a task/session.
type CreateOutboundCardMessageParams struct {
	ChatSessionID        pgtype.UUID
	ChannelChatID        string
	ChannelCardMessageID string
	Status               string
	TaskID               pgtype.UUID
}

// UpdateOutboundCardStatusParams transitions an outbound card's status.
type UpdateOutboundCardStatusParams struct {
	ID     pgtype.UUID
	Status string
}
