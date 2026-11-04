// Package discord is the Discord integration for the channel-agnostic engine.
// It follows the Slack/Telegram BYO model: a workspace admin creates a Discord
// application, adds a Bot user, and pastes the bot token into Multica. Each
// installation is keyed by the bot's own user id (returned by GET /users/@me at
// install time), which is also the @-mention identity inbound translation
// strips.
//
// Inbound runs on a per-installation Gateway WebSocket connection
// (gateway.go), supervised by engine.Supervisor like Feishu, Slack and
// Telegram. Outbound replies post over the REST API (channel.go); verdict
// replies (binding prompt, offline notice) live in replier.go, and the
// EventChatDone subscriber in outbound.go delivers agent answers.
//
// Attachments are not downloaded: a message's files reach the agent as text
// markers only. ponytail: add a MediaResolver (Discord CDN download + object
// storage) when inbound attachments are actually needed.
package discord

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeDiscord is the channel discriminator for the Discord adapter. Defined
// here (not in the channel core) so registering the platform never edits the
// core, mirroring TypeSlack and TypeTelegram.
const TypeDiscord channel.Type = "discord"

// installConfig is the JSON shape stored in channel_installation.config.
//
// app_id holds the bot's Discord user id (snowflake). It fills the generic
// (channel_type, config->>'app_id') routing slot: each Gateway connection
// serves exactly one bot, and the unique index guarantees one bot maps to one
// agent across all workspaces.
//
// bot_token_encrypted is base64-encoded secretbox ciphertext, never plaintext,
// mirroring the other BYO adapters.
type installConfig struct {
	AppID             string `json:"app_id"`
	BotUsername       string `json:"bot_username,omitempty"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
}

// credentials is the decoded, decrypted form the transports run on. The
// installation identity (workspace / agent / installer) is deliberately
// absent: it is resolved per message by the Router.
type credentials struct {
	BotID       string
	BotUsername string
	BotToken    string
}

// Decrypter turns stored ciphertext into plaintext. The wiring injects a
// secretbox-backed implementation; tests inject nil (stored bytes are treated
// as plaintext).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

// decodeCredentials parses the per-installation config blob and decrypts the
// stored bot token. It is the single place the Discord config JSON is
// interpreted.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("discord: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode discord installation config: %w", err)
	}
	token, err := decryptToken(cfg.BotTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt bot token: %w", err)
	}
	return credentials{
		BotID:       cfg.AppID,
		BotUsername: cfg.BotUsername,
		BotToken:    token,
	}, nil
}

// PublicConfig is the non-secret subset of an installation config, safe to
// surface on the management API.
type PublicConfig struct {
	BotID       string
	BotUsername string
}

// DecodePublicConfig extracts the display-safe fields from a stored config
// blob. A decode miss yields a zero PublicConfig rather than an error so the
// management list still renders the row.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{BotID: cfg.AppID, BotUsername: cfg.BotUsername}
}

// decryptToken base64-decodes the stored ciphertext (tolerating MIME newline
// wrapping) and runs it through the injected Decrypter; mirrors the Slack and
// Telegram helpers of the same name.
func decryptToken(enc string, decrypt Decrypter) (string, error) {
	if enc == "" {
		return "", nil
	}
	ciphertext, err := base64.StdEncoding.DecodeString(stripWhitespace(enc))
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if decrypt == nil {
		return string(ciphertext), nil
	}
	plaintext, err := decrypt(ciphertext)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
