package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Real Lark/飞书 Open Platform HTTP APIClient.
//
// Scope: tenant_access_token acquisition + caching, IM v1 interactive-
// card send / patch, the dedicated binding-prompt outbound, AND the
// install-time Bot identity lookup (/open-apis/bot/v3/info) consumed
// by RegistrationService right after a successful device-flow grant.
// The PersonalAgent registration protocol itself is a separate client
// (RegistrationClient) because it speaks to a different host
// (accounts.feishu.cn) with a different auth model (no
// tenant_access_token — the response IS the credentials).
//
// Per-installation credentials flow in on each call via
// InstallationCredentials; the client never reads lark_installation
// directly. tenant_access_token is cached in-process keyed by app_id,
// honoring Lark's `expire` field minus a safety margin so callers
// never present a token that's about to lapse mid-flight.

const (
	// defaultLarkBaseURL is the mainland 飞书 open-platform host. It is the
	// fallback host for an installation whose region is feishu (or unset);
	// Region.OpenPlatformBaseURL maps region=lark to open.larksuite.com.
	// Operators do NOT set MULTICA_LARK_HTTP_BASE_URL to pick a cloud
	// anymore — the per-installation region does that automatically. The
	// env var remains only as a deployment-wide override (proxy / mock /
	// single-cloud staging); tests substitute an httptest.Server URL.
	defaultLarkBaseURL = "https://open.feishu.cn"

	// tokenSafetyMargin is subtracted from Lark's `expire` so we
	// refresh before a token actually lapses. 60s comfortably exceeds
	// any in-flight HTTP timeout we set below.
	tokenSafetyMargin = 60 * time.Second

	// defaultRequestTimeout is the per-call HTTP timeout. Lark's API
	// is normally well under 1s; we leave headroom for cross-region
	// latency from a self-hosted Multica deployment to feishu.cn.
	defaultRequestTimeout = 10 * time.Second

	// Lark's "invalid tenant_access_token" / "tenant_access_token
	// expired" error codes. When we see either, drop the cached token
	// so the next call refreshes from /tenant_access_token/internal.
	// 99991663 = expired, 99991664 = invalid. Documented at:
	// open.feishu.cn/document/server-docs/api-call-guide/server-error-codes.
	codeTokenExpired = 99991663
	codeTokenInvalid = 99991664
)

// HTTPClientConfig configures the production Lark HTTP APIClient.
type HTTPClientConfig struct {
	// BaseURL is an optional deployment-wide override for the Lark
	// open-platform root, e.g. "https://open.feishu.cn" or
	// "https://open.larksuite.com". When set it forces every call —
	// regardless of the installation's region — to that host; tests set
	// it to an httptest.Server URL. When EMPTY (the production default),
	// each call resolves its host from InstallationCredentials.Region so
	// a single deployment serves both Feishu and Lark. Trailing "/" is
	// stripped.
	BaseURL string

	// HTTPClient is the transport used for every outbound call. Tests
	// substitute an *http.Client whose Transport routes to an
	// httptest.Server. Empty defaults to a fresh http.Client with
	// defaultRequestTimeout.
	HTTPClient *http.Client

	// Now is overridable for deterministic token-expiry tests.
	Now func() time.Time

	// Logger receives warnings about Lark error codes. Nil uses
	// slog.Default().
	Logger *slog.Logger
}

func (c HTTPClientConfig) withDefaults() HTTPClientConfig {
	// BaseURL is intentionally NOT defaulted to defaultLarkBaseURL here.
	// An empty BaseURL means "no deployment-wide override" — each call
	// then resolves its host from InstallationCredentials.Region (see
	// resolveBaseURL), so one client serves both Feishu and Lark. A
	// non-empty BaseURL (MULTICA_LARK_HTTP_BASE_URL, or an httptest URL
	// in tests) forces every region to that host.
	c.BaseURL = strings.TrimRight(c.BaseURL, "/")
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Logger == nil {
		c.Logger = slog.Default()
	}
	return c
}

// NewHTTPAPIClient constructs the real APIClient that speaks to Lark's
// open platform over HTTPS. Per-installation credentials flow in via
// each call's InstallationCredentials parameter; tokens are cached
// keyed by app_id so a single Multica server reuses Lark's
// tenant_access_token across calls to the same app.
func NewHTTPAPIClient(cfg HTTPClientConfig) APIClient {
	cfg = cfg.withDefaults()
	return &httpAPIClient{cfg: cfg, tokens: make(map[string]*cachedToken)}
}

