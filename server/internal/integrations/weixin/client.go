package weixin

import (
	"bytes"
	"context"
	cryptorand "crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
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
	endpointGetQRCode   = "ilink/bot/get_bot_qrcode"
	endpointQRStatus    = "ilink/bot/get_qrcode_status"
	endpointGetUpdates  = "ilink/bot/getupdates"
	endpointSendMessage = "ilink/bot/sendmessage"
)

// BaseURLPolicy decides whether an iLink API base URL is trusted. Production
// callers should use the default policy. The seam exists so httptest servers
// can exercise the client without weakening the production allowlist.
type BaseURLPolicy func(*url.URL) bool

// ClientOptions configures the iLink protocol client. Zero values select the
// Tencent production endpoint and conservative timeouts.
type ClientOptions struct {
	BaseURL        string
	ILinkAppID     string
	ClientVersion  string
	ChannelVersion string
	BotAgent       string
	RequestTimeout time.Duration
	PollTimeout    time.Duration
	AllowBaseURL   BaseURLPolicy
}

// Client implements the personal-WeChat iLink JSON/HTTP protocol. It owns no
// credentials or receive cursor; both stay installation-scoped at the caller.
type Client struct {
	httpClient     *http.Client
	baseURL        string
	iLinkAppID     string
	clientVersion  string
	baseInfo       baseInfo
	requestTimeout time.Duration
	pollTimeout    time.Duration
	allowBaseURL   BaseURLPolicy
}

// NewClient constructs a client and rejects an unsafe configured base URL
// before any network request is possible.
func NewClient(httpClient *http.Client, opts ClientOptions) (*Client, error) {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	if opts.BaseURL == "" {
		opts.BaseURL = defaultBaseURL
	}
	if opts.ILinkAppID == "" {
		opts.ILinkAppID = defaultILinkAppID
	}
	if opts.ClientVersion == "" {
		opts.ClientVersion = defaultClientVersion
	}
	if opts.ChannelVersion == "" {
		opts.ChannelVersion = defaultChannelVersion
	}
	if opts.BotAgent == "" {
		opts.BotAgent = defaultBotAgent
	}
	if opts.RequestTimeout <= 0 {
		opts.RequestTimeout = defaultRequestTimeout
	}
	if opts.PollTimeout <= 0 {
		opts.PollTimeout = defaultPollTimeout
	}
	if opts.AllowBaseURL == nil {
		opts.AllowBaseURL = allowTencentBaseURL
	}

	c := &Client{
		httpClient:     httpClient,
		iLinkAppID:     opts.ILinkAppID,
		clientVersion:  opts.ClientVersion,
		baseInfo:       baseInfo{ChannelVersion: opts.ChannelVersion, BotAgent: opts.BotAgent},
		requestTimeout: opts.RequestTimeout,
		pollTimeout:    opts.PollTimeout,
		allowBaseURL:   opts.AllowBaseURL,
	}
	baseURL, err := c.validateBaseURL(opts.BaseURL)
	if err != nil {
		return nil, err
	}
	c.baseURL = baseURL
	return c, nil
}

// allowTencentBaseURL is intentionally narrower than "*.qq.com". QR status,
// API redirects, and media URLs are server-controlled input; accepting an
// arbitrary returned host would turn the integration into an SSRF primitive.
func allowTencentBaseURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "ilinkai.weixin.qq.com" || strings.HasSuffix(host, ".weixin.qq.com")
}

func (c *Client) validateBaseURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("weixin: parse base URL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("weixin: invalid base URL")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return "", errors.New("weixin: base URL must use HTTP or HTTPS")
	}
	if u.Path != "" && u.Path != "/" {
		return "", errors.New("weixin: base URL must not contain a path")
	}
	if c.allowBaseURL == nil || !c.allowBaseURL(u) {
		return "", fmt.Errorf("weixin: untrusted base URL host %q", u.Hostname())
	}
	u.Path = ""
	return strings.TrimRight(u.String(), "/"), nil
}

// RedirectBaseURL validates an IDC redirect_host and returns the HTTPS base
// URL that can be supplied to PollQRCode. The host must not contain a scheme,
// credentials, path, query, fragment, or port.
func (c *Client) RedirectBaseURL(host string) (string, error) {
	host = strings.TrimSpace(host)
	if host == "" || strings.ContainsAny(host, "/?#@:") {
		return "", errors.New("weixin: invalid QR redirect host")
	}
	return c.validateBaseURL("https://" + host)
}

