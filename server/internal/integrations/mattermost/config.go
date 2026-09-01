// Package mattermost is the Mattermost integration for the channel-agnostic
// engine. It follows the Slack/Telegram bring-your-own-bot model with one
// self-hosting twist: because a Mattermost deployment is the operator's own
// server, an installation carries a server URL as well as a credential. The
// workspace admin creates a bot account in the Mattermost System Console,
// generates an access token, and pastes the server URL plus that token into
// Multica.
//
// Inbound runs on a per-installation WebSocket connection
// (mattermost_channel.go) supervised by engine.Supervisor, matching Feishu's
// long-conn and Slack's Socket Mode. Outbound agent replies are delivered as a
// single post on EventChatDone (outbound.go); verdict replies (binding prompt,
// offline notice) live in replier.go.
//
// Maintenance: this package is COMMUNITY-MAINTAINED. Its maintainers, the
// support boundary and the retirement rule are published at
// https://multica.ai/docs/community-maintained
// (apps/docs/content/docs/community-maintained.mdx, four locales). That page
// is the single source of truth — record ownership changes there, not here.
// Changing the shared channel engine? Keep this adapter building, and loop in
// its maintainers for anything that changes Mattermost-visible behavior.
package mattermost

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeMattermost is the channel discriminator for the Mattermost adapter.
// Defined here (not in the channel core) so registering the platform never
// edits the core, mirroring TypeSlack and TypeTelegram.
const TypeMattermost channel.Type = "mattermost"

// installConfig is the JSON shape stored in channel_installation.config for a
// Mattermost installation.
//
// app_id fills the generic (channel_type, config->>'app_id') routing slot. A
// Mattermost bot user id is unique only WITHIN one server, so the key composes
// the canonical server authority with the bot user id — see installationKey.
// The unique index then still means what it says: one bot on one server maps
// to one agent across all Multica workspaces.
//
// access_token_encrypted is base64-encoded secretbox ciphertext, never
// plaintext, mirroring Slack's bot_token_encrypted and Telegram's
// bot_token_encrypted.
type installConfig struct {
	AppID                string `json:"app_id"`
	ServerURL            string `json:"server_url"`
	BotUserID            string `json:"bot_user_id"`
	BotUsername          string `json:"bot_username,omitempty"`
	AccessTokenEncrypted string `json:"access_token_encrypted"`
}

// credentials is the decoded, decrypted form the transports run on. The
// installation identity (workspace / agent / installer) is deliberately
// absent: it is resolved per message by the Router, as in Feishu and Slack.
type credentials struct {
	ServerURL   string
	BotUserID   string
	BotUsername string
	AccessToken string
}

// Decrypter turns stored ciphertext into plaintext. The wiring injects a
// secretbox-backed implementation; tests inject nil (stored bytes are treated
// as plaintext).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

var (
	// ErrInvalidServerURL is returned when the pasted server URL is not an
	// absolute http(s) URL — mapped to 400 by the handler.
	ErrInvalidServerURL = errors.New("mattermost: server URL must be an absolute http(s) URL, e.g. https://mattermost.example.com")
	// ErrInvalidAccessToken is returned when the pasted token is empty or
	// carries bytes that cannot appear in an HTTP header value.
	ErrInvalidAccessToken = errors.New("mattermost: access token is empty or contains invalid characters")
)

// canonicalServerURL normalizes a pasted server URL into the form stored and
// dialed: lower-case scheme and host, no trailing slash, no query or fragment.
// A sub-path is preserved, because Mattermost can be mounted under one (behind
// a reverse proxy at /mattermost, say) and every API path is relative to it.
func canonicalServerURL(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", ErrInvalidServerURL
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidServerURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", ErrInvalidServerURL
	}
	if u.Host == "" {
		return "", ErrInvalidServerURL
	}
	return scheme + "://" + strings.ToLower(u.Host) + strings.TrimRight(u.Path, "/"), nil
}

// installationKey builds the (channel_type, app_id) routing key. Bot user ids
// repeat across servers, so the key is "<authority><path>:<bot user id>" —
// stable for a given deployment, and distinct for the same bot id on two
// different Mattermost servers.
func installationKey(canonicalURL, botUserID string) string {
	authority := canonicalURL
	if _, rest, ok := strings.Cut(canonicalURL, "://"); ok {
		authority = rest
	}
	return authority + ":" + botUserID
}

// websocketURL derives the event-stream URL from the canonical server URL.
func websocketURL(canonicalURL string) string {
	switch {
	case strings.HasPrefix(canonicalURL, "https://"):
		return "wss://" + strings.TrimPrefix(canonicalURL, "https://") + websocketPath
	case strings.HasPrefix(canonicalURL, "http://"):
		return "ws://" + strings.TrimPrefix(canonicalURL, "http://") + websocketPath
	default:
		return canonicalURL + websocketPath
	}
}

// decodeCredentials parses the per-installation config blob and decrypts the
// stored access token. It is the single place the Mattermost config JSON is
// interpreted.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("mattermost: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode mattermost installation config: %w", err)
	}
	token, err := decryptToken(cfg.AccessTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt access token: %w", err)
	}
	return credentials{
		ServerURL:   cfg.ServerURL,
		BotUserID:   cfg.BotUserID,
		BotUsername: cfg.BotUsername,
		AccessToken: token,
	}, nil
}

// PublicConfig is the non-secret subset of an installation config, safe to
// surface on the management API (the encrypted token is never included).
type PublicConfig struct {
	ServerURL   string
	BotUserID   string
	BotUsername string
}

// DecodePublicConfig extracts the display-safe fields from a stored config
// blob. A decode miss yields a zero PublicConfig rather than an error so the
// management list still renders the row.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{
		ServerURL:   cfg.ServerURL,
		BotUserID:   cfg.BotUserID,
		BotUsername: cfg.BotUsername,
	}
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

// validateAccessToken rejects a token that could not be sent as an HTTP header
// value. Mattermost tokens are 26-character alphanumerics; the check is a
// header-injection guard rather than a format assertion, so a future token
// shape keeps working.
func validateAccessToken(token string) (string, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return "", ErrInvalidAccessToken
	}
	for i := 0; i < len(trimmed); i++ {
		if c := trimmed[i]; c < 0x20 || c == 0x7f {
			return "", ErrInvalidAccessToken
		}
	}
	return trimmed, nil
}
