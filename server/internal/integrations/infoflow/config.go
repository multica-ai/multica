// Package infoflow is the InfoFlow (如流) integration for the channel-agnostic
// engine. It uses the bring-your-own-app (BYO) model: an agent owner or
// workspace admin creates an InfoFlow robot and pastes its AppKey / AppSecret
// into Multica. Each channel_installation carries its OWN AppSecret and gets
// its OWN WebSocket session, supervised per-installation by the engine like
// Feishu, Slack and DingTalk — so several agents can each have a distinct bot
// identity in one InfoFlow organization.
//
// Each installation's WebSocket session only ever delivers events for its own
// robot, so the per-installation connection stamps its AppKey into the inbound
// envelope and the resolver routes on it (config->>'app_id'). Unlike a static
// token, InfoFlow outbound needs a short-lived access token minted from
// AppKey/AppSecret; the SDK handles acquisition + refresh internally.
//
// The integration is gated by MULTICA_INFOFLOW_SECRET_KEY. Without it the whole
// wiring block is skipped and the HTTP endpoints report configured:false.
package infoflow

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeInfoFlow is the channel discriminator for the InfoFlow adapter. It is
// defined here (not in the channel core package) on purpose: registering a new
// platform must not require editing the core, so the Type value lives with its
// adapter (mirroring dingtalk.TypeDingTalk and slack.TypeSlack).
const TypeInfoFlow channel.Type = "infoflow"

// installConfig is the JSON shape stored in channel_installation.config for an
// InfoFlow installation. The cross-platform columns stay flat; everything
// InfoFlow-specific lives in this opaque blob (the documented config boundary).
//
// app_id holds the AppKey. It is the per-installation routing key: the generic
// GetChannelInstallationByAppID query (config->>'app_id') and the
// (channel_type, app_id) unique index map an inbound event's AppKey to its
// installation, so several robots — several agents — in one InfoFlow org stay
// distinct.
//
// app_secret_encrypted is base64-encoded secretbox ciphertext, never plaintext
// (mirroring dingtalk's app_secret_encrypted). The AppKey itself is not a
// secret and lives in app_id in the clear.
type installConfig struct {
	AppID              string `json:"app_id"`
	AppSecretEncrypted string `json:"app_secret_encrypted"`
}

// credentials is the decoded, decrypted form the channel factory and outbound
// sender run on. The installation IDENTITY (workspace / agent / installer) is
// deliberately absent: it is resolved per message by the Router's
// InstallationResolver, exactly as the DingTalk and Slack adapters do.
type credentials struct {
	AppKey    string
	AppSecret string
}

// AppKeyFromConfig reads the plaintext AppKey (config.app_id) out of a stored
// channel_installation.config blob. The AppKey is a public identifier, not a
// secret, so — unlike decodeCredentials — this needs no decryptor and never
// fails on a sealed AppSecret: the management API can show which robot an
// installation belongs to even when the at-rest key it was sealed with has
// since rotated.
func AppKeyFromConfig(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	var cfg installConfig
	if json.Unmarshal(raw, &cfg) != nil {
		return ""
	}
	return strings.TrimSpace(cfg.AppID)
}

// Decrypter opens a secretbox-sealed credential blob. *secretbox.Box.Open has
// this shape, so the factory and outbound subscriber take it directly.
type Decrypter func([]byte) ([]byte, error)

var (
	// ErrProtocol is returned when an installation config cannot be decoded.
	ErrProtocol = errors.New("infoflow: invalid installation config")
	// ErrCredentials is returned when the sealed AppSecret cannot be opened.
	ErrCredentials = errors.New("infoflow: invalid app credentials")
)

// decodeCredentials decrypts the AppSecret out of an installation config.
func decodeCredentials(raw json.RawMessage, decrypt Decrypter) (credentials, error) {
	var cfg installConfig
	if decrypt == nil || len(raw) == 0 || json.Unmarshal(raw, &cfg) != nil {
		return credentials{}, ErrProtocol
	}
	appKey := strings.TrimSpace(cfg.AppID)
	if appKey == "" || cfg.AppSecretEncrypted == "" {
		return credentials{}, ErrProtocol
	}
	sealed, err := base64.StdEncoding.DecodeString(cfg.AppSecretEncrypted)
	if err != nil {
		return credentials{}, ErrProtocol
	}
	plain, err := decrypt(sealed)
	if err != nil {
		return credentials{}, fmt.Errorf("%w: %v", ErrCredentials, err)
	}
	appSecret := strings.TrimSpace(string(plain))
	if appSecret == "" {
		return credentials{}, ErrCredentials
	}
	return credentials{AppKey: appKey, AppSecret: appSecret}, nil
}
