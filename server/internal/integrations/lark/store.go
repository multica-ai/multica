package lark

// Channel-backed store for the Feishu integration.
//
// MUL-3515 generalized the lark_* tables into channel_* (a channel_type
// discriminator + a JSONB `config` blob for the platform-specific
// identifiers/credentials). This file owns the one boundary where that JSONB
// is (de)serialized: the rest of the package keeps working with flat domain
// structs whose fields mirror the retired db.Lark* rows one-for-one, so the
// call sites are a mechanical rename rather than a reshape.
//
// The feishu config blob carries exactly the columns that used to be flat on
// lark_installation / lark_user_binding:
//
//	installation: app_id, app_secret_encrypted (base64), tenant_key,
//	              bot_open_id, bot_union_id, region
//	user binding: union_id
//
// app_secret_encrypted is secretbox ciphertext stored as a base64 string.
// The decoder is whitespace-tolerant on purpose: the migration backfill writes
// it via PostgreSQL encode(...,'base64'), which MIME-wraps every 76 chars, and
// a sealed ~72-byte secret exceeds that. The encoder always emits unwrapped
// base64, so rows written by Go are already clean; stripping on read keeps
// both sources interchangeable.

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Installation is the flat, feishu-shaped view of a channel_installation row.
// It keeps field parity with the lark_installation row it replaced, so the
// cutover was a rename at the ~190 call sites. The feishu-specific fields
// (AppID, AppSecretEncrypted, TenantKey, BotOpenID, BotUnionID, Region) come
// from the JSONB config; the rest are flat columns.
type Installation struct {
	ID                 pgtype.UUID
	WorkspaceID        pgtype.UUID
	AgentID            pgtype.UUID
	AppID              string
	AppSecretEncrypted []byte
	TenantKey          pgtype.Text
	BotOpenID          string
	InstallerUserID    pgtype.UUID
	Status             string
	WsLeaseToken       pgtype.Text
	WsLeaseExpiresAt   pgtype.Timestamptz
	InstalledAt        pgtype.Timestamptz
	CreatedAt          pgtype.Timestamptz
	UpdatedAt          pgtype.Timestamptz
	BotUnionID         pgtype.Text
	Region             string
}

// UserBinding is the flat view of a channel_user_binding row. ChannelUserID is
// the feishu open_id; UnionID (secondary identity) lives in the JSONB config.
type UserBinding struct {
	ID             pgtype.UUID
	WorkspaceID    pgtype.UUID
	MulticaUserID  pgtype.UUID
	InstallationID pgtype.UUID
	ChannelUserID  string
	UnionID        pgtype.Text
	BoundAt        pgtype.Timestamptz
}

// ChatSessionBinding is the flat view of a channel_chat_session_binding row.
type ChatSessionBinding struct {
	ID             pgtype.UUID
	ChatSessionID  pgtype.UUID
	InstallationID pgtype.UUID
	ChannelChatID  string
	ChatType       string
	// Config carries the real chat id (larkBindingConfig) when ChannelChatID
	// is a composite "chat:thread" topic-isolation key; "{}" otherwise.
	Config        []byte
	CreatedAt     pgtype.Timestamptz
	LastMessageID pgtype.Text
	LastThreadID  pgtype.Text
}

// InboundMessageDedup is the flat view of a channel_inbound_message_dedup row.
// Every field is a flat column (no JSON), so this mirrors the channel row 1:1.
type InboundMessageDedup struct {
	InstallationID pgtype.UUID
	MessageID      string
	ReceivedAt     pgtype.Timestamptz
	ProcessedAt    pgtype.Timestamptz
	ClaimToken     pgtype.UUID
}

