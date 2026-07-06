// Package slack is the Slack integration for the channel-agnostic engine. It
// uses the bring-your-own-app (BYO) model (MUL-3666): each agent's Slack app is
// created and installed by the workspace admin, who pastes its bot token (xoxb-)
// and app-level token (xapp-) into Multica. Each channel_installation therefore
// carries its OWN app-level token and gets its OWN Socket Mode connection,
// supervised per-installation by the engine like Feishu (slack_channel.go) — so
// several agents can each have a distinct bot identity in one Slack workspace.
// Installations are keyed and routed by the real Slack app id
// (config->>'app_id' == the inbound event's api_app_id). The inbound translation
// (Events API payload -> channel.InboundMessage) lives in inbound.go; the
// outbound reply path (chat.postMessage with Markdown->mrkdwn + threading) lives
// in channel.go. The design references the proven Slack adapter in Nous
// Research's Hermes Agent.
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
// app_id holds the REAL Slack app id (parsed from the xapp- token). It is the
// per-installation routing key: the generic GetChannelInstallationByAppID query
// (config->>'app_id') and the (channel_type, app_id) unique index map an inbound
// event's api_app_id to its installation, so several apps — several agents — in
// one Slack workspace stay distinct. team_id is kept for display only.
//
// bot_token_encrypted (xoxb-, outbound Web API: chat.postMessage) and
// app_token_encrypted (xapp-, this installation's own Socket Mode connection)
// are both stored as base64-encoded secretbox ciphertext, never plaintext
// (mirroring Feishu's app_secret_encrypted). Both are pasted by the admin at
// BYO install time.
type installConfig struct {
	AppID             string `json:"app_id"`
	TeamID            string `json:"team_id,omitempty"`
	BotUserID         string `json:"bot_user_id,omitempty"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
	AppTokenEncrypted string `json:"app_token_encrypted,omitempty"`
}

// credentials is the decoded, decrypted form the outbound sender runs on. The
// installation IDENTITY (workspace / agent / installer) is deliberately absent:
// it is resolved per message by the Router's InstallationResolver, exactly as
// the Feishu adapter does.
type credentials struct {
	TeamID    string
	BotUserID string
	BotToken  string
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
	teamID := cfg.TeamID
	if teamID == "" {
		teamID = cfg.AppID
	}
	return credentials{
		TeamID:    teamID,
		BotUserID: cfg.BotUserID,
		BotToken:  botToken,
	}, nil
}

// PublicConfig is the non-secret subset of an installation config, safe to
// surface on the management API (the encrypted bot token is never included).
type PublicConfig struct {
	AppID     string
	TeamID    string
	BotUserID string
}

// DecodePublicConfig extracts the display-safe fields from a stored config blob.
// A decode miss yields a zero-value PublicConfig rather than an error: the
// management list should still render the row's identity columns.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	teamID := cfg.TeamID
	if teamID == "" {
		teamID = cfg.AppID
	}
	return PublicConfig{AppID: cfg.AppID, TeamID: teamID, BotUserID: cfg.BotUserID}
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
