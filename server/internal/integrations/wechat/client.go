package wechat

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"
)

// defaultWechatBaseURL is the default iLink API host. The QR-login status poll
// returns a per-account base_url which may differ from this (the iLink backend
// shards accounts across hosts); that per-account value is persisted in the
// installation config and used for getupdates/sendmessage. This constant only
// seeds the QR-login flow (the first step, before a base_url is known) and can
// be overridden via the MULTICA_WECHAT_BASE_URL env var for proxy/mock/staging.
const defaultWechatBaseURL = "https://ilinkai.weixin.qq.com"

// requestTimeout bounds a normal (non-long-poll) iLink API call: qrcode,
// qrcode/status, sendmessage. getupdates is a long poll (server holds ~35s) and
// uses its own longer timeout.
const (
	requestTimeout  = 15 * time.Second
	longPollTimeout = 45 * time.Second // getupdates server hold (~35s) + margin
)

// iLinkClient talks to the WeChat ClawBot (iLink) HTTP API. It is stateless
// apart from the injected base URL and HTTP client; per-call credentials
// (bot_token, base_url) are passed in by the caller. All HTTP requests use the
// caller's context so a cancelled context (lease loss, shutdown) tears down an
// in-flight call in bounded time — required by the supervisor's reconnect
// contract.
//
// PROTOCOL CAVEAT: the iLink API is not fully documented publicly; the request
// shapes below are derived from the official ClawBot doc
// (developers.weixin.qq.com/doc/aispeech/knowledge/openapi/Clawbotrelated.html)
// and community reverse-engineering (hao-ji-xing/openclaw-weixin). Field names /
// paths may need calibration against live traffic; they are concentrated here so
// a fix touches one file.
type iLinkClient struct {
	baseURL    string        // default host for the QR-login flow (before per-account base_url is known)
	httpClient *http.Client // shared short-call client; long polls build their own
	logger     *slog.Logger
}

// newILinkClient builds an iLink HTTP client. A non-empty baseURL overrides the
// default iLink host (for proxy/mock/staging via MULTICA_WECHAT_BASE_URL); an
// empty baseURL falls back to defaultWechatBaseURL.
func newILinkClient(baseURL string, logger *slog.Logger) *iLinkClient {
	if baseURL == "" {
		baseURL = defaultWechatBaseURL
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &iLinkClient{
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: requestTimeout},
		logger:     logger,
	}
}

// QRLoginResponse is the result of a successful QR-login status poll: the
// per-account credentials the bot will run on.
type QRLoginResponse struct {
	BotToken    string // bearer token for getupdates / sendmessage
	BaseURL     string // per-account API host for getupdates / sendmessage
	IlinkBotID  string // bot identity, e.g. "xxxxxx@im.bot" (stored as app_id)
	IlinkUserID string // human-readable id of the account that scanned
}

// getQRCode starts a QR-login session. Per the iLink protocol it is a GET to
// /ilink/bot/get_bot_qrcode?bot_type=3. The response carries TWO values:
//   - qrcode: an opaque token used to poll status (getQRStatus re-queries by it)
//   - qrcode_img_content: the actual content URL (https://liteapp.weixin.qq.com/...)
//     that must be rendered as the QR image the user scans.
//
// Callers must NOT render the `qrcode` token as the QR — that yields an
// unscannable text QR. Only qrcode_img_content is scannable.
func (c *iLinkClient) getQRCode(ctx context.Context) (qrcode, qrImageContent string, err error) {
	var resp struct {
		Ret             int    `json:"ret"`
		Qrcode          string `json:"qrcode"`
		QrcodeImgContent string `json:"qrcode_img_content"`
		Message         string `json:"errmsg"`
	}
	if err := c.getJSON(ctx, c.baseURL, "/ilink/bot/get_bot_qrcode?bot_type=3", &resp); err != nil {
		return "", "", err
	}
	if resp.Qrcode == "" {
		return "", "", fmt.Errorf("wechat: qrcode response missing field (ret=%d errmsg=%q)", resp.Ret, resp.Message)
	}
	return resp.Qrcode, resp.QrcodeImgContent, nil
}

