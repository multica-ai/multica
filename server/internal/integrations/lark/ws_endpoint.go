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
	"strings"
	"strconv"
	"time"
)

// The bootstrap host for the long-conn `/callback/ws/endpoint` request
// is the installation's open-platform host — open.feishu.cn for Feishu
// (mainland), open.larksuite.com for Lark (international) — resolved per
// call from InstallationCredentials.Region via Region.OpenPlatformBaseURL
// (Lark returns the actual wss URL in the response body, so only the
// bootstrap POST host has to be region-aware). A deployment-wide
// MULTICA_LARK_CALLBACK_BASE_URL still overrides every installation when
// set (staging / mock).

// HTTPConnectionTokenFetcher is the production EndpointFetcher. It
// exchanges per-installation app credentials for a short-lived
// WebSocket URL + ClientConfig by calling
// `POST /callback/ws/endpoint` on Lark's open-platform host — the
// same bootstrap path the official `larksuite/oapi-sdk-go/v3/ws`
// client uses. The request body carries `{AppID, AppSecret}` plain
// (no tenant_access_token bearer); the response carries the wss URL
// (single-use, embedded device_id/service_id auth) and a ClientConfig
// with PingInterval / ReconnectInterval / ReconnectNonce /
// ReconnectCount in seconds.
//
// We do NOT cache the response. The wss URL is single-use by design
// (the embedded `device_id` is rotated on every bootstrap call), so
// re-using it on a reconnect would yield an auth rejection that looks
// like a Lark outage. The connector calls Endpoint() once per Run.
//
// PersonalAgent compatibility — OPEN RISK (MUL-2671 review thread):
// the official Feishu docs describe long-conn mode as "supports
// 企业自建应用 only". The PersonalAgent device-flow archetype is not
// listed as supported; live confirmation is pending. If the bootstrap
// call returns a structured "app type not supported" error, this code
// surfaces the code+msg directly so the Hub's backoff loop logs the
// real reason instead of looping silently. The smoke test path is
// `multica` -> register a PersonalAgent -> enable WS -> watch logs.
type HTTPConnectionTokenFetcher struct {
	cfg HTTPConnectionTokenConfig
}

// HTTPConnectionTokenConfig wires the fetcher's dependencies. BaseURL is
// an optional deployment-wide override; when empty (the production
// default) Endpoint() resolves the bootstrap host per installation from
// the region. Tests substitute an httptest.Server URL to force all
// regions to the fake server.
type HTTPConnectionTokenConfig struct {
	BaseURL    string
	HTTPClient *http.Client
	Now        func() time.Time
	Logger     *slog.Logger
}

func (c HTTPConnectionTokenConfig) withDefaults() HTTPConnectionTokenConfig {
	// BaseURL is intentionally NOT defaulted here. Empty means "no
	// deployment-wide override" — Endpoint() then resolves the bootstrap
	// host per installation from InstallationCredentials.Region, so one
	// fetcher serves both Feishu and Lark. A non-empty BaseURL
	// (MULTICA_LARK_CALLBACK_BASE_URL, or an httptest URL in tests)
	// forces every installation to that host.
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

// NewHTTPConnectionTokenFetcher returns the production EndpointFetcher
// bound to the supplied configuration.
func NewHTTPConnectionTokenFetcher(cfg HTTPConnectionTokenConfig) (*HTTPConnectionTokenFetcher, error) {
	return &HTTPConnectionTokenFetcher{cfg: cfg.withDefaults()}, nil
}

// bootstrapRequest mirrors the SDK's BootstrapRequest. Field names use
// PascalCase exactly because the server-side JSON tags are PascalCase
// (`AppID`, not `app_id`); the SDK's pbbp2 schema dictates the format
// and lower-snake_case would not match.
type bootstrapRequest struct {
	AppID     string `json:"AppID"`
	AppSecret string `json:"AppSecret"`
}

// endpointResponse mirrors the SDK's EndpointResp + Endpoint +
// ClientConfig. Field naming is PascalCase to match Lark's wire shape.
type endpointResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		URL          string `json:"URL"`
		ClientConfig struct {
			ReconnectCount    int `json:"ReconnectCount"`
			ReconnectInterval int `json:"ReconnectInterval"`
			ReconnectNonce    int `json:"ReconnectNonce"`
			PingInterval      int `json:"PingInterval"`
		} `json:"ClientConfig"`
	} `json:"data"`
}

