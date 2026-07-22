// Package dingtalk is the DingTalk integration for the channel-agnostic engine.
// It uses the bring-your-own-app (BYO) model: a workspace admin creates their
// own DingTalk Stream-mode robot and pastes its AppKey (client id) and AppSecret
// (client secret) into Multica. Each channel_installation carries its OWN
// AppSecret and gets its OWN Stream-mode connection, supervised per-installation
// by the engine like Feishu and Slack (dingtalk_channel.go) — so several agents
// can each have a distinct bot identity in one DingTalk organization.
//
// Each installation's Stream connection only ever delivers events for its own
// robot, so the per-installation connection stamps its AppKey into the inbound
// envelope and the resolver routes on it (config->>'app_id'). Unlike Slack's
// static bot token, DingTalk outbound needs a short-lived access_token minted
// from AppKey/AppSecret, so the outbound path caches it like Feishu's
// tenant_access_token (token.go).
package dingtalk

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeDingTalk is the channel discriminator for the DingTalk adapter. It is
// defined here (not in the channel core package) on purpose: registering a new
// platform must not require editing the core, so the Type value lives with its
// adapter (mirroring slack.TypeSlack).
const TypeDingTalk channel.Type = "dingtalk"

// installConfig is the JSON shape stored in channel_installation.config for a
// DingTalk installation. The cross-platform columns stay flat; everything
// DingTalk-specific lives in this opaque blob (the documented config boundary).
//
// app_id holds the AppKey, which for a Stream-mode robot equals the inbound
// event's robotCode. It is the per-installation routing key: the generic
// GetChannelInstallationByAppID query (config->>'app_id') and the
// (channel_type, app_id) unique index map an inbound event's robotCode to its
// installation, so several robots — several agents — in one DingTalk org stay
// distinct.
//
// robot_code is kept explicit for the outbound send APIs (oToMessages.batchSend
// / groupMessages.send both require it); it equals app_id but is stored
// separately so the outbound path never has to assume the equivalence.
//
// app_secret_encrypted is base64-encoded secretbox ciphertext, never plaintext
// (mirroring Feishu's app_secret_encrypted and Slack's bot_token_encrypted).
// The AppKey itself is not a secret and lives in app_id in the clear, exactly
// like Feishu stores app_id in the clear next to app_secret_encrypted.
type installConfig struct {
	AppID              string `json:"app_id"`
	RobotCode          string `json:"robot_code,omitempty"`
	AppSecretEncrypted string `json:"app_secret_encrypted"`
}

// robotCodeOrAppID returns the explicit robot_code, falling back to app_id for
// the Stream-mode robot where the two are equal (older configs stored only
// app_id).
func (c installConfig) robotCodeOrAppID() string {
	if c.RobotCode != "" {
		return c.RobotCode
	}
	return c.AppID
}

// credentials is the decoded, decrypted form the outbound sender and the
// access-token cache run on. The installation IDENTITY (workspace / agent /
// installer) is deliberately absent: it is resolved per message by the Router's
// InstallationResolver, exactly as the Feishu and Slack adapters do.
type credentials struct {
	AppKey    string
	RobotCode string
	AppSecret string
}

// Decrypter turns stored ciphertext into plaintext. The wiring injects a
// secretbox-backed implementation; tests inject an identity decrypter (or nil,
// which treats the stored bytes as plaintext).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

// decodeCredentials parses the per-installation config blob and decrypts the
// stored AppSecret. It is the single place the DingTalk config JSON is
// interpreted for the outbound/token paths.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("dingtalk: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode dingtalk installation config: %w", err)
	}
	appSecret, err := decryptToken(cfg.AppSecretEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt app secret: %w", err)
	}
	return credentials{
		AppKey:    cfg.AppID,
		RobotCode: cfg.robotCodeOrAppID(),
		AppSecret: appSecret,
	}, nil
}

// PublicConfig is the non-secret subset of an installation config, safe to
// surface on the management API (the encrypted AppSecret is never included).
type PublicConfig struct {
	AppID     string
	RobotCode string
}

// DecodePublicConfig extracts the display-safe fields from a stored config blob.
// A decode miss yields a zero-value PublicConfig rather than an error: the
// management list should still render the row's identity columns.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{AppID: cfg.AppID, RobotCode: cfg.robotCodeOrAppID()}
}

// decryptToken base64-decodes the stored ciphertext (tolerating the MIME
// newline wrapping PostgreSQL's encode(...,'base64') emits) and runs it through
// the injected Decrypter. An empty stored value decodes to an empty secret; a
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