// pollQRStatus checks a QR-login session by the qrcode token. Per the iLink
// protocol it is a GET to /ilink/bot/get_qrcode_status?qrcode=<token> that
// BLOCKS (long-poll style) until the user scans+confirms in WeChat, then returns
// {status:"confirmed", bot_token, baseurl, ilink_bot_id, ilink_user_id}. Before
// the scan it holds the connection open, so this call uses a long timeout (it is
// driven from the install service's lazy poll, not a tight loop). The response's
// `status` string discriminates the state ("confirmed" on success). On timeout
// while still waiting it returns ("pending", …) so the caller polls again.
func (c *iLinkClient) pollQRStatus(ctx context.Context, qrcode string) (status string, login QRLoginResponse, err error) {
	// Long-poll: the server holds until scan/confirm or its own timeout. Use an
	// isolated client with a long timeout but still bound by the caller's ctx.
	lpClient := &http.Client{Timeout: longPollTimeout}
	var resp struct {
		Status      string `json:"status"` // "confirmed" on success
		Ret         int    `json:"ret"`
		BotToken    string `json:"bot_token"`
		BaseURL     string `json:"baseurl"`
		IlinkBotID  string `json:"ilink_bot_id"`
		IlinkUserID string `json:"ilink_user_id"`
		Message     string `json:"errmsg"`
	}
	if err := c.getJSONWithClient(ctx, lpClient, c.baseURL, "/ilink/bot/get_qrcode_status?qrcode="+qrcode, &resp); err != nil {
		// A timeout while waiting for the scan is the normal "still pending"
		// signal for a blocking poll — surface it as pending, not an error, so
		// the install service keeps polling instead of aborting.
		if ctx.Err() != nil {
			return "", QRLoginResponse{}, ctx.Err()
		}
		if isTimeout(err) {
			return "pending", QRLoginResponse{}, nil
		}
		return "", QRLoginResponse{}, err
	}
	if resp.Status == "confirmed" || resp.BotToken != "" {
		login = QRLoginResponse{
			BotToken:    resp.BotToken,
			BaseURL:     resp.BaseURL,
			IlinkBotID:  resp.IlinkBotID,
			IlinkUserID: resp.IlinkUserID,
		}
		return "confirmed", login, nil
	}
	return "pending", QRLoginResponse{}, nil
}

// isTimeout reports whether err is an HTTP client timeout (client.Timeout
// exceeded). net/http wraps this as a *url.Error whose Timeout() is true.
func isTimeout(err error) bool {
	var timeoutErr interface{ Timeout() bool }
	if errors.As(err, &timeoutErr) {
		return timeoutErr.Timeout()
	}
	return false
}

// resetChannel asks the iLink backend to reset the bot's IM channel (used when
// the bot token is suspected stale / the account needs re-linking).
func (c *iLinkClient) resetChannel(ctx context.Context, botToken, baseURL string) error {
	body := map[string]any{}
	var resp struct {
		Ret     int    `json:"ret"`
		Message string `json:"errmsg"`
	}
	return c.postJSONAuthed(ctx, baseURL, "/ilink/bot/channel_reset", body, botToken, &resp)
}

// iLinkMessage is one inbound message as delivered by getupdates. The iLink
// payload nests content under item_list[].text_item.text and uses a numeric
// message_type; these are flattened here by parseUpdates.
type iLinkMessage struct {
	MsgID        string // the message id (msg_id)
	FromUserID   string // sender, e.g. "xxx@im.wechat"
	ToUserID     string // the bot id, e.g. "xxx@im.bot"
	GroupID      string // group id, when from a group
	MsgType      string // normalized: "text" or the raw numeric as string
	Content      string // text body
	ContextToken string // MUST be echoed back on sendmessage
	CreateTime   int64
}

// getUpdatesResult is the outcome of one getupdates long poll.
type getUpdatesResult struct {
	Messages   []iLinkMessage
	NextCursor string // get_updates_buf to use on the next call
}

// getUpdates long-polls the iLink backend for new messages. The server holds the
// connection open for ~35s when there are no messages; the caller's context
// MUST be honoured so lease loss / shutdown can interrupt the poll. cursor is the
// opaque get_updates_buf from the prior call (empty for the first call).
//
// iLink getupdates response shape: { ret, get_updates_buf, msgs: [ { msg_id,
// from_user_id, to_user_id, group_id (optional), message_type (numeric),
// context_token, item_list: [ { text_item: { text } } ], create_time } ] }.
func (c *iLinkClient) getUpdates(ctx context.Context, botToken, baseURL, cursor string) (getUpdatesResult, error) {
	body := map[string]any{}
	if cursor != "" {
		body["get_updates_buf"] = cursor
	}
	var resp struct {
		Ret           int           `json:"ret"`
		GetUpdatesBuf string        `json:"get_updates_buf"`
		Msgs          []rawIlinkMsg `json:"msgs"`
		Message       string        `json:"errmsg"`
	}
	if err := c.postJSONAuthedWithClient(ctx, &http.Client{Timeout: longPollTimeout}, baseURL, "/ilink/bot/getupdates", body, botToken, &resp); err != nil {
		// A client timeout on getupdates is the NORMAL long-poll expiry (no
		// messages arrived during the hold window). Surfacing it as an error
		// would make the supervisor tear down + reconnect on every empty poll
		// cycle. Treat it as an empty result and let the caller poll again.
		if ctx.Err() != nil {
			return getUpdatesResult{}, err
		}
		if isTimeout(err) {
			return getUpdatesResult{NextCursor: cursor, Messages: nil}, nil
		}
		return getUpdatesResult{}, err
	}
	out := getUpdatesResult{NextCursor: resp.GetUpdatesBuf}
	for _, m := range resp.Msgs {
		out.Messages = append(out.Messages, m.flatten())
	}
	return out, nil
}

