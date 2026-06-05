package lark

import "time"

// OpenID is a Lark user's per-installation identifier. Different
// installations of the same app produce different open_ids for the
// same human user; cross-installation identity merging would need
// union_id (Phase 2). Typed alias instead of plain string so callers
// can't accidentally pass a Multica user UUID where a Lark open_id is
// expected.
type OpenID string

// ChatID identifies a Lark conversation (p2p or group). One ChatID maps
// to one Multica chat_session via lark_chat_session_binding.
type ChatID string

// ChatType discriminates p2p (single-user DM with the Bot) from group
// chats. The DB column constraints lark_chat_session_binding.lark_chat_type
// to the same two values.
type ChatType string

const (
	ChatTypeP2P   ChatType = "p2p"
	ChatTypeGroup ChatType = "group"
)

// InstallationStatus mirrors the lark_installation.status check
// constraint. A revoked installation accepts no further events; its
// WebSocket is torn down and inbound events are dropped with an
// audit row.
type InstallationStatus string

const (
	InstallationActive  InstallationStatus = "active"
	InstallationRevoked InstallationStatus = "revoked"
)

// DropReason enumerates the categories the inbound pipeline writes
// into lark_inbound_audit.drop_reason. The DB column is open TEXT so
// new reasons can be added without a migration; callers should reuse
// these constants to keep dashboards / queries consistent.
//
// All drop_reason values are recorded WITHOUT message body — see
// MUL-2671 §4.7 (drop-audit policy).
type DropReason string

const (
	// DropReasonUnboundUser — the sender's open_id has no row in
	// lark_user_binding for this installation. The Bot replies with the
	// binding card; the message itself is not stored.
	DropReasonUnboundUser DropReason = "unbound_user"

	// DropReasonNonWorkspaceMember — the sender resolved to a Multica
	// user, but that user is not a member of this installation's
	// workspace. The Bot replies with a "not in this workspace" notice;
	// the message itself is not stored.
	DropReasonNonWorkspaceMember DropReason = "non_workspace_member"

	// DropReasonNotAddressedInGroup — the message arrived in a group
	// chat but did not @ the Bot and was not a reply to a Bot card.
	// Group chats only ingest messages explicitly addressed to the Bot.
	DropReasonNotAddressedInGroup DropReason = "not_addressed_in_group"

	// DropReasonDuplicate — message_id already present in
	// lark_inbound_message_dedup. WebSocket reconnects can replay events;
	// this is the idempotency path.
	DropReasonDuplicate DropReason = "duplicate"

	// DropReasonRevokedInstallation — installation.status='revoked'.
	// The WS connection should already be closed; this catches any
	// in-flight events that landed during teardown.
	DropReasonRevokedInstallation DropReason = "revoked_installation"

	// DropReasonInvalidEvent — payload failed schema validation
	// (missing required fields, wrong event_type for this hook, etc.).
	DropReasonInvalidEvent DropReason = "invalid_event"
)

// BindingTokenTTL caps the lifetime of a member-binding token. The DB
// CHECK on lark_binding_token (`expires_at <= created_at + INTERVAL '15
// minutes'`) enforces the same bound at the storage layer, so a
// misconfigured caller or a hand-inserted SQL row cannot exceed it.
// Keep these two values in sync if the product value changes.
const BindingTokenTTL = 15 * time.Minute