// BindingTokenRow is the flat view of a channel_binding_token row. ChannelUserID
// is the feishu open_id the token will bind once redeemed. (Named *Row to avoid
// colliding with BindingToken in binding_token.go, which is the freshly-minted
// raw-token shape returned to the caller.)
type BindingTokenRow struct {
	TokenHash      string
	WorkspaceID    pgtype.UUID
	InstallationID pgtype.UUID
	ChannelUserID  string
	ExpiresAt      pgtype.Timestamptz
	ConsumedAt     pgtype.Timestamptz
	CreatedAt      pgtype.Timestamptz
}

// OutboundCardMessage is the flat view of a channel_outbound_card_message row.
type OutboundCardMessage struct {
	ID                   pgtype.UUID
	ChatSessionID        pgtype.UUID
	TaskID               pgtype.UUID
	ChannelChatID        string
	ChannelCardMessageID string
	Status               string
	LastPatchedAt        pgtype.Timestamptz
	CreatedAt            pgtype.Timestamptz
}

// feishuInstallConfig is the JSON shape of channel_installation.config for the
// feishu channel. app_secret_encrypted is decoded by hand (see decodeSecret)
// rather than as a json []byte field, so MIME-wrapped base64 from the SQL
// backfill round-trips too. omitempty mirrors the migration's jsonb_strip_nulls.
type feishuInstallConfig struct {
	AppID              string `json:"app_id"`
	AppSecretEncrypted string `json:"app_secret_encrypted,omitempty"`
	TenantKey          string `json:"tenant_key,omitempty"`
	BotOpenID          string `json:"bot_open_id,omitempty"`
	BotUnionID         string `json:"bot_union_id,omitempty"`
	Region             string `json:"region,omitempty"`
}

// feishuBindingConfig is the JSON shape of channel_user_binding.config.
type feishuBindingConfig struct {
	UnionID string `json:"union_id,omitempty"`
}

// installationFromRow decodes a channel_installation row (flat columns + JSONB
// config) into the flat Installation domain struct.
func installationFromRow(row db.ChannelInstallation) (Installation, error) {
	var cfg feishuInstallConfig
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return Installation{}, fmt.Errorf("decode installation config: %w", err)
		}
	}
	secret, err := decodeSecret(cfg.AppSecretEncrypted)
	if err != nil {
		return Installation{}, fmt.Errorf("decode app_secret_encrypted: %w", err)
	}
	return Installation{
		ID:                 row.ID,
		WorkspaceID:        row.WorkspaceID,
		AgentID:            row.AgentID,
		AppID:              cfg.AppID,
		AppSecretEncrypted: secret,
		TenantKey:          textOrNull(cfg.TenantKey),
		BotOpenID:          cfg.BotOpenID,
		InstallerUserID:    row.InstallerUserID,
		Status:             row.Status,
		WsLeaseToken:       row.WsLeaseToken,
		WsLeaseExpiresAt:   row.WsLeaseExpiresAt,
		InstalledAt:        row.InstalledAt,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
		BotUnionID:         textOrNull(cfg.BotUnionID),
		Region:             cfg.Region,
	}, nil
}

// encodeInstallConfig builds the channel_installation.config JSONB from the
// feishu fields of an Installation. The secret is emitted as unwrapped base64.
func encodeInstallConfig(inst Installation) ([]byte, error) {
	cfg := feishuInstallConfig{
		AppID:      inst.AppID,
		TenantKey:  inst.TenantKey.String,
		BotOpenID:  inst.BotOpenID,
		BotUnionID: inst.BotUnionID.String,
		Region:     inst.Region,
	}
	if len(inst.AppSecretEncrypted) > 0 {
		cfg.AppSecretEncrypted = base64.StdEncoding.EncodeToString(inst.AppSecretEncrypted)
	}
	return json.Marshal(cfg)
}