type httpAPIClient struct {
	cfg HTTPClientConfig

	mu sync.Mutex
	// tokens caches tenant_access_token keyed by app_id only — NOT by
	// (app_id, region). This is safe because a Lark/飞书 app_id (the
	// "cli_..." credential) is globally unique across both clouds and an
	// app exists on exactly one of them, so an app_id never maps to two
	// regions. The DB enforces the same assumption with UNIQUE(app_id) on
	// lark_installation. If Lark ever reused an app_id across clouds, both
	// this cache key and that constraint would need region added.
	tokens map[string]*cachedToken
}

type cachedToken struct {
	value     string
	expiresAt time.Time
}

// IsConfigured reports true: once this client exists at all, the
// outbound transport path (send / patch / binding prompt / bot info)
// is wired. The stub returns false because every call there errors
// with ErrAPIClientNotConfigured; the real client is the inverse
// contract.
func (c *httpAPIClient) IsConfigured() bool { return true }

// tenantAccessToken returns a usable tenant_access_token for the
// given installation, reusing a cached token while it is alive (minus
// safety margin) and otherwise fetching a fresh one from Lark.
//
// Concurrent callers serialize on the per-client mutex during the
// uncached path; the cached path takes the mutex only for the lookup
// and releases before doing any I/O. Steady-state contention is
// therefore one map-read under the lock, not a per-call HTTP round
// trip.
func (c *httpAPIClient) tenantAccessToken(ctx context.Context, creds InstallationCredentials) (string, error) {
	if creds.AppID == "" {
		return "", errors.New("lark http client: missing app_id")
	}
	if creds.AppSecret == "" {
		return "", errors.New("lark http client: missing app_secret")
	}

	now := c.cfg.Now()
	c.mu.Lock()
	if t, ok := c.tokens[creds.AppID]; ok && t.expiresAt.After(now) {
		val := t.value
		c.mu.Unlock()
		return val, nil
	}
	c.mu.Unlock()

	// Self-built (internal) app endpoint. Marketplace / multi-tenant
	// apps would use /tenant_access_token/v3 with a different body
	// shape; PersonalAgent in this MVP is per-workspace self-built so
	// we stay on /internal.
	body := map[string]string{
		"app_id":     creds.AppID,
		"app_secret": creds.AppSecret,
	}
	var resp struct {
		Code              int    `json:"code"`
		Msg               string `json:"msg"`
		TenantAccessToken string `json:"tenant_access_token"`
		Expire            int64  `json:"expire"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodPost, "/open-apis/auth/v3/tenant_access_token/internal", "", body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: tenant_access_token: %w", err)
	}
	if resp.Code != 0 || resp.TenantAccessToken == "" {
		return "", fmt.Errorf("lark http client: tenant_access_token: code=%d msg=%q", resp.Code, resp.Msg)
	}

	expire := time.Duration(resp.Expire) * time.Second
	// Clamp to >= 2× safety margin so a misbehaving upstream that
	// returns a sub-minute expire never makes us cache a token that
	// is already past its safe window.
	if expire < tokenSafetyMargin*2 {
		expire = tokenSafetyMargin * 2
	}
	expiresAt := c.cfg.Now().Add(expire - tokenSafetyMargin)

	c.mu.Lock()
	c.tokens[creds.AppID] = &cachedToken{value: resp.TenantAccessToken, expiresAt: expiresAt}
	c.mu.Unlock()

	return resp.TenantAccessToken, nil
}

// resolveBaseURL picks the open-platform host for one call. An explicit
// cfg.BaseURL (MULTICA_LARK_HTTP_BASE_URL, or an httptest URL in tests)
// overrides every region and routes all traffic there. With no override,
// the host comes from the installation's region, so Feishu and Lark
// installations served by the same process each reach their own cloud.
func (c *httpAPIClient) resolveBaseURL(creds InstallationCredentials) string {
	if c.cfg.BaseURL != "" {
		return c.cfg.BaseURL
	}
	return creds.Region.OpenPlatformBaseURL()
}

// invalidateToken drops the cached token for an app_id. Called when
// Lark surfaces an expired / invalid token error code so the next
// call refreshes instead of looping on a stale entry.
func (c *httpAPIClient) invalidateToken(appID string) {
	c.mu.Lock()
	delete(c.tokens, appID)
	c.mu.Unlock()
}

// outboundMessageRequest builds the (path, body) the three send methods
// share. When target.IsSet() the message is routed through Lark's reply
// endpoint (POST /im/v1/messages/{message_id}/reply) so it threads back
// into the originating 话题 — reply_in_thread carries the target's
// InThread flag (Lark also keeps the reply in-thread automatically when
// the parent message already belongs to a thread). Otherwise the message
// goes to the chat-level send endpoint keyed by receive_id=chat_id, the
// historical behavior. Body is map[string]any (not map[string]string)
// because reply_in_thread is a bool.
func outboundMessageRequest(chatID ChatID, msgType, content string, target ReplyTarget) (string, map[string]any) {
	if target.IsSet() {
		return "/open-apis/im/v1/messages/" + url.PathEscape(target.MessageID) + "/reply", map[string]any{
			"msg_type":        msgType,
			"content":         content,
			"reply_in_thread": target.InThread,
		}
	}
	q := url.Values{}
	q.Set("receive_id_type", "chat_id")
	return "/open-apis/im/v1/messages?" + q.Encode(), map[string]any{
		"receive_id": string(chatID),
		"msg_type":   msgType,
		"content":    content,
	}
}

// SendInteractiveCard posts a fresh interactive card into a chat and
// returns Lark's message_id so the Patcher can target subsequent
// patches at the same card.
func (c *httpAPIClient) SendInteractiveCard(ctx context.Context, p SendCardParams) (string, error) {
	if p.ChatID == "" {
		return "", errors.New("lark http client: missing chat_id")
	}
	if p.CardJSON == "" {
		return "", errors.New("lark http client: missing card json")
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return "", err
	}
	path, body := outboundMessageRequest(p.ChatID, "interactive", p.CardJSON, p.ReplyTarget)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodPost, path, token, body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: send interactive card: %w", err)
	}
	if resp.Code != 0 || resp.Data.MessageID == "" {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return "", &APIError{Op: "send interactive card", Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.MessageID, nil
}

// SendTextMessage posts a plain text IM message into a Lark chat.
// This is the Patcher's primary outbound for agent chat replies —
// using a normal text bubble instead of an interactive card makes
// free-form replies feel like a native Lark conversation. The
// content envelope Lark expects is a JSON-encoded `{"text": "..."}`
// blob; we encode it here so callers pass raw text.
func (c *httpAPIClient) SendTextMessage(ctx context.Context, p SendTextParams) (string, error) {
	if p.ChatID == "" {
		return "", errors.New("lark http client: missing chat_id")
	}
	if p.Text == "" {
		return "", errors.New("lark http client: missing text")
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return "", err
	}
	// Lark's `text` msg_type expects content = JSON-encoded {"text": "..."}.
	// json.Marshal handles the escape of newlines / quotes / unicode so
	// the agent's reply round-trips intact.
	contentBytes, err := json.Marshal(map[string]string{"text": p.Text})
	if err != nil {
		return "", fmt.Errorf("lark http client: encode text content: %w", err)
	}
	path, body := outboundMessageRequest(p.ChatID, "text", string(contentBytes), p.ReplyTarget)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodPost, path, token, body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: send text message: %w", err)
	}
	if resp.Code != 0 || resp.Data.MessageID == "" {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return "", &APIError{Op: "send text message", Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.MessageID, nil
}

// SendMarkdownCard posts the agent's reply as an interactive card
// using Lark's schema-2.0 envelope with a single `tag: "markdown"`
// body element. Lark's client renders the markdown into formatted
// text (bold, italics, lists, links, fenced code blocks, tables, …)
// rather than showing raw markdown characters as it does for
// `msg_type=text`. We deliberately keep `SendTextMessage` as a
// separate path for plain-prose replies — a card around a one-line
// "Hello!" adds visual chrome that the user doesn't want; the
// routing decision (markdown vs text) lives at the Patcher layer.
//
// Why schema 2.0 rather than the legacy schema with a `div` +
// `lark_md` text element: the legacy `lark_md` tag's markdown
// dialect is much narrower — no fenced code blocks (syntax
// highlighting), no tables, no heading sizes. Schema-2.0's
// `markdown` tag is closer to GFM.
func (c *httpAPIClient) SendMarkdownCard(ctx context.Context, p SendMarkdownCardParams) (string, error) {
	if p.ChatID == "" {
		return "", errors.New("lark http client: missing chat_id")
	}
	if p.Markdown == "" {
		return "", errors.New("lark http client: missing markdown body")
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return "", err
	}
	card := map[string]any{
		"schema": "2.0",
		"body": map[string]any{
			"elements": []any{
				map[string]any{"tag": "markdown", "content": p.Markdown},
			},
		},
	}
	if p.Summary != "" {
		card["config"] = map[string]any{
			"summary": map[string]any{"content": p.Summary},
		}
	}
	cardBytes, err := json.Marshal(card)
	if err != nil {
		return "", fmt.Errorf("lark http client: encode markdown card: %w", err)
	}
	path, body := outboundMessageRequest(p.ChatID, "interactive", string(cardBytes), p.ReplyTarget)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			MessageID string `json:"message_id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodPost, path, token, body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: send markdown card: %w", err)
	}
	if resp.Code != 0 || resp.Data.MessageID == "" {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return "", &APIError{Op: "send markdown card", Code: resp.Code, Msg: resp.Msg}
	}
	return resp.Data.MessageID, nil
}

