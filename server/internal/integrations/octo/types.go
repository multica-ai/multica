package octo

import (
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// UID is an Octo user's identifier within a deployment. Typed alias rather than
// a plain string so callers can't accidentally pass a Multica user UUID where an
// Octo uid is expected.
type UID string

// ChannelID identifies an Octo conversation (DM or group). One ChannelID maps to
// one Multica chat_session via octo_chat_session_binding.
type ChannelID string

// ChannelType discriminates conversation kinds, matching the WuKongIM channel
// types carried on the wire.
type ChannelType int16

const (
	ChannelDM    ChannelType = 1 // direct (1:1) message
	ChannelGroup ChannelType = 2 // group chat
	ChannelTopic ChannelType = 5 // community topic / thread
)

// InstallationStatus mirrors the octo_installation.status CHECK constraint.
type InstallationStatus string

const (
	InstallationActive  InstallationStatus = "active"
	InstallationRevoked InstallationStatus = "revoked"
)

// BindingTokenTTL caps the lifetime of a member-binding token. The DB CHECK on
// octo_binding_token enforces the same bound at the storage layer. Keep these in
// sync if the product value changes.
const BindingTokenTTL = 15 * time.Minute

// InboundMessage is the dispatcher's input: a single Octo message already
// decoded by the transport layer (internal/integrations/im) and normalized into
// the fields the business pipeline needs. The bridge that owns the im.Socket
// converts an im.BotMessage into this shape (resolving robot_id → routing,
// computing AddressedToBot) so the dispatcher has no direct dependency on the
// transport package.
type InboundMessage struct {
	// RobotID identifies which bot received the event — the routing key to the
	// octo_installation row (octo_installation.robot_id).
	RobotID string
	// MessageID is the Octo/WuKongIM message id; the dedup key. Empty means the
	// event carries no id and dedup is skipped.
	MessageID string
	// SenderUID is the Octo uid of the human (or bot) who sent the message.
	SenderUID UID
	ChannelID ChannelID
	// ChannelType is the WuKongIM channel type (DM / group / topic).
	ChannelType ChannelType
	// Body is the message text handed to the agent.
	Body string
	// AddressedToBot is the bridge's verdict on whether a group message was
	// directed at the bot (@mention or reply-to-bot). Ignored for DMs.
	AddressedToBot bool
}

// Outcome categorizes what the Dispatcher decided to do with an InboundMessage.
type Outcome string

const (
	// OutcomeDropped — the message was not ingested (duplicate, revoked
	// installation, not addressed in a group, or an infra error before any
	// durable write).
	OutcomeDropped Outcome = "dropped"

	// OutcomeNeedsBinding — the sender uid is unbound; the caller should reply
	// with a binding prompt. The message itself is NOT stored.
	OutcomeNeedsBinding Outcome = "needs_binding"

	// OutcomeIngested — the message landed in chat_session and (unless the agent
	// is offline/archived) a chat task was enqueued.
	OutcomeIngested Outcome = "ingested"

	// OutcomeAgentOffline — the message landed in chat_session but the agent has
	// no runtime configured, so no task could be enqueued. The caller should
	// tell the user the agent is offline.
	OutcomeAgentOffline Outcome = "agent_offline"

	// OutcomeAgentArchived — the message landed in chat_session but the agent is
	// archived. The caller should tell the user to unarchive or rebind.
	OutcomeAgentArchived Outcome = "agent_archived"
)

// DropReason enumerates the categories the inbound pipeline writes into
// octo_inbound_audit.drop_reason. The DB column is open TEXT so new reasons can
// be added without a migration; reuse these constants for consistent
// dashboards. All drop_reason values are recorded WITHOUT message body.
type DropReason string

const (
	DropReasonUnboundUser         DropReason = "unbound_user"
	DropReasonNonWorkspaceMember  DropReason = "non_workspace_member"
	DropReasonNotAddressedInGroup DropReason = "not_addressed_in_group"
	DropReasonDuplicate           DropReason = "duplicate"
	DropReasonRevokedInstallation DropReason = "revoked_installation"
	DropReasonInvalidEvent        DropReason = "invalid_event"
)

// DispatchResult is the typed outcome of Dispatcher.Handle.
type DispatchResult struct {
	Outcome    Outcome
	DropReason DropReason

	// InstallationID is the resolved installation (zero when routing failed).
	InstallationID pgtype.UUID
	// ChatSessionID is set when the message was ingested into a session.
	ChatSessionID pgtype.UUID
	// SenderUID echoes the sender so the caller's reply (binding prompt, offline
	// notice) targets the right user without re-parsing.
	SenderUID UID
	// TaskID is set when a chat task was enqueued.
	TaskID pgtype.UUID
}
