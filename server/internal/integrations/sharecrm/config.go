// Package sharecrm is the ShareCRM (纷享销客) bot integration for Multica's
// channel-agnostic engine. It uses the bring-your-own-app (BYO) model: a
// workspace admin pastes an open-platform App ID + App Secret (and optional
// API base URL for private clouds). Each channel_installation gets its OWN
// SSE long connection, supervised per-installation like DingTalk Stream and
// Slack Socket Mode.
//
// Protocol (Gateway v1.3):
//
//	POST /im-gateway/auth/token          → accessToken
//	GET  /im-gateway/bot/events?token=…  → SSE inbound (connected/message/reset)
//	POST /im-gateway/qixin/message/send  → outbound text (requires active SSE)
//
// One appId may hold only one active SSE connection; Multica's Supervisor lease
// ensures a single replica owns each installation's connection.
//
// Maintenance: this package is COMMUNITY-MAINTAINED. Its maintainers, the
// support boundary and the retirement rule are published at
// https://multica.ai/docs/community-maintained
// (apps/docs/content/docs/community-maintained.mdx, four locales). That page
// is the single source of truth — record ownership changes there, not here.
// Changing the shared channel engine? Keep this adapter building, and loop in
// its maintainers for anything that changes ShareCRM-visible behavior.
package sharecrm

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeShareCRM is the channel discriminator for the ShareCRM adapter.
const TypeShareCRM channel.Type = "sharecrm"

// DefaultGatewayBaseURL is the public ShareCRM IM Gateway host.
const DefaultGatewayBaseURL = "https://open.fxiaoke.com"

// GatewayProtocolVersion is sent on SSE connect so the gateway emits the v1.2+
// structured message shape and history_messages (v1.3).
const GatewayProtocolVersion = "1.3.0"

// installConfig is the JSON shape stored in channel_installation.config.
//
// app_id holds the Gateway appId — the routing key (config->>'app_id') used by
// GetChannelInstallationByAppID and the (channel_type, app_id) unique index.
// app_secret_encrypted is base64-encoded secretbox ciphertext.
// gateway_base_url is optional; empty means DefaultGatewayBaseURL.
type installConfig struct {
	AppID              string `json:"app_id"`
	AppSecretEncrypted string `json:"app_secret_encrypted"`
	GatewayBaseURL     string `json:"gateway_base_url,omitempty"`
}

// credentials is the decrypted form used by outbound send and SSE connect.
type credentials struct {
	AppID          string
	AppSecret      string
	GatewayBaseURL string
}

// Decrypter turns stored ciphertext into plaintext.
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

func (c installConfig) gatewayBase() string {
	base := strings.TrimSpace(c.GatewayBaseURL)
	if base == "" {
		return DefaultGatewayBaseURL
	}
	return strings.TrimRight(base, "/")
}

func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("sharecrm: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode sharecrm installation config: %w", err)
	}
	appSecret, err := decryptToken(cfg.AppSecretEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt app secret: %w", err)
	}
	return credentials{
		AppID:          cfg.AppID,
		AppSecret:      appSecret,
		GatewayBaseURL: cfg.gatewayBase(),
	}, nil
}

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
