package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/auth"
)

const googleStatePrefix = "google"

var ErrGoogleNotConfigured = errors.New("Google OAuth is not configured")

type GoogleBindingState struct {
	UserID      string `json:"user_id"`
	NextPath    string `json:"next_path,omitempty"`
	RedirectURI string `json:"redirect_uri,omitempty"`
	IssuedAt    int64  `json:"issued_at"`
}

type GoogleConfig struct {
	ClientID     string
	ClientSecret string
}

func LoadGoogleConfig() (GoogleConfig, error) {
	cfg := GoogleConfig{
		ClientID:     strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_ID")),
		ClientSecret: strings.TrimSpace(os.Getenv("GOOGLE_CLIENT_SECRET")),
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return GoogleConfig{}, ErrGoogleNotConfigured
	}
	return cfg, nil
}

func (c GoogleConfig) RedirectURL() string {
	return strings.TrimRight(AppURL(), "/") + "/auth/callback"
}

func (c GoogleConfig) AuthorizationURL(state string) string {
	return c.AuthorizationURLWithRedirectURI(state, c.RedirectURL())
}

func (c GoogleConfig) AuthorizationURLWithRedirectURI(state, redirectURI string) string {
	redirectURI = strings.TrimSpace(redirectURI)
	if redirectURI == "" {
		redirectURI = c.RedirectURL()
	}

	values := url.Values{}
	values.Set("client_id", c.ClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("state", state)
	values.Set("response_type", "code")
	values.Set("scope", "openid email profile")
	values.Set("access_type", "offline")
	values.Set("prompt", "consent")
	return fmt.Sprintf("https://accounts.google.com/o/oauth2/v2/auth?%s", values.Encode())
}

func IsGoogleBindingState(state string) bool {
	return strings.HasPrefix(state, googleStatePrefix+".")
}

func BuildGoogleBindingState(payload GoogleBindingState) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(encoded))
	signature := hex.EncodeToString(mac.Sum(nil))
	return googleStatePrefix + "." + encoded + "." + signature, nil
}

func ParseGoogleBindingState(state string) (GoogleBindingState, error) {
	parts := strings.Split(state, ".")
	if len(parts) != 3 || parts[0] != googleStatePrefix {
		return GoogleBindingState{}, errors.New("invalid google state format")
	}
	mac := hmac.New(sha256.New, auth.JWTSecret())
	mac.Write([]byte(parts[1]))
	expected := mac.Sum(nil)
	got, err := hex.DecodeString(parts[2])
	if err != nil || !hmac.Equal(expected, got) {
		return GoogleBindingState{}, errors.New("invalid google state signature")
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return GoogleBindingState{}, errors.New("invalid google state payload")
	}
	var payload GoogleBindingState
	if err := json.Unmarshal(raw, &payload); err != nil {
		return GoogleBindingState{}, errors.New("invalid google state payload")
	}
	return payload, nil
}