// rawIlinkMsg mirrors one entry of the iLink getupdates msgs array before
// flattening. iLink does NOT expose a message id; dedup is by the server-driven
// get_updates_buf cursor (each message is delivered once per cursor advance).
// flatten synthesizes a stable id from the message's invariant fields so the
// engine's two-phase dedup (which keys on (installation, message_id)) still
// works as a reconnect-safety net.
type rawIlinkMsg struct {
	FromUserID   string `json:"from_user_id"`
	ToUserID     string `json:"to_user_id"`
	GroupID      string `json:"group_id"`
	MessageType  json.RawMessage `json:"message_type"` // numeric or string
	ContextToken string          `json:"context_token"`
	ItemList     []struct {
		Type     int `json:"type"`
		TextItem struct {
			Text string `json:"text"`
		} `json:"text_item"`
	} `json:"item_list"`
	CreateTime int64 `json:"create_time"`
}

// flatten normalizes a raw iLink msg into iLinkMessage: joins the text body from
// item_list, maps the numeric message_type to a readable string (1=text), and
// synthesizes a stable message id from the invariant fields.
func (m rawIlinkMsg) flatten() iLinkMessage {
	var body string
	for _, it := range m.ItemList {
		if it.TextItem.Text != "" {
			if body != "" {
				body += "\n"
			}
			body += it.TextItem.Text
		}
	}
	return iLinkMessage{
		MsgID:        synthesizeMsgID(m),
		FromUserID:   m.FromUserID,
		ToUserID:     m.ToUserID,
		GroupID:      m.GroupID,
		MsgType:      normalizeMsgType(m.MessageType),
		Content:      body,
		ContextToken: m.ContextToken,
		CreateTime:   m.CreateTime,
	}
}

// synthesizeMsgID builds a stable identifier for an iLink message from its
// invariant fields. iLink exposes no message id, so this hash of (from, to,
// context_token, create_time, body) serves as the dedup key. create_time gives
// per-message uniqueness; context_token binds it to the conversation. Two
// genuinely identical messages in the same second would collide, which is
// acceptable (dedup treats the second as a duplicate — a rare false positive
// preferable to the false negative of skipping real messages).
func synthesizeMsgID(m rawIlinkMsg) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%d", m.FromUserID, m.ToUserID, m.ContextToken, m.CreateTime)
	for _, it := range m.ItemList {
		fmt.Fprintf(h, "\x00%s", it.TextItem.Text)
	}
	sum := h.Sum(nil)
	// First 16 bytes hex = 32 chars, plenty of entropy for a dedup key and short
	// enough to fit comfortably in the dedup table's message_id column.
	return hex.EncodeToString(sum[:16])
}

// normalizeMsgType maps the iLink numeric message_type to a readable string.
// iLink uses 1=text; other numbers (image/voice/…) are kept as their numeric
// string so non-text is visible without a full enum table (the inbound path
// maps anything non-"text" to MsgTypeUnknown anyway).
func normalizeMsgType(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	// Try number first (the documented form).
	var n int
	if err := json.Unmarshal(raw, &n); err == nil {
		if n == 1 {
			return "text"
		}
		return strconv.Itoa(n)
	}
	// Fall back to a string value.
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	return ""
}

// sendMessage posts a text reply. contextToken MUST be the context_token of the
// inbound message being replied to, or the reply is not associated with the
// conversation (the core iLink quirk). toUserID is the destination WeChat user
// id ("xxx@im.wechat"). Returns the client_id of the delivered reply.
//
// iLink sendmessage body (per the community bridge source): the message is
// wrapped in a "msg" object with from_user_id empty (the bot), message_type=2
// (BOT-originated), message_state=2 (FINISH), a per-call client_id, and the
// item_list carrying type=1 (TEXT). Missing the msg wrapper / client_id /
// message_state yields ret=-2 (server rejects the malformed body).
func (c *iLinkClient) sendMessage(ctx context.Context, botToken, baseURL, contextToken, toUserID, text string) (string, error) {
	if contextToken == "" {
		return "", errors.New("wechat: sendmessage requires a non-empty context_token")
	}
	clientID := "multica-" + randomID()
	body := map[string]any{
		"msg": map[string]any{
			"from_user_id":  "",
			"to_user_id":    toUserID,
			"client_id":     clientID,
			"message_type":  2, // 2 = BOT-originated
			"message_state": 2, // 2 = FINISH
			"context_token": contextToken,
			"item_list": []map[string]any{
				{"type": 1, "text_item": map[string]any{"text": text}}, // type 1 = TEXT
			},
		},
	}
	var resp struct {
		Ret       int    `json:"ret"`
		MessageID string `json:"msg_id"`
		Message   string `json:"errmsg"`
	}
	if err := c.postJSONAuthed(ctx, baseURL, "/ilink/bot/sendmessage", body, botToken, &resp); err != nil {
		return "", err
	}
	if resp.Ret != 0 {
		c.logger.WarnContext(ctx, "wechat sendmessage rejected",
			"ret", resp.Ret, "errmsg", resp.Message,
			"to_user_id", toUserID, "base_url", baseURL)
		return clientID, fmt.Errorf("wechat: sendmessage rejected (ret=%d errmsg=%q)", resp.Ret, resp.Message)
	}
	c.logger.InfoContext(ctx, "wechat sendmessage ok",
		"ret", resp.Ret, "client_id", clientID, "to_user_id", toUserID)
	return clientID, nil
}

