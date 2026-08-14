// Package qianwen implements the HTTP polling bridge used by a private
// Qianwen Skill on Quark/Qianwen smart glasses.
package qianwen

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
)

// TypeQianwen is the durable channel discriminator for the Qianwen Skill
// polling bridge. It lives with the adapter so adding the platform does not
// change the channel core.
const TypeQianwen channel.Type = "qianwen"

const (
	connectionIDPrefix  = "qwc_"
	accessTokenPrefix   = "qws_"
	personalPollingMode = "personal_polling"
)

// installConfig is stored in channel_installation.config. The access token is
// generated once and returned to the installer; only its SHA-256 digest is
// persisted. app_id is a public routing id, not a credential.
type installConfig struct {
	AppID           string `json:"app_id"`
	AccessTokenHash string `json:"access_token_hash"`
	Mode            string `json:"mode"`
}

// PublicConfig is safe to return from management APIs.
type PublicConfig struct {
	ConnectionID string `json:"connection_id"`
	Mode         string `json:"mode"`
}

// DecodePublicConfig returns only the non-secret connection metadata.
func DecodePublicConfig(raw json.RawMessage) PublicConfig {
	var cfg installConfig
	_ = json.Unmarshal(raw, &cfg)
	return PublicConfig{ConnectionID: cfg.AppID, Mode: cfg.Mode}
}

func encodeInstallConfig(connectionID, token string) ([]byte, error) {
	cfg := installConfig{
		AppID:           connectionID,
		AccessTokenHash: hashAccessToken(token),
		Mode:            personalPollingMode,
	}
	return json.Marshal(cfg)
}

func verifyAccessToken(raw json.RawMessage, token string) bool {
	if !validEncodedCredential(token, accessTokenPrefix, 32) {
		return false
	}
	var cfg installConfig
	if err := json.Unmarshal(raw, &cfg); err != nil || cfg.Mode != personalPollingMode || cfg.AccessTokenHash == "" {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(cfg.AccessTokenHash)
	if err != nil || len(want) != sha256.Size {
		return false
	}
	got := sha256.Sum256([]byte(token))
	return subtle.ConstantTimeCompare(want, got[:]) == 1
}

// ValidCredentialShape rejects malformed public route keys before they reach
// the database or a rate-limiter key. It validates only encoding and length;
// authenticate still performs the constant-time secret comparison.
func ValidCredentialShape(connectionID, token string) bool {
	return validEncodedCredential(connectionID, connectionIDPrefix, 18) &&
		validEncodedCredential(token, accessTokenPrefix, 32)
}

func validEncodedCredential(value, prefix string, size int) bool {
	if !strings.HasPrefix(value, prefix) {
		return false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(value, prefix))
	return err == nil && len(decoded) == size
}

func hashAccessToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func generateCredentials() (connectionID, accessToken string, err error) {
	connectionID, err = randomCredential(connectionIDPrefix, 18)
	if err != nil {
		return "", "", fmt.Errorf("generate connection id: %w", err)
	}
	accessToken, err = randomCredential(accessTokenPrefix, 32)
	if err != nil {
		return "", "", fmt.Errorf("generate access token: %w", err)
	}
	return connectionID, accessToken, nil
}

func randomCredential(prefix string, size int) (string, error) {
	if size <= 0 {
		return "", errors.New("credential size must be positive")
	}
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return prefix + base64.RawURLEncoding.EncodeToString(b), nil
}
