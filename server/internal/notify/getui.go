package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

const (
	defaultGetuiBaseURL       = "https://restapi.getui.com/v2"
	getuiMaxResponseBodyBytes = 4096
	getuiTokenRefreshSkew     = time.Minute
)

var ErrGetuiNotConfigured = errors.New("getui is not configured")
var ErrGetuiTokenExpired = errors.New("getui token expired")

var getuiTokenCaches sync.Map

type GetuiConfig struct {
	AppID        string
	AppKey       string
	MasterSecret string
	BaseURL      string
	HTTPClient   *http.Client
}

type GetuiPushMessage struct {
	CID       string
	RequestID string
	Title     string
	Body      string
	TTL       int64
}

type GetuiPushResult struct {
	TaskID string
	Status string
}

type getuiCachedToken struct {
	token     string
	expiresAt time.Time
}

type getuiTokenCache struct {
	mu    sync.Mutex
	token getuiCachedToken
}

type getuiCommonResponse struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

type getuiAuthData struct {
	ExpireTime json.RawMessage `json:"expire_time"`
	Token      string          `json:"token"`
}

func LoadGetuiConfig() (GetuiConfig, error) {
	cfg := GetuiConfig{
		AppID:        strings.TrimSpace(os.Getenv("GETUI_APP_ID")),
		AppKey:       strings.TrimSpace(os.Getenv("GETUI_APP_KEY")),
		MasterSecret: strings.TrimSpace(os.Getenv("GETUI_MASTER_SECRET")),
		BaseURL:      strings.TrimSpace(os.Getenv("GETUI_BASE_URL")),
		HTTPClient:   &http.Client{Timeout: 10 * time.Second},
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = defaultGetuiBaseURL
	}
	if cfg.AppID == "" || cfg.AppKey == "" || cfg.MasterSecret == "" {
		return GetuiConfig{}, ErrGetuiNotConfigured
	}
	return cfg, nil
}

func (c GetuiConfig) PushSingleByCID(ctx context.Context, msg GetuiPushMessage) (GetuiPushResult, error) {
	token, err := c.AuthToken(ctx, false)
	if err != nil {
		return GetuiPushResult{}, err
	}

	result, err := c.pushSingleByCIDWithToken(ctx, token, msg)
	if errors.Is(err, ErrGetuiTokenExpired) {
		token, authErr := c.AuthToken(ctx, true)
		if authErr != nil {
			return GetuiPushResult{}, authErr
		}
		return c.pushSingleByCIDWithToken(ctx, token, msg)
	}
	return result, err
}

func (c GetuiConfig) AuthToken(ctx context.Context, forceRefresh bool) (string, error) {
	cache := c.tokenCache()
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if !forceRefresh && cache.token.token != "" && time.Now().Before(cache.token.expiresAt.Add(-getuiTokenRefreshSkew)) {
		return cache.token.token, nil
	}

	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signHash := sha256.Sum256([]byte(c.AppKey + timestamp + c.MasterSecret))
	body, err := json.Marshal(map[string]string{
		"sign":      hex.EncodeToString(signHash[:]),
		"timestamp": timestamp,
		"appkey":    c.AppKey,
	})
	if err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/auth"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")

	var resp getuiCommonResponse
	if err := c.doJSON(req, &resp); err != nil {
		return "", err
	}
	if resp.Code != 0 {
		return "", fmt.Errorf("getui auth failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	var data getuiAuthData
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return "", errors.New("invalid getui auth response")
	}
	token := strings.TrimSpace(data.Token)
	if token == "" {
		return "", errors.New("getui auth response missing token")
	}
	expiresAt := parseGetuiExpireTime(data.ExpireTime)
	if expiresAt.IsZero() {
		expiresAt = time.Now().Add(24 * time.Hour)
	}

	cache.token = getuiCachedToken{token: token, expiresAt: expiresAt}
	return token, nil
}

func (c GetuiConfig) pushSingleByCIDWithToken(ctx context.Context, token string, msg GetuiPushMessage) (GetuiPushResult, error) {
	cid := strings.TrimSpace(msg.CID)
	if cid == "" {
		return GetuiPushResult{}, errors.New("missing getui cid")
	}
	requestID := strings.TrimSpace(msg.RequestID)
	if requestID == "" {
		requestID = randomGetuiRequestID()
	}
	ttl := msg.TTL
	if ttl == 0 {
		ttl = int64((2 * time.Hour) / time.Millisecond)
	}

	body, err := json.Marshal(map[string]any{
		"request_id": truncateRunes(requestID, 32),
		"settings": map[string]any{
			"ttl": ttl,
		},
		"audience": map[string]any{
			"cid": []string{cid},
		},
		"push_message": map[string]any{
			"notification": map[string]any{
				"title":      truncateRunes(firstNonBlank(msg.Title, "Multica"), 50),
				"body":       truncateRunes(firstNonBlank(msg.Body, "You have a new notification."), 256),
				"click_type": "none",
			},
		},
	})
	if err != nil {
		return GetuiPushResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/push/single/cid"), bytes.NewReader(body))
	if err != nil {
		return GetuiPushResult{}, err
	}
	req.Header.Set("Content-Type", "application/json;charset=utf-8")
	req.Header.Set("token", token)

	var resp getuiCommonResponse
	if err := c.doJSON(req, &resp); err != nil {
		return GetuiPushResult{}, err
	}
	if resp.Code == 10001 {
		return GetuiPushResult{}, ErrGetuiTokenExpired
	}
	if resp.Code != 0 {
		return GetuiPushResult{}, fmt.Errorf("getui push failed: code=%d msg=%s", resp.Code, resp.Msg)
	}

	taskID, status := parseGetuiPushResult(resp.Data, cid)
	return GetuiPushResult{TaskID: taskID, Status: status}, nil
}

func (c GetuiConfig) doJSON(req *http.Request, out any) error {
	client := c.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, getuiMaxResponseBodyBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("getui returned %d: %s", resp.StatusCode, msg)
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("invalid getui response: %w", err)
	}
	return nil
}

func (c GetuiConfig) endpoint(path string) string {
	base := strings.TrimRight(c.BaseURL, "/")
	return base + "/" + strings.Trim(c.AppID, "/") + path
}

func (c GetuiConfig) tokenCache() *getuiTokenCache {
	key := strings.Join([]string{c.BaseURL, c.AppID, c.AppKey}, "|")
	cache, _ := getuiTokenCaches.LoadOrStore(key, &getuiTokenCache{})
	return cache.(*getuiTokenCache)
}

func parseGetuiExpireTime(raw json.RawMessage) time.Time {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		ms, err := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		if err == nil && ms > 0 {
			return time.UnixMilli(ms)
		}
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil && n > 0 {
		return time.UnixMilli(n)
	}
	return time.Time{}
}

func parseGetuiPushResult(raw json.RawMessage, cid string) (string, string) {
	var data map[string]map[string]string
	if err := json.Unmarshal(raw, &data); err != nil {
		return "", ""
	}
	for taskID, statuses := range data {
		return taskID, statuses[cid]
	}
	return "", ""
}

func randomGetuiRequestID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 10)
	}
	return hex.EncodeToString(b[:])
}

func truncateRunes(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || value == "" || utf8.RuneCountInString(value) <= limit {
		return value
	}
	out := make([]rune, 0, limit)
	for _, r := range value {
		if len(out) >= limit {
			break
		}
		out = append(out, r)
	}
	return string(out)
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			return value
		}
	}
	return ""
}
