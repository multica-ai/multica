package wecom

import "time"

// Userid is a WeCom member identifier (plaintext after resolution).
type Userid string

// ChatID identifies a WeCom conversation (single DM or group).
type ChatID string

// ChatType mirrors wecom_chat_session_binding.wecom_chat_type.
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
	DropReasonUseridResolveFailed DropReason = "userid_resolve_failed"
)

// BindingTokenTTL is the binding token lifetime. expires_at is computed in
// Go while created_at defaults to DB now(); keep below the CHECK cap
// (created_at + 15 minutes) so app/DB clock skew cannot reject the INSERT.
const BindingTokenTTL = 14*time.Minute + 59*time.Second

const DefaultWecomWSURL = "wss://openws.work.weixin.qq.com"