// BeginQRCode starts a QR authorization session. The returned Code is an
// opaque status-poll token; ImageContent is what the frontend renders.
func (c *Client) BeginQRCode(ctx context.Context, localTokens []string) (QRCode, error) {
	query := url.Values{"bot_type": []string{defaultBotType}}
	var out QRCode
	err := c.doJSON(ctx, request{
		method:  http.MethodPost,
		baseURL: c.baseURL,
		path:    endpointGetQRCode,
		query:   query,
		body: map[string]any{
			"local_token_list": localTokens,
		},
		timeout: c.requestTimeout,
	}, &out)
	if err != nil {
		return QRCode{}, err
	}
	if strings.TrimSpace(out.Code) == "" || strings.TrimSpace(out.ImageContent) == "" {
		return QRCode{}, errors.New("weixin: QR response is incomplete")
	}
	return out, nil
}

// PollQRCode performs one long-poll status request. verifyCode is included only
// after the server reports need_verifycode. baseURL starts at the fixed Tencent
// host and may change only through RedirectBaseURL.
func (c *Client) PollQRCode(ctx context.Context, baseURL, code, verifyCode string) (QRStatus, error) {
	if strings.TrimSpace(code) == "" {
		return QRStatus{}, errors.New("weixin: QR code token is required")
	}
	if baseURL == "" {
		baseURL = c.baseURL
	}
	validated, err := c.validateBaseURL(baseURL)
	if err != nil {
		return QRStatus{}, err
	}
	query := url.Values{"qrcode": []string{code}}
	if verifyCode != "" {
		query.Set("verify_code", verifyCode)
	}
	var out QRStatus
	err = c.doJSON(ctx, request{
		method:  http.MethodGet,
		baseURL: validated,
		path:    endpointQRStatus,
		query:   query,
		timeout: c.pollTimeout,
	}, &out)
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return QRStatus{Status: "wait"}, nil
		}
		return QRStatus{}, err
	}
	if err := validateQRStatus(out); err != nil {
		return QRStatus{}, err
	}
	if out.confirmed() {
		accountBaseURL, err := c.validateBaseURL(out.BaseURL)
		if err != nil {
			return QRStatus{}, fmt.Errorf("weixin: rejected confirmed account base URL: %w", err)
		}
		out.BaseURL = accountBaseURL
	}
	if out.Status == "scaned_but_redirect" && out.RedirectHost != "" {
		if _, err := c.RedirectBaseURL(out.RedirectHost); err != nil {
			return QRStatus{}, fmt.Errorf("weixin: rejected QR redirect: %w", err)
		}
	}
	return out, nil
}

// GetUpdates long-polls one installation. An internal client-side timeout is a
// normal empty poll and returns the caller's cursor unchanged. Parent-context
// cancellation remains an error so a supervised receive loop exits promptly.
func (c *Client) GetUpdates(ctx context.Context, baseURL, token, cursor string, timeout time.Duration) (Updates, error) {
	if strings.TrimSpace(token) == "" {
		return Updates{}, errors.New("weixin: bot token is required")
	}
	validated, err := c.validateBaseURL(baseURL)
	if err != nil {
		return Updates{}, err
	}
	if timeout <= 0 {
		timeout = c.pollTimeout
	}
	var out Updates
	err = c.doJSON(ctx, request{
		method:  http.MethodPost,
		baseURL: validated,
		path:    endpointGetUpdates,
		body: getUpdatesRequest{
			Cursor:   cursor,
			BaseInfo: c.baseInfo,
		},
		token:   token,
		timeout: timeout,
	}, &out)
	if err != nil {
		if ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded) {
			return Updates{Cursor: cursor}, nil
		}
		return Updates{}, err
	}
	if out.Ret != 0 || out.ErrCode != 0 {
		return Updates{}, &APIError{
			Operation: "getupdates",
			Ret:       out.Ret,
			ErrCode:   out.ErrCode,
			Message:   out.ErrorMessage,
		}
	}
	return out, nil
}

// SendTextInput is one reply-driven personal-WeChat text delivery. ClientID may
// be supplied by a durable outbound worker and must be reused when retrying an
// ambiguous network failure.
type SendTextInput struct {
	BaseURL      string
	Token        string
	ToUserID     string
	ContextToken string
	Text         string
	RunID        string
	ClientID     string
}