// Endpoint implements EndpointFetcher.
func (f *HTTPConnectionTokenFetcher) Endpoint(ctx context.Context, creds InstallationCredentials) (WSEndpoint, error) {
	if creds.AppID == "" || creds.AppSecret == "" {
		return WSEndpoint{}, errors.New("lark ws endpoint: missing app_id / app_secret")
	}
	body := bootstrapRequest{AppID: creds.AppID, AppSecret: creds.AppSecret}
	raw, err := json.Marshal(body)
	if err != nil {
		return WSEndpoint{}, fmt.Errorf("marshal body: %w", err)
	}
	// Resolve the bootstrap host per call: an explicit cfg.BaseURL
	// override wins (env / httptest), otherwise the installation's region
	// picks Feishu vs Lark so one fetcher serves both clouds.
	base := f.cfg.BaseURL
	if base == "" {
		base = creds.Region.OpenPlatformBaseURL()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/callback/ws/endpoint", bytes.NewReader(raw))
	if err != nil {
		return WSEndpoint{}, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	// Locale header is sent verbatim by the SDK — Lark uses it for the
	// error `msg` field (Chinese vs English). We pick zh because that's
	// the audience Multica server logs are read by today; if i18n
	// matters later this becomes an env or a per-installation knob.
	req.Header.Set("locale", "zh")
	resp, err := f.cfg.HTTPClient.Do(req)
	if err != nil {
		return WSEndpoint{}, fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()
	rawResp, err := io.ReadAll(resp.Body)
	if err != nil {
		return WSEndpoint{}, fmt.Errorf("read body: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return WSEndpoint{}, fmt.Errorf("http %d: %s", resp.StatusCode, truncate(string(rawResp), 512))
	}
	var decoded endpointResponse
	if err := json.Unmarshal(rawResp, &decoded); err != nil {
		return WSEndpoint{}, fmt.Errorf("decode response: %w (raw=%s)", err, truncate(string(rawResp), 256))
	}
	if decoded.Code != 0 || decoded.Data.URL == "" {
		// Surface the structured Lark error verbatim — that's what
		// operators need to disambiguate "app type not supported"
		// (PersonalAgent risk) from "credentials wrong" from "Lark
		// outage". The downstream Hub backoff logs this on each
		// reconnect attempt.
		return WSEndpoint{}, fmt.Errorf("lark ws endpoint: code=%d msg=%q", decoded.Code, decoded.Msg)
	}
	serviceID, err := parseServiceIDFromURL(decoded.Data.URL)
	if err != nil {
		return WSEndpoint{}, fmt.Errorf("parse service_id from wss url: %w", err)
	}
	return WSEndpoint{
		URL:               decoded.Data.URL,
		Headers:           http.Header{},
		ServiceID:         serviceID,
		PingInterval:      time.Duration(decoded.Data.ClientConfig.PingInterval) * time.Second,
		ReconnectInterval: time.Duration(decoded.Data.ClientConfig.ReconnectInterval) * time.Second,
		ReconnectNonce:    time.Duration(decoded.Data.ClientConfig.ReconnectNonce) * time.Second,
		ReconnectCount:    decoded.Data.ClientConfig.ReconnectCount,
	}, nil
}

// parseServiceIDFromURL extracts the `service_id` query parameter Lark
// embeds in the wss URL. The connector needs this value to address
// outbound Frame.Service for ping/pong and ACK frames; the SDK does
// the same.
func parseServiceIDFromURL(rawURL string) (int32, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, err
	}
	sid := u.Query().Get("service_id")
	if sid == "" {
		return 0, errors.New("missing service_id query parameter")
	}
	n, err := strconv.ParseInt(sid, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("service_id %q is not an int: %w", sid, err)
	}
	return int32(n), nil
}
