// Package telegram is the Telegram integration for the channel-agnostic engine.
// Each workspace admin creates a Telegram bot via BotFather and pastes its bot
// token into Multica. Each channel_installation carries its OWN bot token and
// identity. Installations are keyed and routed by the numeric bot id (the
// leading part of the token before the `:`, stored at config->>'app_id').
package telegram

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// installConfig is the JSON shape stored in channel_installation.config for a
// Telegram installation. The cross-platform columns stay flat; everything
// Telegram-specific lives in this opaque blob (the documented config boundary).
//
// app_id holds the numeric bot id (parsed from the bot token prefix).
// It is the per-installation routing key: the generic GetChannelInstallationByAppID
// query (config->>'app_id') maps an inbound webhook's bot id to its installation.
//
// bot_token_encrypted is the bot token from BotFather, stored as base64-encoded
// secretbox ciphertext, never plaintext. webhook_secret is a verification token
// for the Telegram webhook (stored in plaintext, as it is not a credential).
type installConfig struct {
	AppID             string `json:"app_id"`
	BotUsername       string `json:"bot_username,omitempty"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
	WebhookSecret     string `json:"webhook_secret,omitempty"`
}

// credentials is the decoded, decrypted form the outbound sender runs on.
type credentials struct {
	BotID       string
	BotUsername string
	BotToken    string
}

// Decrypter turns stored ciphertext into plaintext. The wiring injects a
// secretbox-backed implementation; tests inject an identity decrypter (or nil,
// which treats the stored bytes as plaintext).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

// ErrInvalidBotToken is returned when a bot token does not match the expected format.
var ErrInvalidBotToken = errors.New("telegram: bot token must be <bot_id>:<secret>")

// decodeCredentials parses the per-installation config blob and decrypts the
// stored tokens. It is the single place the Telegram config JSON is interpreted.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("telegram: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode telegram installation config: %w", err)
	}
	botToken, err := decryptToken(cfg.BotTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt bot token: %w", err)
	}
	return credentials{
		BotID:       cfg.AppID,
		BotUsername: cfg.BotUsername,
		BotToken:    botToken,
	}, nil
}

// PublicConfig is the non-secret subset of an installation config, safe to
// surface on the management API (the encrypted bot token is never included).
type PublicConfig struct {
	AppID       string
	BotUsername string
}

// DecodePublicConfig extracts the display-safe fields from a stored config blob.
// A decode miss yields a zero-value PublicConfig rather than an error: the
// management list should still render the row's identity columns.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{AppID: cfg.AppID, BotUsername: cfg.BotUsername}
}

// WebhookSecret extracts the per-installation webhook verification token
// (installConfig.WebhookSecret) from a stored config blob. It is stored in
// plaintext (it is not a credential — see the installConfig doc comment) so
// this is a plain decode, not a decrypt. A decode miss yields "", which the
// public webhook handler treats as "never matches" rather than panicking.
func WebhookSecret(raw json.RawMessage) string {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return cfg.WebhookSecret
}

// decryptToken base64-decodes the stored ciphertext (tolerating the MIME
// newline wrapping PostgreSQL's encode(...,'base64') emits) and runs it through
// the injected Decrypter. An empty stored value decodes to an empty token; a
// nil Decrypter treats the decoded bytes as plaintext (test convenience).
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

// stripWhitespace removes ASCII whitespace so a MIME-wrapped base64 string
// (newlines every 64 chars) and an unwrapped one decode identically.
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

// parseTelegramBotID extracts the numeric bot id from a bot token. Telegram
// tokens are "<bot_id>:<auth_secret>"; the bot id is the per-bot routing key
// stored at config->>'app_id' (mirrors slack's parseSlackAppID). It is stable
// for the life of the bot and is what the inbound webhook path routes on.
func parseTelegramBotID(botToken string) (string, error) {
	id, _, ok := strings.Cut(strings.TrimSpace(botToken), ":")
	if !ok || id == "" {
		return "", ErrInvalidBotToken
	}
	return id, nil
}