// SendText posts one finished bot message and validates the protocol-level ret
// field. An HTTP 200 with ret != 0 is a delivery failure.
func (c *Client) SendText(ctx context.Context, in SendTextInput) (string, error) {
	if strings.TrimSpace(in.Token) == "" {
		return "", errors.New("weixin: bot token is required")
	}
	if strings.TrimSpace(in.ToUserID) == "" {
		return "", errors.New("weixin: destination user is required")
	}
	if strings.TrimSpace(in.ContextToken) == "" {
		return "", errors.New("weixin: context token is required")
	}
	if in.Text == "" {
		return "", errors.New("weixin: outbound text is required")
	}
	validated, err := c.validateBaseURL(in.BaseURL)
	if err != nil {
		return "", err
	}
	clientID := in.ClientID
	if clientID == "" {
		clientID, err = randomClientID()
		if err != nil {
			return "", err
		}
	}
	var out apiResponse
	err = c.doJSON(ctx, request{
		method:  http.MethodPost,
		baseURL: validated,
		path:    endpointSendMessage,
		token:   in.Token,
		timeout: c.requestTimeout,
		body: sendMessageRequest{
			Message: Message{
				ToUserID:     in.ToUserID,
				ClientID:     clientID,
				MessageType:  messageTypeBot,
				MessageState: messageStateFinish,
				ContextToken: in.ContextToken,
				RunID:        in.RunID,
				Items: []MessageItem{{
					Type: messageItemTypeText,
					Text: &TextItem{Text: in.Text},
				}},
			},
			BaseInfo: c.baseInfo,
		},
	}, &out)
	if err != nil {
		return clientID, err
	}
	if out.Ret != 0 || out.ErrCode != 0 {
		return clientID, &APIError{
			Operation: "sendmessage",
			Ret:       out.Ret,
			ErrCode:   out.ErrCode,
			Message:   out.ErrorMessage,
		}
	}
	return clientID, nil
}

type request struct {
	method  string
	baseURL string
	path    string
	query   url.Values
	body    any
	token   string
	timeout time.Duration
}

func (c *Client) doJSON(parent context.Context, in request, out any) error {
	if parent == nil {
		return errors.New("weixin: nil request context")
	}
	endpoint, err := url.Parse(in.baseURL)
	if err != nil {
		return fmt.Errorf("weixin: parse request base URL: %w", err)
	}
	endpoint.Path = strings.TrimRight(endpoint.Path, "/") + "/" + strings.TrimLeft(in.path, "/")
	endpoint.RawQuery = in.query.Encode()

	var body io.Reader
	if in.body != nil {
		payload, err := json.Marshal(in.body)
		if err != nil {
			return fmt.Errorf("weixin: encode %s request: %w", in.path, err)
		}
		body = bytes.NewReader(payload)
	}
	ctx := parent
	cancel := func() {}
	if in.timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, in.timeout)
	}
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, in.method, endpoint.String(), body)
	if err != nil {
		return fmt.Errorf("weixin: build %s request: %w", in.path, err)
	}
	req.Header.Set("iLink-App-Id", c.iLinkAppID)
	req.Header.Set("iLink-App-ClientVersion", c.clientVersion)
	if in.body != nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("AuthorizationType", "ilink_bot_token")
		req.Header.Set("X-WECHAT-UIN", randomWechatUIN())
	}
	if in.token != "" {
		req.Header.Set("Authorization", "Bearer "+in.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("weixin: %s request failed: %w", in.path, err)
	}
	defer resp.Body.Close()

	payload, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		return fmt.Errorf("weixin: read %s response: %w", in.path, err)
	}
	if len(payload) > maxResponseBytes {
		return fmt.Errorf("weixin: %s response exceeds %d bytes", in.path, maxResponseBytes)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("weixin: %s returned HTTP %d", in.path, resp.StatusCode)
	}
	if out == nil || len(bytes.TrimSpace(payload)) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("weixin: decode %s response: %w", in.path, err)
	}
	return nil
}

// randomWechatUIN is base64(decimal(random uint32)), matching Tencent's
// reference client. It is a request nonce, not a credential.
func randomWechatUIN() string {
	var b [4]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return base64.StdEncoding.EncodeToString([]byte("0"))
	}
	v := binary.BigEndian.Uint32(b[:])
	return base64.StdEncoding.EncodeToString([]byte(strconv.FormatUint(uint64(v), 10)))
}

func randomClientID() (string, error) {
	var b [16]byte
	if _, err := cryptorand.Read(b[:]); err != nil {
		return "", fmt.Errorf("weixin: generate client id: %w", err)
	}
	return "multica-" + hex.EncodeToString(b[:]), nil
}

// ChunkText splits a reply on rune boundaries. The iLink reference adapter
// uses 4,000 characters as its conservative text-message limit.
func ChunkText(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 4000
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return []string{text}
	}
	out := make([]string, 0, (len(runes)+maxRunes-1)/maxRunes)
	for len(runes) > 0 {
		n := min(maxRunes, len(runes))
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}