// PatchInteractiveCard updates an existing card's body. Lark's
// message-patch endpoint replaces the whole card payload; callers
// (i.e. the Patcher) render the full updated card each time.
func (c *httpAPIClient) PatchInteractiveCard(ctx context.Context, p PatchCardParams) error {
	if p.LarkCardMessageID == "" {
		return errors.New("lark http client: missing card message id")
	}
	if p.CardJSON == "" {
		return errors.New("lark http client: missing card json")
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	body := map[string]string{"content": p.CardJSON}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	path := "/open-apis/im/v1/messages/" + url.PathEscape(p.LarkCardMessageID)
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodPatch, path, token, body, &resp); err != nil {
		return fmt.Errorf("lark http client: patch interactive card: %w", err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return fmt.Errorf("lark http client: patch interactive card: code=%d msg=%q", resp.Code, resp.Msg)
	}
	return nil
}

// SendBindingPromptCard renders the member-binding card and posts it
// directly to the unbound user's open_id (not the chat). Keeping the
// card template inside this client — rather than the dispatcher —
// means the dispatcher never has to know about Lark's card schema.
func (c *httpAPIClient) SendBindingPromptCard(ctx context.Context, p BindingPromptParams) error {
	if p.OpenID == "" {
		return errors.New("lark http client: missing open_id")
	}
	if p.BindURL == "" {
		return errors.New("lark http client: missing bind url")
	}
	cardJSON, err := bindingPromptTemplate(p.BindURL)
	if err != nil {
		return fmt.Errorf("lark http client: render binding prompt: %w", err)
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("receive_id_type", "open_id")
	body := map[string]string{
		"receive_id": string(p.OpenID),
		"msg_type":   "interactive",
		"content":    cardJSON,
	}
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	path := "/open-apis/im/v1/messages?" + q.Encode()
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodPost, path, token, body, &resp); err != nil {
		return fmt.Errorf("lark http client: send binding prompt: %w", err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return fmt.Errorf("lark http client: send binding prompt: code=%d msg=%q", resp.Code, resp.Msg)
	}
	return nil
}

// GetBotInfo calls /open-apis/bot/v3/info to learn the Bot's
// per-installation `open_id` and then /open-apis/contact/v3/users/
// {open_id}?user_id_type=open_id to resolve its stable `union_id`.
// RegistrationService is the only caller — right after the device-
// flow registration returns fresh `client_id` / `client_secret`, the
// service mints a tenant_access_token with those creds and calls
// this method so the installation row can be frozen with both Bot
// identifiers in the same transaction as the installer-bind.
//
// Why two API calls instead of one: /bot/v3/info does not return
// union_id in the public schema. The WS inbound decoder needs
// union_id to disambiguate which bot was @-mentioned in a multi-bot
// group chat (the per-app open_id field on mentions is structurally
// inverse across WS perspectives — see MUL-2671 triage), so we
// invest one extra HTTP round-trip at install time to capture it
// and avoid running the wrong supervisor for every event going
// forward.
//
// A missing union_id (contact lookup denied by app scope, or Lark
// returns an empty field) is NOT a hard failure here — the
// installation is still usable for p2p chats and the decoder can
// fall back to the (broken) open_id match path until the operator
// fixes scopes. We log a warning so the gap is visible.
//
// Other fields the upstream APIs return (display name, avatar, IP
// whitelist) are deliberately dropped; downstream reads can fetch
// them on demand from the bot_open_id, and freezing them into our
// schema would create a drift surface every time the operator edits
// the Bot on Lark's side.
func (c *httpAPIClient) GetBotInfo(ctx context.Context, creds InstallationCredentials) (BotInfo, error) {
	if creds.AppID == "" || creds.AppSecret == "" {
		return BotInfo{}, errors.New("lark http client: missing app credentials for GetBotInfo")
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return BotInfo{}, err
	}
	var botResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Bot  struct {
			OpenID string `json:"open_id"`
		} `json:"bot"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodGet, "/open-apis/bot/v3/info", token, nil, &botResp); err != nil {
		return BotInfo{}, fmt.Errorf("lark http client: bot info: %w", err)
	}
	if botResp.Code != 0 {
		if isTokenError(botResp.Code) {
			c.invalidateToken(creds.AppID)
		}
		return BotInfo{}, fmt.Errorf("lark http client: bot info: code=%d msg=%q", botResp.Code, botResp.Msg)
	}
	if botResp.Bot.OpenID == "" {
		return BotInfo{}, errors.New("lark http client: bot info: response missing open_id")
	}

	// Resolve union_id via the contact endpoint. Soft-fail: log and
	// return the BotInfo with empty UnionID. Callers (Registration-
	// Service.finishSuccess) accept the gap and persist what they
	// have.
	unionID, lookupErr := c.fetchBotUnionID(ctx, c.resolveBaseURL(creds), creds.AppID, token, botResp.Bot.OpenID)
	if lookupErr != nil {
		c.cfg.Logger.Warn("lark http client: bot union_id lookup failed; continuing without it",
			"app_id", creds.AppID,
			"bot_open_id", botResp.Bot.OpenID,
			"err", lookupErr)
	}
	return BotInfo{OpenID: OpenID(botResp.Bot.OpenID), UnionID: unionID}, nil
}

// GetMessage retrieves a message by id via
// GET /open-apis/im/v1/messages/{message_id}. The endpoint always wraps
// the result in data.items[] — one element for a normal message, and a
// forward sentinel followed by the bundled child messages for a
// `merge_forward`. We pass user_id_type=open_id so sender.id and
// mentions[].id come back as open_ids, matching the identifiers the
// rest of the package keys on.
//
// body.content is forwarded verbatim (the raw, JSON-encoded, msg_type-
// specific string Lark double-encodes); the enricher's flattener owns
// interpreting it. A deleted / out-of-scope message surfaces as a Lark
// error code, which we turn into a normal Go error so the enricher can
// degrade to its "[unable to fetch]" placeholder without aborting the
// inbound pipeline.
func (c *httpAPIClient) GetMessage(ctx context.Context, creds InstallationCredentials, messageID string) ([]LarkMessage, error) {
	if messageID == "" {
		return nil, errors.New("lark http client: missing message_id")
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("user_id_type", "open_id")
	path := "/open-apis/im/v1/messages/" + url.PathEscape(messageID) + "?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []larkRESTMessageItem `json:"items"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodGet, path, token, nil, &resp); err != nil {
		return nil, fmt.Errorf("lark http client: get message: %w", err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(creds.AppID)
		}
		return nil, fmt.Errorf("lark http client: get message: code=%d msg=%q", resp.Code, resp.Msg)
	}

	out := make([]LarkMessage, 0, len(resp.Data.Items))
	for _, it := range resp.Data.Items {
		out = append(out, it.normalize())
	}
	return out, nil
}

// larkListMessagesMaxPageSize is Lark's hard cap on a single
// im/v1/messages page. We clamp to it so a caller asking for more
// silently gets the max rather than a 400 from Lark.
const larkListMessagesMaxPageSize = 50

// ListChatMessages retrieves a bounded, recent window of messages in one
// chat via GET /open-apis/im/v1/messages?container_id_type=chat. Where
// GetMessage fetches a single message by id, this lists a conversation;
// it backs the enricher's group-context prefetch. We pass
// sort_type=ByCreateTimeDesc so the newest messages come first and a
// small page_size captures "the last N" without paginating, keeping the
// inbound ACK path's fan-out to a single round-trip. user_id_type=open_id
// matches the identifiers the rest of the package keys on; body.content
// is forwarded verbatim for the enricher's flattener to interpret.
func (c *httpAPIClient) ListChatMessages(ctx context.Context, creds InstallationCredentials, p ListMessagesParams) ([]LarkMessage, error) {
	if p.ChatID == "" {
		return nil, errors.New("lark http client: missing chat_id")
	}
	size := p.PageSize
	if size <= 0 {
		size = 1
	} else if size > larkListMessagesMaxPageSize {
		size = larkListMessagesMaxPageSize
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("container_id_type", "chat")
	q.Set("container_id", string(p.ChatID))
	q.Set("sort_type", "ByCreateTimeDesc")
	q.Set("page_size", strconv.Itoa(size))
	q.Set("user_id_type", "open_id")
	if p.EndTime > 0 {
		q.Set("end_time", strconv.FormatInt(p.EndTime, 10))
	}
	path := "/open-apis/im/v1/messages?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []larkRESTMessageItem `json:"items"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodGet, path, token, nil, &resp); err != nil {
		return nil, fmt.Errorf("lark http client: list chat messages: %w", err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(creds.AppID)
		}
		return nil, fmt.Errorf("lark http client: list chat messages: code=%d msg=%q", resp.Code, resp.Msg)
	}

	out := make([]LarkMessage, 0, len(resp.Data.Items))
	for _, it := range resp.Data.Items {
		out = append(out, it.normalize())
	}
	return out, nil
}

// larkBatchGetUsersMaxIDs is Lark's hard cap on user_ids per
// contact/v3/users/batch call. We drop the overflow rather than error so
// a caller asking for more still gets the first 50 resolved.
const larkBatchGetUsersMaxIDs = 50

// AddMessageReaction adds an emoji reaction to a message via
// POST /open-apis/im/v1/messages/{message_id}/reactions.
// Returns the reaction_id so it can be deleted later.
func (c *httpAPIClient) AddMessageReaction(ctx context.Context, p AddReactionParams) (string, error) {
	if p.MessageID == "" {
		return "", errors.New("lark http client: missing message_id")
	}
	if p.EmojiType == "" {
		return "", errors.New("lark http client: missing emoji_type")
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return "", err
	}
	body := map[string]any{
		"reaction_type": map[string]string{"emoji_type": p.EmojiType},
	}
	path := "/open-apis/im/v1/messages/" + url.PathEscape(p.MessageID) + "/reactions"
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			ReactionID string `json:"reaction_id"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodPost, path, token, body, &resp); err != nil {
		return "", fmt.Errorf("lark http client: add message reaction: %w", err)
	}
	if resp.Code != 0 || resp.Data.ReactionID == "" {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return "", fmt.Errorf("lark http client: add message reaction: code=%d msg=%q", resp.Code, resp.Msg)
	}
	return resp.Data.ReactionID, nil
}

// DeleteMessageReaction removes a reaction from a message via
// DELETE /open-apis/im/v1/messages/{message_id}/reactions/{reaction_id}.
func (c *httpAPIClient) DeleteMessageReaction(ctx context.Context, p DeleteReactionParams) error {
	if p.MessageID == "" {
		return errors.New("lark http client: missing message_id")
	}
	if p.ReactionID == "" {
		return errors.New("lark http client: missing reaction_id")
	}
	token, err := c.tenantAccessToken(ctx, p.InstallationID)
	if err != nil {
		return err
	}
	path := "/open-apis/im/v1/messages/" + url.PathEscape(p.MessageID) + "/reactions/" + url.PathEscape(p.ReactionID)
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(p.InstallationID), http.MethodDelete, path, token, nil, &resp); err != nil {
		return fmt.Errorf("lark http client: delete message reaction: %w", err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(p.InstallationID.AppID)
		}
		return fmt.Errorf("lark http client: delete message reaction: code=%d msg=%q", resp.Code, resp.Msg)
	}
	return nil
}

// BatchGetUsers resolves user open_ids to display names via
// GET /open-apis/contact/v3/users/batch?user_ids=…&user_id_type=open_id.
// It mirrors fetchBotUnionID's single-user contact lookup, batched. Only
// id->name pairs the API actually returns are included; a restricted
// contact scope or an unknown id simply yields a smaller map (code==0
// with fewer items), never an error, so the enricher degrades to
// positional speaker labels. Ids past Lark's 50-per-call cap are dropped.
func (c *httpAPIClient) BatchGetUsers(ctx context.Context, creds InstallationCredentials, openIDs []string) (map[string]string, error) {
	if len(openIDs) == 0 {
		return map[string]string{}, nil
	}
	if len(openIDs) > larkBatchGetUsersMaxIDs {
		openIDs = openIDs[:larkBatchGetUsersMaxIDs]
	}
	token, err := c.tenantAccessToken(ctx, creds)
	if err != nil {
		return nil, err
	}
	q := url.Values{}
	q.Set("user_id_type", "open_id")
	for _, id := range openIDs {
		if id != "" {
			q.Add("user_ids", id)
		}
	}
	path := "/open-apis/contact/v3/users/batch?" + q.Encode()

	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Items []struct {
				OpenID string `json:"open_id"`
				Name   string `json:"name"`
			} `json:"items"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, c.resolveBaseURL(creds), http.MethodGet, path, token, nil, &resp); err != nil {
		return nil, fmt.Errorf("lark http client: batch get users: %w", err)
	}
	if resp.Code != 0 {
		if isTokenError(resp.Code) {
			c.invalidateToken(creds.AppID)
		}
		return nil, fmt.Errorf("lark http client: batch get users: code=%d msg=%q", resp.Code, resp.Msg)
	}

	out := make(map[string]string, len(resp.Data.Items))
	for _, it := range resp.Data.Items {
		if it.OpenID != "" && it.Name != "" {
			out[it.OpenID] = it.Name
		}
	}
	return out, nil
}

