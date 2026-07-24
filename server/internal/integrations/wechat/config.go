package wechat

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// installConfig is the JSON shape stored in channel_installation.config for a
// WeChat ClawBot installation. The cross-platform columns stay flat; everything
// WeChat-specific lives in this opaque blob (the documented config boundary).
//
// app_id holds the iLink bot id (e.g. "xxxxxx@im.bot"). It is the
// per-installation routing key: the generic GetChannelInstallationByAppID query
// (config->>'app_id') and the (channel_type, app_id) unique index map an inbound
// event's to_user_id (the bot id the message was addressed to) to its
// installation, so several WeChat bots — several agents — stay distinct.
//
// bot_token_encrypted is the iLink bot_token returned by the QR-login status
// poll, stored as base64-encoded secretbox ciphertext (never plaintext,
// mirroring Feishu's app_secret_encrypted and Slack's bot_token_encrypted).
// base_url is the per-account API base the status poll returns (the iLink
// backend shards accounts across hosts), so every getupdates/sendmessage call
// must use the installation's own base_url. ilink_user_id is the human-readable
// id of the WeChat account that scanned the QR code (display only).
type installConfig struct {
	AppID             string `json:"app_id"`
	BotTokenEncrypted string `json:"bot_token_encrypted"`
	BaseURL           string `json:"base_url,omitempty"`
	IlinkUserID       string `json:"ilink_user_id,omitempty"`
}

// credentials is the decoded, decrypted form the outbound sender runs on. The
// installation IDENTITY (workspace / agent / installer) is deliberately absent:
// it is resolved per message by the Router's InstallationResolver, exactly as
// the Feishu and Slack adapters do.
type credentials struct {
	AppID   string
	BotToken string
	BaseURL string
}

// Decrypter turns stored ciphertext into plaintext. The wiring injects a
// secretbox-backed implementation; tests inject an identity decrypter (or nil,
// which treats the stored bytes as plaintext).
type Decrypter func(ciphertext []byte) (plaintext []byte, err error)

// decodeCredentials parses the per-installation config blob and decrypts the
// stored bot token. It is the single place the WeChat config JSON is
// interpreted.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	if len(raw) == 0 {
		return credentials{}, errors.New("wechat: empty installation config")
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil {
		return credentials{}, fmt.Errorf("decode wechat installation config: %w", err)
	}
	botToken, err := decryptToken(cfg.BotTokenEncrypted, decrypt)
	if err != nil {
		return credentials{}, fmt.Errorf("decrypt bot token: %w", err)
	}
	return credentials{
		AppID:    cfg.AppID,
		BotToken: botToken,
		BaseURL:  cfg.BaseURL,
	}, nil
}

// PublicConfig is the non-secret subset of an installation config, safe to
// surface on the management API (the encrypted bot token is never included).
type PublicConfig struct {
	AppID       string
	IlinkUserID string
}

// DecodePublicConfig extracts the display-safe fields from a stored config blob.
// A decode miss yields a zero-value PublicConfig rather than an error: the
// management list should still render the row's identity columns.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{AppID: cfg.AppID, IlinkUserID: cfg.IlinkUserID}
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

// encodeBase64 returns the base64 encoding of ciphertext, the form stored in
// installConfig.BotTokenEncrypted (the inverse of decryptToken's base64 decode).
// Used by the install path after secretbox.Seal returns raw ciphertext bytes.
func encodeBase64(ciphertext []byte) string {
	return base64.StdEncoding.EncodeToString(ciphertext)
}

// jsonMarshalConfig marshals an installConfig, failing loud on a marshalling
// error rather than persisting a half-formed blob.
func jsonMarshalConfig(cfg installConfig) ([]byte, error) {
	return json.Marshal(cfg)
}
