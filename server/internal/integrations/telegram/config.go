// Package telegram is the Telegram integration for the channel-agnostic
// engine. It follows the Slack BYO model: the workspace admin creates a bot
// via @BotFather, pastes its bot token into Multica, and the installation is
// keyed by the bot's numeric id (the token prefix). Inbound runs on a
// per-installation getUpdates long-polling loop (telegram_channel.go) —
// Telegram offers no WebSocket transport, and long polling is the deployment
// model that needs no public HTTPS endpoint, matching the Feishu/Slack
// "persistent per-installation connection supervised by engine.Supervisor"
// shape. Outbound agent replies stream via throttled editMessageText
// (outbound.go); verdict replies (binding prompt, offline notice) live in
// replier.go.
//
// Maintenance: this package is COMMUNITY-MAINTAINED. @leonzone contributed it
// and is its code owner — the first stop for Telegram-specific bugs and behavior
// questions, on a best-effort volunteer basis. The Multica team keeps this
// package compiling and its tests green through shared-layer refactors, but
// does not use Telegram and cannot verify behavior against the real platform;
// that part depends on the code owner. If the integration breaks in a way that
// cannot be fixed without real Telegram access and no fix lands for a few
// releases, it may be deprecated rather than left quietly broken. Changing the
// shared channel engine? Keep this adapter building — and loop in the code
// owner for anything that changes Telegram-visible behavior.
package telegram

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeTelegram is the channel discriminator for the Telegram adapter. Defined
// here (not in the channel core) so registering the platform never edits the
// core, mirroring TypeSlack.
const TypeTelegram channel.Type = "telegram"

// installConfig is the JSON shape stored in channel_installation.config for a
// Telegram installation.
//
// app_id holds the bot's numeric id as a string (parsed from the token's
// "<id>:<secret>" prefix). It fills the generic (channel_type, config->>'app_id')
// routing slot; inbound needs no per-event routing lookup (each polling loop
// serves exactly one installation), but the unique index still guarantees one
// bot maps to one agent across all workspaces.
//
// bot_token_encrypted is base64-encoded secretbox ciphertext, never plaintext,
// mirroring Slack's bot_token_encrypted and Feishu's app_secret_encrypted.
type installConfig struct {
	AppID             string `json:"app_id"`
	BotUsername       string `json:"bot_username,omitempty"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
}

// credentials is the decoded, decrypted form the transports run on. The
// installation identity (workspace / agent / installer) is deliberately
// absent: it is resolved per message by the Router, as in Feishu and Slack.
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
// stored bot token. It is the single place the Telegram config JSON is
// interpreted.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("telegram: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode telegram installation config: %w", err)
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
// surface on the management API (the encrypted bot token is never included).
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
// wrapping) and runs it through the injected Decrypter; mirrors the Slack
// helper of the same name.
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

// parseBotID extracts the bot's numeric id from a bot token. BotFather tokens
// are "<numeric id>:<secret>"; the id part is the stable per-bot identity used
// as the installation routing key (config->>'app_id').
func parseBotID(token string) (string, error) {
	id, secret, ok := strings.Cut(strings.TrimSpace(token), ":")
	if !ok || id == "" || secret == "" {
		return "", ErrInvalidBotToken
	}
	for _, r := range id {
		if r < '0' || r > '9' {
			return "", ErrInvalidBotToken
		}
	}
	return id, nil
}
