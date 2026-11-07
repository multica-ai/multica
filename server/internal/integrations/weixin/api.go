// Package weixin connects WeChat iLink Bot conversations to Multica channels.
package weixin

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	DefaultBaseURL   = "https://ilinkai.weixin.qq.com"
	protocolVersion  = "2.4.10"
	apiTimeout       = 15 * time.Second
	pollTimeout      = 40 * time.Second
	maxResponseBytes = 4 << 20
)

var (
	ErrProtocol     = errors.New("weixin: invalid provider response")
	ErrTokenExpired = errors.New("weixin: authorization expired; reconnect WeChat")
	ErrNoContext    = errors.New("weixin: send a new WeChat message before replying")
)

// ProviderError contains only codes: provider bodies can contain credentials.
type ProviderError struct {
	Status int
	Code   int
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("weixin: provider rejected request (HTTP %d, code %d)", e.Status, e.Code)
}

type apiResult struct {
	Ret     *int `json:"ret"`
	ErrCode int  `json:"errcode"`
}

func (r apiResult) check(required bool) error {
	if r.ErrCode == -14 || r.Ret != nil && *r.Ret == -14 {
		return ErrTokenExpired
	}
	code := r.ErrCode
	if code == 0 && r.Ret != nil {
		code = *r.Ret
	}
	if code != 0 {
		return &ProviderError{Status: http.StatusOK, Code: code}
	}
	if required && r.Ret == nil {
		return ErrProtocol
	}
	return nil
}

// id preserves 64-bit provider identifiers without passing through float64.
type id string

func (v *id) UnmarshalJSON(b []byte) error {
	if len(b) == 0 || string(b) == "null" {
		return nil
	}
	if b[0] == '"' {
		return json.Unmarshal(b, (*string)(v))
	}
	var n json.Number
	if err := json.Unmarshal(b, &n); err != nil {
		return err
	}
	if _, err := strconv.ParseUint(n.String(), 10, 64); err != nil {
		return err
	}
	*v = id(n.String())
	return nil
}

type mediaRef struct {
	Query       string `json:"encrypt_query_param,omitempty"`
	AESKey      string `json:"aes_key,omitempty"`
	EncryptType int    `json:"encrypt_type,omitempty"`
	FullURL     string `json:"full_url,omitempty"`
}

type messageItem struct {
	Type int `json:"type"`
	Text *struct {
		Text string `json:"text"`
	} `json:"text_item,omitempty"`
	Image *struct {
		Media  mediaRef `json:"media"`
		AESKey string   `json:"aeskey,omitempty"`
		Size   int64    `json:"mid_size,omitempty"`
	} `json:"image_item,omitempty"`
	Voice *struct {
		Media mediaRef `json:"media"`
		Text  string   `json:"text,omitempty"`
	} `json:"voice_item,omitempty"`
	File *struct {
		Media  mediaRef `json:"media"`
		Name   string   `json:"file_name"`
		Length string   `json:"len,omitempty"`
	} `json:"file_item,omitempty"`
	Video *struct {
		Media mediaRef `json:"media"`
		Size  int64    `json:"video_size,omitempty"`
	} `json:"video_item,omitempty"`
}

type message struct {
	ID           id            `json:"message_id,omitempty"`
	From         string        `json:"from_user_id"`
	To           string        `json:"to_user_id"`
	ClientID     string        `json:"client_id,omitempty"`
	Type         int           `json:"message_type"`
	State        *int          `json:"message_state,omitempty"`
	GroupID      string        `json:"group_id,omitempty"`
	CreatedAt    int64         `json:"create_time_ms,omitempty"`
	ContextToken string        `json:"context_token,omitempty"`
	Items        []messageItem `json:"item_list"`
}

type qrResult struct {
	apiResult
	QRCode  string `json:"qrcode"`
	Content string `json:"qrcode_img_content"`
}

type qrStatus struct {
	apiResult
	Status       string `json:"status"`
	Token        string `json:"bot_token"`
	BotID        string `json:"ilink_bot_id"`
	UserID       string `json:"ilink_user_id"`
	BaseURL      string `json:"baseurl"`
	RedirectHost string `json:"redirect_host"`
}

type updates struct {
	apiResult
	Messages  []message `json:"msgs"`
	Cursor    string    `json:"get_updates_buf"`
	TimeoutMS int64     `json:"longpolling_timeout_ms"`
}

type apiClient struct {
	baseURL string
	token   string
	http    *http.Client
}

func newAPI(baseURL, token string, client *http.Client) *apiClient {
	if baseURL == "" {
		baseURL = DefaultBaseURL
	}
	if client == nil {
		client = &http.Client{Timeout: pollTimeout}
	}
	copy := *client
	copy.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &apiClient{baseURL: strings.TrimRight(baseURL, "/"), token: token, http: &copy}
}

