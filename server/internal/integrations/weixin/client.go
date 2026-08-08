package weixin

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
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
	DefaultBaseURL = "https://ilinkai.weixin.qq.com"
	channelVersion = "2.2.0"
	clientVersion  = "131584"
)

type Client struct{ HTTP *http.Client }

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 40 * time.Second}
	}
	return &Client{HTTP: httpClient}
}

type QRCode struct {
	Code    string `json:"qrcode"`
	Content string `json:"qrcode_img_content"`
}

type QRStatus struct {
	Status       string `json:"status"`
	RedirectHost string `json:"redirect_host"`
	BotToken     string `json:"bot_token"`
	BotID        string `json:"ilink_bot_id"`
	BaseURL      string `json:"baseurl"`
	WeixinUserID string `json:"ilink_user_id"`
}

type APIResponse struct {
	Ret       int              `json:"ret"`
	ErrCode   int              `json:"errcode"`
	ErrorMsg  string           `json:"errmsg"`
	SyncBuf   string           `json:"get_updates_buf"`
	TimeoutMS int              `json:"longpolling_timeout_ms"`
	Messages  []InboundMessage `json:"msgs"`
}

type APIError struct {
	Ret     int
	ErrCode int
	Message string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("weixin: iLink ret=%d errcode=%d: %s", e.Ret, e.ErrCode, e.Message)
}

type InboundMessage struct {
	MessageID    json.RawMessage `json:"message_id"`
	FromUserID   string          `json:"from_user_id"`
	ToUserID     string          `json:"to_user_id"`
	RoomID       string          `json:"room_id"`
	ChatRoomID   string          `json:"chat_room_id"`
	ContextToken string          `json:"context_token"`
	MessageType  int             `json:"message_type"`
	ItemList     []MessageItem   `json:"item_list"`
}

type MessageItem struct {
	Type     int       `json:"type"`
	TextItem *TextItem `json:"text_item,omitempty"`
}

type TextItem struct {
	Text string `json:"text"`
}

func (m InboundMessage) ID() string {
	return strings.Trim(string(bytes.TrimSpace(m.MessageID)), `"`)
}

func (m InboundMessage) Text() string {
	for _, item := range m.ItemList {
		if item.Type == 1 && item.TextItem != nil {
			return strings.TrimSpace(item.TextItem.Text)
		}
	}
	return ""
}

func (c *Client) GetQRCode(ctx context.Context) (QRCode, error) {
	var out QRCode
	err := c.get(ctx, DefaultBaseURL, "/ilink/bot/get_bot_qrcode?bot_type=3", &out)
	return out, err
}

func (c *Client) GetQRCodeStatus(ctx context.Context, baseURL, code string) (QRStatus, error) {
	var out QRStatus
	err := c.get(ctx, baseURL, "/ilink/bot/get_qrcode_status?qrcode="+url.QueryEscape(code), &out)
	return out, err
}

func (c *Client) GetUpdates(ctx context.Context, baseURL, token, syncBuf string) (APIResponse, error) {
	var out APIResponse
	err := c.post(ctx, baseURL, "/ilink/bot/getupdates", token, map[string]any{"get_updates_buf": syncBuf}, &out)
	return out, err
}

func (c *Client) SendText(ctx context.Context, baseURL, token, to, text, contextToken string) (string, error) {
	clientID, err := randomHex(16)
	if err != nil {
		return "", err
	}
	msg := map[string]any{
		"from_user_id": "", "to_user_id": to, "client_id": clientID,
		"message_type": 2, "message_state": 2,
		"item_list": []any{map[string]any{"type": 1, "text_item": map[string]any{"text": text}}},
	}
	if contextToken != "" {
		msg["context_token"] = contextToken
	}
	var out APIResponse
	if err := c.post(ctx, baseURL, "/ilink/bot/sendmessage", token, map[string]any{"msg": msg}, &out); err != nil {
		return "", err
	}
	if out.Ret != 0 || out.ErrCode != 0 {
		return "", &APIError{Ret: out.Ret, ErrCode: out.ErrCode, Message: out.ErrorMsg}
	}
	return clientID, nil
}

func (c *Client) get(ctx context.Context, baseURL, path string, out any) error {
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
	if err != nil {
		return err
	}
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", clientVersion)
	return c.do(req, out)
}

func (c *Client) post(ctx context.Context, baseURL, path, token string, payload map[string]any, out any) error {
	base, err := normalizeBaseURL(baseURL)
	if err != nil {
		return err
	}
	payload["base_info"] = map[string]any{"channel_version": channelVersion}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("AuthorizationType", "ilink_bot_token")
	req.Header.Set("iLink-App-Id", "bot")
	req.Header.Set("iLink-App-ClientVersion", clientVersion)
	req.Header.Set("X-WECHAT-UIN", randomWeixinUIN())
	req.Header.Set("Authorization", "Bearer "+token)
	return c.do(req, out)
}

func (c *Client) do(req *http.Request, out any) error {
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("weixin: iLink HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func normalizeBaseURL(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		raw = DefaultBaseURL
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil || u.Hostname() == "" {
		return "", errors.New("weixin: invalid iLink base URL")
	}
	host := strings.ToLower(u.Hostname())
	if host != "ilinkai.weixin.qq.com" && !strings.HasSuffix(host, ".weixin.qq.com") {
		return "", errors.New("weixin: untrusted iLink base URL")
	}
	u.RawQuery, u.Fragment, u.Path = "", "", ""
	return strings.TrimRight(u.String(), "/"), nil
}

func randomWeixinUIN() string {
	var b [4]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	n := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(n), 10)))
}

func randomHex(size int) (string, error) {
	b := make([]byte, size)
	if _, err := cryptorand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}