// userBindingFromRow decodes a channel_user_binding row into UserBinding.
func userBindingFromRow(row db.ChannelUserBinding) (UserBinding, error) {
	var cfg feishuBindingConfig
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return UserBinding{}, fmt.Errorf("decode user binding config: %w", err)
		}
	}
	return UserBinding{
		ID:             row.ID,
		WorkspaceID:    row.WorkspaceID,
		MulticaUserID:  row.MulticaUserID,
		InstallationID: row.InstallationID,
		ChannelUserID:  row.ChannelUserID,
		UnionID:        textOrNull(cfg.UnionID),
		BoundAt:        row.BoundAt,
	}, nil
}

// encodeBindingConfig builds channel_user_binding.config from a UserBinding.
// Returns the JSON null-stripped (an absent union_id is "{}"), so the
// upsert's `config || jsonb_strip_nulls(EXCLUDED.config)` merge never clobbers
// a previously-captured union_id with this write.
func encodeBindingConfig(b UserBinding) ([]byte, error) {
	return json.Marshal(feishuBindingConfig{UnionID: b.UnionID.String})
}

// chatSessionBindingFromRow copies a channel_chat_session_binding row into the
// flat domain struct. Config stays raw bytes; outboundChatID decodes it.
func chatSessionBindingFromRow(row db.ChannelChatSessionBinding) ChatSessionBinding {
	return ChatSessionBinding{
		ID:             row.ID,
		ChatSessionID:  row.ChatSessionID,
		InstallationID: row.InstallationID,
		ChannelChatID:  row.ChannelChatID,
		ChatType:       row.ChatType,
		Config:         row.Config,
		CreatedAt:      row.CreatedAt,
		LastMessageID:  row.LastMessageID,
		LastThreadID:   row.LastThreadID,
	}
}

// dedupFromRow copies a channel_inbound_message_dedup row into the flat domain
// struct. No JSON: every field is a flat column.
func dedupFromRow(row db.ChannelInboundMessageDedup) InboundMessageDedup {
	return InboundMessageDedup{
		InstallationID: row.InstallationID,
		MessageID:      row.MessageID,
		ReceivedAt:     row.ReceivedAt,
		ProcessedAt:    row.ProcessedAt,
		ClaimToken:     row.ClaimToken,
	}
}

// bindingTokenFromRow copies a channel_binding_token row into the flat domain
// struct.
func bindingTokenFromRow(row db.ChannelBindingToken) BindingTokenRow {
	return BindingTokenRow{
		TokenHash:      row.TokenHash,
		WorkspaceID:    row.WorkspaceID,
		InstallationID: row.InstallationID,
		ChannelUserID:  row.ChannelUserID,
		ExpiresAt:      row.ExpiresAt,
		ConsumedAt:     row.ConsumedAt,
		CreatedAt:      row.CreatedAt,
	}
}

// outboundCardFromRow copies a channel_outbound_card_message row into the flat
// domain struct.
func outboundCardFromRow(row db.ChannelOutboundCardMessage) OutboundCardMessage {
	return OutboundCardMessage{
		ID:                   row.ID,
		ChatSessionID:        row.ChatSessionID,
		TaskID:               row.TaskID,
		ChannelChatID:        row.ChannelChatID,
		ChannelCardMessageID: row.ChannelCardMessageID,
		Status:               row.Status,
		LastPatchedAt:        row.LastPatchedAt,
		CreatedAt:            row.CreatedAt,
	}
}

// decodeSecret base64-decodes the stored app secret ciphertext. It tolerates
// the newline wrapping PostgreSQL's encode(...,'base64') inserts, so secrets
// written by the SQL backfill and by encodeInstallConfig both decode. An empty
// string yields a nil slice (an installation mid-registration before the
// secret is sealed).
func decodeSecret(s string) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	return base64.StdEncoding.DecodeString(stripWhitespace(s))
}

// stripWhitespace removes the ASCII whitespace MIME base64 wrapping introduces.
func stripWhitespace(s string) string {
	if !strings.ContainsAny(s, "\n\r \t") {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch r {
		case '\n', '\r', ' ', '\t':
			return -1
		default:
			return r
		}
	}, s)
}