// larkRESTMessageItem is the IM v1 message item shape returned by the
// get / list endpoints. It differs from the WS receive event in two
// ways the enricher cares about: msg_type (not message_type), and a
// flat `sender.id` / `mentions[].id` string (not a nested id object).
type larkRESTMessageItem struct {
	MessageID      string `json:"message_id"`
	RootID         string `json:"root_id"`
	ParentID       string `json:"parent_id"`
	UpperMessageID string `json:"upper_message_id"`
	MsgType        string `json:"msg_type"`
	CreateTime     string `json:"create_time"`
	Deleted        bool   `json:"deleted"`
	Sender         struct {
		ID         string `json:"id"`
		IDType     string `json:"id_type"`
		SenderType string `json:"sender_type"`
	} `json:"sender"`
	Body struct {
		Content string `json:"content"`
	} `json:"body"`
	Mentions []struct {
		Key  string `json:"key"`
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"mentions"`
}

func (it larkRESTMessageItem) normalize() LarkMessage {
	m := LarkMessage{
		MessageID:      it.MessageID,
		MessageType:    it.MsgType,
		Content:        it.Body.Content,
		SenderID:       it.Sender.ID,
		SenderType:     it.Sender.SenderType,
		CreateTime:     it.CreateTime,
		ParentID:       it.ParentID,
		RootID:         it.RootID,
		UpperMessageID: it.UpperMessageID,
		Deleted:        it.Deleted,
	}
	for _, mn := range it.Mentions {
		m.Mentions = append(m.Mentions, LarkMessageMention{Key: mn.Key, ID: mn.ID, Name: mn.Name})
	}
	return m
}

// fetchBotUnionID resolves a Bot's `union_id` from its `open_id` via
// /open-apis/contact/v3/users/{open_id}?user_id_type=open_id. Split
// out from GetBotInfo so the failure mode is explicit and the call
// sites that only need open_id don't pay for the second round-trip.
//
// Empty string + nil error is a valid outcome: Lark's user endpoint
// can return code=0 with no union_id field when the app's contact
// scope is restricted. Caller logs and continues; the decoder still
// works in single-bot deployments where open_id-based matching is
// unambiguous.
func (c *httpAPIClient) fetchBotUnionID(ctx context.Context, baseURL, appID, token, openID string) (string, error) {
	if openID == "" {
		return "", errors.New("empty open_id")
	}
	q := url.Values{}
	q.Set("user_id_type", "open_id")
	path := "/open-apis/contact/v3/users/" + url.PathEscape(openID) + "?" + q.Encode()
	var resp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			User struct {
				UnionID string `json:"union_id"`
			} `json:"user"`
		} `json:"data"`
	}
	if err := c.doJSON(ctx, baseURL, http.MethodGet, path, token, nil, &resp); err != nil {
		return "", fmt.Errorf("contact users: %w", err)
	}
	if resp.Code != 0 {
		// invalidateToken is keyed by app_id (the cache key on
		// httpAPIClient.tokens), NOT by the bearer string. Passing
		// the bearer would do nothing and a stale token would keep
		// being reused on every retry until natural TTL expiry.
		if isTokenError(resp.Code) {
			c.invalidateToken(appID)
		}
		return "", fmt.Errorf("contact users: code=%d msg=%q", resp.Code, resp.Msg)
	}
	return resp.Data.User.UnionID, nil
}

// doJSON encapsulates the verb + URL + auth-header + JSON
// encode/decode dance so each public method stays a thin shape-only
// adapter. baseURL is the per-call open-platform host the caller
// resolved via resolveBaseURL (region-aware). token == "" skips the
// Authorization header (only the tenant_access_token endpoint takes
// that path).
func (c *httpAPIClient) doJSON(ctx context.Context, baseURL, method, path, token string, body, out any) error {
	var rdr io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal body: %w", err)
		}
		rdr = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, baseURL+path, rdr)
	if err != nil {
		return fmt.Errorf("new request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	rawBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(rawBody), 512))
	}
	if out != nil && len(rawBody) > 0 {
		if err := json.Unmarshal(rawBody, out); err != nil {
			return fmt.Errorf("decode body: %w (raw=%s)", err, truncate(string(rawBody), 256))
		}
	}
	return nil
}

func isTokenError(code int) bool {
	return code == codeTokenExpired || code == codeTokenInvalid
}

// APIError is a structured Lark business error: the request reached
// Lark, returned HTTP 200, but Lark rejected it with a non-zero
// `code`. This is distinct from the transport-level errors doJSON
// surfaces (network failure, 5xx, timeout), which are returned as
// plain wrapped errors. The distinction matters for the threaded-reply
// fallback: a business code is definitive ("nothing was sent, and here
// is exactly why"), whereas a transport error is ambiguous ("the
// message may or may not have been delivered") and must NOT trigger a
// chat-level retry that could duplicate or leak the reply.
type APIError struct {
	Op   string
	Code int
	Msg  string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("lark http client: %s: code=%d msg=%q", e.Op, e.Code, e.Msg)
}

// threadReplyUnsupportedCodes are the reply-endpoint business codes
// that definitively mean "this specific trigger message / topic cannot
// receive a threaded reply" AND nothing was sent, while a plain
// chat-level send to the same chat is unaffected. Only these justify
// the chat-level fallback. Rate limits (230020), "message is being
// sent" (230049, ambiguous), permission/content errors (which would
// also fail at chat level), and all transport/5xx/timeout failures are
// deliberately excluded: those stay failures so we never duplicate a
// reply or leak a thread-only reply into the main group chat.
// Codes are from the IM reply-message endpoint error table.
var threadReplyUnsupportedCodes = map[int]struct{}{
	230011: {}, // the trigger message has been recalled
	230019: {}, // the topic does not exist
	230050: {}, // the trigger message is invisible to the operator
	230071: {}, // the group does not support reply in thread
	230072: {}, // aggregated messages do not support reply in thread
	230111: {}, // cannot reply to a self-destructing message
}

// isThreadReplyUnsupported reports whether err is a Lark APIError whose
// code means the threaded reply cannot land on this target. Only such
// errors are safe to retry at the chat level. Transport errors and
// other business codes return false.
func isThreadReplyUnsupported(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		_, ok := threadReplyUnsupportedCodes[apiErr.Code]
		return ok
	}
	return false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// bindingPromptTemplate renders the "you need to bind" interactive
// card. Single primary CTA pointing at the redemption URL; the rest
// of the body is plain-text Chinese copy matching the in-app voice.
//
// Kept here (not in defaultRenderer) so the binding card template can
// evolve independently of the streaming-status cards the Patcher
// renders — they have different lifecycles (binding card is one-shot,
// status cards are patched in place).
func bindingPromptTemplate(bindURL string) (string, error) {
	doc := map[string]any{
		"config": map[string]any{"wide_screen_mode": true},
		"header": map[string]any{
			"template": "blue",
			"title":    map[string]any{"tag": "plain_text", "content": "Multica"},
		},
		"elements": []any{
			map[string]any{
				"tag": "div",
				"text": map[string]any{
					"tag":     "lark_md",
					"content": "你还没有绑定 Multica 账户。点击下方按钮完成绑定后即可使用此 Agent。",
				},
			},
			map[string]any{
				"tag": "action",
				"actions": []any{
					map[string]any{
						"tag":  "button",
						"text": map[string]any{"tag": "plain_text", "content": "去绑定"},
						"type": "primary",
						"url":  bindURL,
					},
				},
			},
		},
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}