// getJSON is an unauthenticated GET (used by the QR-login flow, which runs
// before a bot_token exists).
func (c *iLinkClient) getJSON(ctx context.Context, baseURL, path string, out any) error {
	return c.getJSONWithClient(ctx, c.httpClient, baseURL, path, out)
}

// getJSONWithClient is an unauthenticated GET using the supplied HTTP client.
// Used by pollQRStatus which needs a longer timeout than the default short-call
// client (the status endpoint blocks until the user scans).
func (c *iLinkClient) getJSONWithClient(ctx context.Context, httpClient *http.Client, baseURL, path string, out any) error {
	if baseURL == "" {
		return errors.New("wechat: empty base url")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
	if err != nil {
		return fmt.Errorf("wechat: build GET %s: %w", path, err)
	}
	resp, err := httpClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("wechat: %s: %w", path, err)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("wechat: %s: read body: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wechat: %s: HTTP %d: %s", path, resp.StatusCode, truncate(string(respBody), 300))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("wechat: %s: decode response: %w", path, err)
		}
	}
	return nil
}

// postJSON is an unauthenticated POST (legacy helper; QR-login is GET now).
func (c *iLinkClient) postJSON(ctx context.Context, baseURL, path string, body any, out any) error {
	return c.postJSONAuthedWithClient(ctx, c.httpClient, baseURL, path, body, "", out)
}

// postJSONAuthed is a POST authenticated with a bot_token bearer header plus
// the iLink-specific headers (AuthorizationType, X-WECHAT-UIN).
func (c *iLinkClient) postJSONAuthed(ctx context.Context, baseURL, path string, body any, botToken string, out any) error {
	return c.postJSONAuthedWithClient(ctx, c.httpClient, baseURL, path, body, botToken, out)
}

// postJSONAuthedWithClient is the shared POST worker. It marshals body to JSON,
// attaches the iLink auth headers when botToken is non-empty, and unmarshals the
// JSON response into out.
func (c *iLinkClient) postJSONAuthedWithClient(ctx context.Context, httpClient *http.Client, baseURL, path string, body any, botToken string, out any) error {
	if baseURL == "" {
		return errors.New("wechat: empty base url")
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("wechat: marshal request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("wechat: build POST %s: %w", path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	if botToken != "" {
		req.Header.Set("Authorization", "Bearer "+botToken)
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("X-WECHAT-UIN", randomUIN())
	}
	return c.do(req, path, out)
}

// do executes a prepared request, decodes the JSON body into out, and maps HTTP
// errors. ctx cancellation is reported as ctx.Err so the Connect loop can tell
// graceful shutdown from a real transport failure.
func (c *iLinkClient) do(req *http.Request, path string, out any) error {
	resp, err := c.httpClient.Do(req)
	if resp != nil {
		defer resp.Body.Close()
	}
	if err != nil {
		if ctx := req.Context(); ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("wechat: %s: %w", path, err)
	}
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20)) // 1MiB cap; messages are text
	if err != nil {
		return fmt.Errorf("wechat: %s: read body: %w", path, err)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("wechat: %s: HTTP %d: %s", path, resp.StatusCode, truncate(string(respBody), 300))
	}
	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			return fmt.Errorf("wechat: %s: decode response: %w", path, err)
		}
	}
	return nil
}

// randomUIN returns the X-WECHAT-UIN header value: base64 of the DECIMAL string
// of a random uint32 (per the reverse-engineered protocol — NOT the raw bytes).
// rand failure yields a zero value ("MA==" for "0"), since the header is a
// nonce, not a credential.
func randomUIN() string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	v := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(v), 10)))
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// randomID returns a hex string suitable for a per-call client_id (16 random
// bytes). It uses crypto/rand so it is not guessable, matching the bridge's
// crypto.randomUUID() intent.
func randomID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
