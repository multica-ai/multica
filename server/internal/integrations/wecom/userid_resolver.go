package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const wecomAPIBase = "https://qyapi.weixin.qq.com/cgi-bin"

type UserIDResolver struct {
	client *http.Client
	mu     sync.Mutex
	tokens map[string]tokenEntry // keyed by corpID|corpSecret hash key
}

type tokenEntry struct {
	token     string
	expiresAt time.Time
}

func NewUserIDResolver() *UserIDResolver {
	return &UserIDResolver{
		client: &http.Client{Timeout: 10 * time.Second},
		tokens: make(map[string]tokenEntry),
	}
}

// Resolve returns a plaintext userid. If raw is already a short alphanumeric
// account name, it is returned as-is. Otherwise it is treated as an encrypted
// open_userid and converted via the self-built app API.
func (r *UserIDResolver) Resolve(ctx context.Context, corpID, corpSecret, raw string) (string, error) {
	if raw == "" {
		return "", fmt.Errorf("empty userid")
	}
	if looksLikePlainUserid(raw) {
		return raw, nil
	}
	token, err := r.accessToken(ctx, corpID, corpSecret)
	if err != nil {
		return "", err
	}
	reqBody, _ := json.Marshal(map[string]any{
		"open_userid_list": []string{raw},
	})
	url := fmt.Sprintf("%s/batch/openuserid_to_userid?access_token=%s", wecomAPIBase, token)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(reqBody))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		ErrCode int `json:"errcode"`
		ErrMsg  string `json:"errmsg"`
		UseridList []struct {
			OpenUserid string `json:"open_userid"`
			Userid     string `json:"userid"`
		} `json:"userid_list"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.ErrCode != 0 {
		return "", fmt.Errorf("openuserid_to_userid: %s", parsed.ErrMsg)
	}
	for _, row := range parsed.UseridList {
		if row.OpenUserid == raw && row.Userid != "" {
			return row.Userid, nil
		}
	}
	return "", fmt.Errorf("userid conversion failed for %q", raw)
}

func (r *UserIDResolver) accessToken(ctx context.Context, corpID, corpSecret string) (string, error) {
	key := corpID + "|" + corpSecret
	r.mu.Lock()
	if ent, ok := r.tokens[key]; ok && time.Now().Before(ent.expiresAt) {
		r.mu.Unlock()
		return ent.token, nil
	}
	r.mu.Unlock()

	url := fmt.Sprintf("%s/gettoken?corpid=%s&corpsecret=%s", wecomAPIBase, corpID, corpSecret)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var parsed struct {
		ErrCode     int    `json:"errcode"`
		ErrMsg      string `json:"errmsg"`
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	if parsed.ErrCode != 0 || parsed.AccessToken == "" {
		return "", fmt.Errorf("gettoken: %s", parsed.ErrMsg)
	}
	r.mu.Lock()
	r.tokens[key] = tokenEntry{
		token:     parsed.AccessToken,
		expiresAt: time.Now().Add(time.Duration(parsed.ExpiresIn-120) * time.Second),
	}
	r.mu.Unlock()
	return parsed.AccessToken, nil
}

func looksLikePlainUserid(s string) bool {
	if len(s) == 0 || len(s) > 64 {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' || r == '.' {
			continue
		}
		return false
	}
	return true
}
