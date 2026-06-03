package notify

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultAPNSProductionBaseURL = "https://api.push.apple.com"
	defaultAPNSSandboxBaseURL    = "https://api.sandbox.push.apple.com"
	apnsMaxResponseBodyBytes     = 4096
)

var ErrAPNSNotConfigured = errors.New("apns is not configured")
var ErrAPNSDeviceTokenInvalid = errors.New("apns device token is invalid")

type APNSConfig struct {
	TeamID     string
	KeyID      string
	BundleID   string
	AuthKeyP8  string
	BaseURL    string
	HTTPClient *http.Client
}

type APNSPushMessage struct {
	DeviceToken string
	RequestID   string
	Title       string
	Body        string
	ClickURL    string
	CollapseID  string
}

type APNSPushResult struct {
	APNSID string
	Reason string
}

type apnsErrorResponse struct {
	Reason string `json:"reason"`
}

func LoadAPNSConfig() (APNSConfig, error) {
	authKey := strings.TrimSpace(os.Getenv("APNS_AUTH_KEY_P8"))
	if authKey == "" {
		authKeyPath := strings.TrimSpace(os.Getenv("APNS_AUTH_KEY_PATH"))
		if authKeyPath != "" {
			data, err := os.ReadFile(authKeyPath)
			if err != nil {
				return APNSConfig{}, err
			}
			authKey = string(data)
		}
	}
	authKey = strings.ReplaceAll(authKey, `\n`, "\n")

	cfg := APNSConfig{
		TeamID:     strings.TrimSpace(os.Getenv("APNS_TEAM_ID")),
		KeyID:      strings.TrimSpace(os.Getenv("APNS_KEY_ID")),
		BundleID:   strings.TrimSpace(os.Getenv("APNS_BUNDLE_ID")),
		AuthKeyP8:  authKey,
		BaseURL:    strings.TrimSpace(os.Getenv("APNS_BASE_URL")),
		HTTPClient: &http.Client{Timeout: 10 * time.Second},
	}
	if cfg.BaseURL == "" {
		switch strings.ToLower(strings.TrimSpace(os.Getenv("APNS_ENV"))) {
		case "sandbox", "development", "dev":
			cfg.BaseURL = defaultAPNSSandboxBaseURL
		default:
			cfg.BaseURL = defaultAPNSProductionBaseURL
		}
	}
	if cfg.TeamID == "" || cfg.KeyID == "" || cfg.BundleID == "" || cfg.AuthKeyP8 == "" {
		return APNSConfig{}, ErrAPNSNotConfigured
	}
	return cfg, nil
}

func (c APNSConfig) SendPush(ctx context.Context, msg APNSPushMessage) (APNSPushResult, error) {
	deviceToken := strings.TrimSpace(msg.DeviceToken)
	if deviceToken == "" {
		return APNSPushResult{}, errors.New("missing apns device token")
	}

	token, err := c.authToken()
	if err != nil {
		return APNSPushResult{}, err
	}

	payload := map[string]any{
		"aps": map[string]any{
			"alert": map[string]any{
				"title": truncateRunes(firstNonBlank(msg.Title, "Multica"), 178),
				"body":  truncateRunes(firstNonBlank(msg.Body, "You have a new notification."), 512),
			},
			"sound": "default",
		},
	}
	if clickURL := normalizeAPNSClickURL(msg.ClickURL); clickURL != "" {
		payload["url"] = clickURL
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return APNSPushResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/3/device/"+deviceToken), bytes.NewReader(body))
	if err != nil {
		return APNSPushResult{}, err
	}
	req.Header.Set("Authorization", "bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("apns-topic", c.BundleID)
	req.Header.Set("apns-push-type", "alert")
	req.Header.Set("apns-priority", "10")
	if requestID := strings.TrimSpace(msg.RequestID); requestID != "" {
		req.Header.Set("apns-id", requestID)
	}
	if collapseID := normalizeAPNSCollapseID(msg.CollapseID); collapseID != "" {
		req.Header.Set("apns-collapse-id", collapseID)
	}

	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return APNSPushResult{}, err
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, apnsMaxResponseBodyBytes))
	result := APNSPushResult{APNSID: resp.Header.Get("apns-id")}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return result, nil
	}

	var apnsErr apnsErrorResponse
	if len(respBody) > 0 {
		_ = json.Unmarshal(respBody, &apnsErr)
	}
	reason := strings.TrimSpace(apnsErr.Reason)
	if reason == "" {
		reason = strings.TrimSpace(string(respBody))
	}
	if reason == "" {
		reason = http.StatusText(resp.StatusCode)
	}
	result.Reason = reason
	if isAPNSInvalidDeviceTokenResponse(resp.StatusCode, reason) {
		return result, fmt.Errorf("%w: %s", ErrAPNSDeviceTokenInvalid, reason)
	}
	return result, fmt.Errorf("apns push failed: status=%d reason=%s", resp.StatusCode, reason)
}

func (c APNSConfig) authToken() (string, error) {
	key, err := parseAPNSPrivateKey(c.AuthKeyP8)
	if err != nil {
		return "", err
	}
	header, err := json.Marshal(map[string]string{
		"alg": "ES256",
		"kid": c.KeyID,
	})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]any{
		"iss": c.TeamID,
		"iat": time.Now().Unix(),
	})
	if err != nil {
		return "", err
	}

	signingInput := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(signingInput))
	r, s, err := ecdsa.Sign(rand.Reader, key, digest[:])
	if err != nil {
		return "", err
	}
	signature := appendFixedWidthInt(nil, r, 32)
	signature = appendFixedWidthInt(signature, s, 32)
	return signingInput + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func parseAPNSPrivateKey(raw string) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(raw)))
	if block == nil {
		return nil, errors.New("invalid apns auth key pem")
	}
	if key, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ecdsaKey, ok := key.(*ecdsa.PrivateKey)
		if !ok {
			return nil, errors.New("apns auth key is not an ecdsa private key")
		}
		return validateAPNSPrivateKey(ecdsaKey)
	}
	key, err := x509.ParseECPrivateKey(block.Bytes)
	if err != nil {
		return nil, errors.New("invalid apns auth key")
	}
	return validateAPNSPrivateKey(key)
}

func validateAPNSPrivateKey(key *ecdsa.PrivateKey) (*ecdsa.PrivateKey, error) {
	if key == nil || key.Curve != elliptic.P256() {
		return nil, errors.New("apns auth key must use p-256")
	}
	return key, nil
}

func appendFixedWidthInt(dst []byte, value *big.Int, width int) []byte {
	raw := value.Bytes()
	if len(raw) > width {
		raw = raw[len(raw)-width:]
	}
	for i := len(raw); i < width; i++ {
		dst = append(dst, 0)
	}
	return append(dst, raw...)
}

func (c APNSConfig) endpoint(path string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	return base + "/" + strings.TrimLeft(path, "/")
}

func normalizeAPNSClickURL(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(value, "wujieai-multicam://") {
		return value
	}
	return ""
}

func normalizeAPNSCollapseID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if len(value) > 64 {
		return value[:64]
	}
	return value
}

func isAPNSInvalidDeviceTokenResponse(statusCode int, reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "baddevicetoken", "devicetokennotfortopic", "unregistered":
		return statusCode == http.StatusBadRequest || statusCode == http.StatusGone
	default:
		return false
	}
}
