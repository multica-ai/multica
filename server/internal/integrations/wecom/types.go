// Package wecom is the WeCom (企业微信) smart-bot ("智能机器人" / aibot)
// adapter for the channel-agnostic inbound engine (MUL-3620). It plugs into the
// same engine.Router / ResolverSet as Feishu and Slack.
//
// Unlike the internal customer-service ("内部客服号") flow which is HTTP-callback
// based, the smart-bot flow is a client-initiated WebSocket long connection
// against wss://openws.work.weixin.qq.com. Each installation carries a
// (bot_id, secret) pair; after the WS handshake the client subscribes with
// aibot_subscribe and thereafter receives aibot_msg_callback events and sends
// aibot_send_msg / aibot_respond_msg / aibot_upload_media_* over the same
// socket. No public callback URL is required.
//
// One installation = one bot = one WebSocket. WeCom allows only one active
// connection per bot; a second connection kicks the first with a
// disconnected_event. That single-active-connection guarantee lines up with
// engine.Supervisor's WS lease, so the multi-replica invariant (at most one
// active connection per installation across processes) already holds without
// wecom-specific code.
//
// Maintenance: this package is COMMUNITY-MAINTAINED. Its maintainers, the
// support boundary and the retirement rule are published at
// https://multica.ai/docs/community-maintained
// (apps/docs/content/docs/community-maintained.mdx, four locales). That page
// is the single source of truth — record ownership changes there, not here.
// Changing the shared channel engine? Keep this adapter building, and loop in
// its maintainers for anything that changes WeCom-visible behavior.
//
// Inbound handles text, the transcript WeCom returns for a voice note,
// photos, files, videos and 图文混排 (media_ingest.go downloads and decrypts
// what a callback points at); a kind it cannot read still gets a short
// receipt.
//
// Outbound file delivery cannot report back to the agent that produced the
// file. `multica attachment upload` returns once the object is in storage and
// bound to the reply, while the send into the room runs on EventChatDone —
// after the run has ended. A delivery that is shed, refused by WeCom, or lost
// with the socket is therefore told to the person in the chat
// (outbound_media.go) and never to the agent, which has already exited.
// Routing that outcome back into a later turn is its own piece of work.
//
// Known limit, deliberate: outbound delivery requires a SINGLE backend
// replica, because the only send path is the in-process WebSocket in
// sendersRegistry while EventChatDone dispatches on the in-process
// events.Bus. See SELF_HOSTING.md.
package wecom

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TypeWecom is the channel discriminator for the WeCom smart-bot adapter.
// It is defined here alongside the wecom-specific types (rather than in
// package channel) so a build that excludes wecom does not force a package
// channel edit; slack follows the same pattern with TypeSlack.
const TypeWecom channel.Type = "wecom"

// channelTypeWecom is the string form persisted in channel_installation.
const channelTypeWecom = string(TypeWecom)

// InstallationStatus mirrors the channel_installation.status column values.
type InstallationStatus string

const (
	InstallationActive  InstallationStatus = "active"
	InstallationRevoked InstallationStatus = "revoked"
)

// Installation is the decoded, in-memory view of a WeCom smart-bot
// channel_installation row. SecretEncrypted is a secretbox-sealed blob and
// never plaintext; callers who need the plaintext go through the Credentials
// resolver.
type Installation struct {
	ID              pgtype.UUID
	WorkspaceID     pgtype.UUID
	AgentID         pgtype.UUID
	InstallerUserID pgtype.UUID
	Status          InstallationStatus

	// BotID is the smart-bot identifier the WeCom admin console
	// assigns at bot creation. It is BOTH the auth identity presented in
	// the aibot_subscribe frame AND the routing key we persist as
	// config->>'app_id' so GetChannelInstallationByAppID resolves an
	// inbound event to its installation.
	BotID string

	// SecretEncrypted is the sealed long-connection secret. This is
	// distinct from the token/EncodingAESKey used by callback-mode bots
	// (which we do not use). Rotated via re-install.
	SecretEncrypted []byte

	// BotDisplayName is what the bot is called in a chat. A WeCom group
	// mention arrives as literal text — "@Multica Bot /new 重新分析" — with no
	// structured mention list anywhere in the payload, so recognising where
	// the mention ends is the only way a name containing a space does not
	// swallow the command after it.
	//
	// Optional. Empty falls back to the whitespace heuristic in
	// stripLeadingMentions, which is correct for a single-word name and is
	// what every existing installation gets.
	BotDisplayName string
}

// InstallationCredentials is the plaintext-bearing view the WebSocket
// subscribe frame needs. It is minted per-connect by the credentials resolver
// so a plaintext secret never lives on the durable Installation itself.
type InstallationCredentials struct {
	BotID  string
	Secret string
}

// installConfig is the on-disk (JSONB) shape of channel_installation.config
// for wecom smart-bot rows. `app_id == bot_id` so the shared
// idx_channel_installation_type_appid index and GetChannelInstallationByAppID
// query stay generic.
type installConfig struct {
	AppID           string `json:"app_id"`
	BotID           string `json:"bot_id"`
	SecretEncrypted []byte `json:"secret_encrypted"`

	// BotDisplayName is the bot's name as it appears in a chat, used only to
	// recognise its own @-mention (ws_frame.go). Optional and absent on every
	// existing row; absent means the whitespace heuristic, which is what the
	// adapter did before this field existed.
	BotDisplayName string `json:"bot_display_name,omitempty"`
}

// encodeInstallConfig marshals an Installation's config-bearing fields into
// the JSONB blob stored in channel_installation.config.
func encodeInstallConfig(inst Installation) ([]byte, error) {
	if inst.BotID == "" {
		return nil, errors.New("wecom: bot_id is required")
	}
	return json.Marshal(installConfig{
		AppID:           inst.BotID,
		BotID:           inst.BotID,
		SecretEncrypted: inst.SecretEncrypted,
		BotDisplayName:  inst.BotDisplayName,
	})
}

// installationFromRow hydrates an Installation from a channel_installation
// row. The row's ChannelType is trusted (the callers already scope queries by
// channel_type = 'wecom'), so we don't re-check it here.
func installationFromRow(row db.ChannelInstallation) (Installation, error) {
	var cfg installConfig
	if len(row.Config) > 0 {
		if err := json.Unmarshal(row.Config, &cfg); err != nil {
			return Installation{}, fmt.Errorf("wecom: decode installation config: %w", err)
		}
	}
	return Installation{
		ID:              row.ID,
		WorkspaceID:     row.WorkspaceID,
		AgentID:         row.AgentID,
		InstallerUserID: row.InstallerUserID,
		Status:          InstallationStatus(row.Status),
		BotID:           cfg.BotID,
		SecretEncrypted: cfg.SecretEncrypted,
		BotDisplayName:  cfg.BotDisplayName,
	}, nil
}