// The provider can move an account to another WeChat IDC during QR login.
// Neither a browser-supplied URL nor an arbitrary redirect may receive tokens.
func validateBaseURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Port() != "" || u.RawQuery != "" || u.Fragment != "" || u.Path != "" && u.Path != "/" {
		return "", ErrProtocol
	}
	host := strings.ToLower(u.Hostname())
	if !strings.HasSuffix(host, ".weixin.qq.com") {
		return "", ErrProtocol
	}
	return "https://" + host, nil
}

func (c *apiClient) request(ctx context.Context, method, endpoint string, body any, authenticated bool, result any) error {
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+"/ilink/bot/"+endpoint, reader)
	if err != nil {
		return ErrProtocol
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", strconv.Itoa(2<<16|4<<8|10))
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		var random [4]byte
		if _, err := rand.Read(random[:]); err != nil {
			return err
		}
		req.Header.Set("X-WECHAT-UIN", base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(binary.BigEndian.Uint32(random[:])), 10))))
	}
	if authenticated {
		if c.token == "" {
			return ErrTokenExpired
		}
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// net/url errors can include the QR code or verification code in URLs.
		return errors.New("weixin: provider request failed")
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &ProviderError{Status: resp.StatusCode}
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil || len(b) > maxResponseBytes {
		return ErrProtocol
	}
	if err := json.Unmarshal(b, result); err != nil {
		return ErrProtocol
	}
	return nil
}

func baseInfo() map[string]string {
	return map[string]string{"channel_version": protocolVersion, "bot_agent": "Multica"}
}

func (c *apiClient) QRCode(ctx context.Context) (qrResult, error) {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	var result qrResult
	err := c.request(ctx, http.MethodPost, "get_bot_qrcode?bot_type=3", map[string]any{"local_token_list": []string{}}, false, &result)
	if err != nil {
		return result, err
	}
	if err := result.check(false); err != nil {
		return result, err
	}
	if result.QRCode == "" || result.Content == "" {
		return result, ErrProtocol
	}
	return result, nil
}

func (c *apiClient) QRStatus(ctx context.Context, qr, verifyCode string) (qrStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	q := url.Values{"qrcode": {qr}}
	if verifyCode != "" {
		q.Set("verify_code", verifyCode)
	}
	var result qrStatus
	err := c.request(ctx, http.MethodGet, "get_qrcode_status?"+q.Encode(), nil, false, &result)
	if err != nil {
		return result, err
	}
	if err := result.check(false); err != nil {
		return result, err
	}
	switch result.Status {
	case "wait", "scaned", "expired", "need_verifycode", "verify_code_blocked", "binded_redirect":
	case "scaned_but_redirect":
		if _, err := validateBaseURL("https://" + result.RedirectHost); err != nil {
			return result, err
		}
	case "confirmed":
		if result.Token == "" || result.BotID == "" || result.UserID == "" {
			return result, ErrProtocol
		}
		if result.BaseURL == "" {
			result.BaseURL = DefaultBaseURL
			if base, err := validateBaseURL(c.baseURL); err == nil {
				result.BaseURL = base
			}
		}
		if _, err := validateBaseURL(result.BaseURL); err != nil {
			return result, err
		}
	default:
		return result, ErrProtocol
	}
	return result, nil
}

func (c *apiClient) Updates(ctx context.Context, cursor string) (updates, error) {
	ctx, cancel := context.WithTimeout(ctx, pollTimeout)
	defer cancel()
	var result updates
	err := c.request(ctx, http.MethodPost, "getupdates", map[string]any{"get_updates_buf": cursor, "base_info": baseInfo()}, true, &result)
	if err != nil {
		return result, err
	}
	return result, result.check(true)
}

func (c *apiClient) Send(ctx context.Context, msg message) (string, error) {
	if msg.ContextToken == "" {
		return "", ErrNoContext
	}
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	var result struct {
		apiResult
		MessageID id `json:"message_id"`
	}
	err := c.request(ctx, http.MethodPost, "sendmessage", map[string]any{"msg": msg, "base_info": baseInfo()}, true, &result)
	if err != nil {
		return "", err
	}
	if err := result.check(true); err != nil {
		return "", err
	}
	// Some successful responses omit a server ID. The stable client ID records
	// acceptance, not proof that the recipient read or received the message.
	if result.MessageID == "" {
		return msg.ClientID, nil
	}
	return string(result.MessageID), nil
}

func textMessage(to, token, clientID, text string) message {
	finished := 2
	item := messageItem{Type: 1}
	item.Text = &struct {
		Text string `json:"text"`
	}{Text: text}
	return message{To: to, ClientID: clientID, Type: 2, State: &finished, ContextToken: token, Items: []messageItem{item}}
}

func (c *apiClient) notify(ctx context.Context, start bool) error {
	ctx, cancel := context.WithTimeout(ctx, apiTimeout)
	defer cancel()
	endpoint := "msg/notifystop"
	if start {
		endpoint = "msg/notifystart"
	}
	var result apiResult
	if err := c.request(ctx, http.MethodPost, endpoint, map[string]any{"base_info": baseInfo()}, true, &result); err != nil {
		return err
	}
	return result.check(true)
}
