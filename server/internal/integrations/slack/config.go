// Package slack is the Slack implementation of channel.Channel — the second
// adapter driven by the channel-agnostic engine (MUL-3516), proving the
// MUL-3506 thesis that adding an IM is "implement Channel + register" with no
// engine, core, or channel_* schema change. It mirrors the Feishu reference
// adapter (server/internal/integrations/lark/feishu_channel.go): Connect runs
// the platform receive loop (here Slack Socket Mode, an outbound WebSocket
// long-conn that needs no public inbound URL) and hands every decoded event to
// the engine's shared inbound handler as a normalized channel.InboundMessage;
// Send posts a text reply via chat.postMessage. The design references the
// proven Slack adapter in Nous Research's Hermes Agent.
package slack

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// installConfig is the JSON shape stored in channel_installation.config for a
// Slack installation. The cross-platform columns stay flat; everything
// Slack-specific lives in this opaque blob (the documented config boundary).
//
// app_id holds the Slack team_id — the per-installation routing key — so the
// generic GetChannelInstallationByAppID query (which reads config->>'app_id')
// and the (channel_type, config->>'app_id') unique index route Slack inbound
// events with NO new query and NO schema change. team_id is also kept as its
// own field for readability; the two carry the same value.
//
// Tokens are stored as base64-encoded secretbox ciphertext (never plaintext),
// mirroring Feishu's app_secret_encrypted. The bot token (xoxb-…) authorizes
// Web API calls (chat.postMessage); the app-level token (xapp-…) authorizes the
// Socket Mode connection.
type installConfig struct {
	AppID             string `json:"app_id"`
	TeamID            string `json:"team_id,omitempty"`
	BotUserID         string `json:"bot_user_id,omitempty"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
	AppTokenEncrypted string `json:"app_token_encrypted"`
}

// credentials is the decoded, decrypted form the adapter runs on. The
// installation IDENTITY (workspace / agent / installer) is deliberately absent:
// it is resolved per message by the Router's InstallationResolver, exactly as
// the Feishu adapter does.
type credentials struct {
	TeamID    string
	BotUserID string
	BotToken  string
	AppToken  string
}

// Decrypter turns stored ciphertext into plaintext. The wiring injects a
// secretbox-backed implementation; tests inject an identity decrypter (or nil,
// which treats the stored bytes as plaintext).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

// decodeCredentials parses the per-installation config blob and decrypts the
// stored tokens. It is the single place the Slack config JSON is interpreted.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("slack: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode slack installation config: %w", err)
	}
	botToken, err := decryptToken(cfg.BotTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt bot token: %w", err)
	}
	appToken, err := decryptToken(cfg.AppTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt app token: %w", err)
	}
	teamID := cfg.TeamID
	if teamID == "" {
		teamID = cfg.AppID
	}
	return credentials{
		TeamID:    teamID,
		BotUserID: cfg.BotUserID,
		BotToken:  botToken,
		AppToken:  appToken,
	}, nil
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
