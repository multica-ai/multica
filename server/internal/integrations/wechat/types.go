package wechat

import "time"

type ChatType string

const (
	ChatTypeSingle ChatType = "single"
	ChatTypeGroup  ChatType = "group"
)

type InstallationStatus string

const (
	InstallationActive  InstallationStatus = "active"
	InstallationRevoked InstallationStatus = "revoked"
)

type DropReason string

const (
	DropReasonUnboundUser          DropReason = "unbound_user"
	DropReasonNonWorkspaceMember   DropReason = "non_workspace_member"
	DropReasonNotAddressedInGroup  DropReason = "not_addressed_in_group"
	DropReasonDuplicate            DropReason = "duplicate"
	DropReasonRevokedInstallation  DropReason = "revoked_installation"
	DropReasonInvalidEvent         DropReason = "invalid_event"
)

// BindingTokenTTL caps the lifetime of a member-binding token.
const BindingTokenTTL = 15 * time.Minute

// InboundMessage is the normalized shape the WS connector hands to the
// Dispatcher after decoding the aibot_msg_callback body.
type InboundMessage struct {
	MessageID      string
	BotID          string
	ChatID         string
	ChatType       ChatType
	SenderUserID   string
	SenderName     string
	Body           string
	MsgType        string
	CallbackReqID  string
	AddressedToBot bool
}

type Outcome string

const (
	OutcomeDropped        Outcome = "dropped"
	OutcomeNeedsBinding   Outcome = "needs_binding"
	OutcomeIngested       Outcome = "ingested"
	OutcomeAgentOffline   Outcome = "agent_offline"
	OutcomeAgentArchived  Outcome = "agent_archived"
)

type DispatchResult struct {
	Outcome        Outcome
	DropReason     DropReason
	InstallationID string
	ChatSessionID  string
	SenderUserID   string
}
