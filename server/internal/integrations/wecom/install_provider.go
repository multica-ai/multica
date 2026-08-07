package wecom

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"
)

// Provider abstracts the WeCom scan-code create-bot API (spec §4, the
// reference doc at repo root `扫码创建智能机器人-API.md`). The HTTPProvider
// implementation talks to work.weixin.qq.com/ai/qc/{generate,query_result};
// the install worker uses the interface directly so tests can inject a fake
// with deterministic responses.
type Provider interface {
	// Generate creates a fresh QR session. `scode` is the polling handle;
	// `auth_url` is the URL the frontend renders into a QR image (spec
	// §4).
	Generate(ctx context.Context) (GenerateResult, error)
	// QueryResult polls the QR status for a previously-generated scode.
	// Two-second minimum interval is enforced by the worker, not here.
	QueryResult(ctx context.Context, scode string) (QueryResult, error)
}

// GenerateResult mirrors the /ai/qc/generate payload, which arrives nested
// under `data` on the wire — see decodeProviderBody.
type GenerateResult struct {
	Scode   string `json:"scode"`
	AuthURL string `json:"auth_url"`
}

// QueryStatus enumerates the /ai/qc/query_result "status" wire values (spec
// §4.2). init and pending are treated identically by the worker — the doc
// calls out that both mean "unauthorized OR expired", so the worker relies
// on the DB-side expires_at check for terminal expiry rather than trusting
// this string to tell them apart.
type QueryStatus string

const (
	QueryStatusInit    QueryStatus = "init"
	QueryStatusPending QueryStatus = "pending"
	QueryStatusSuccess QueryStatus = "success"
)

// QueryResult is the /ai/qc/query_result payload, nested under `data` on the
// wire like GenerateResult. BotInfo is only populated when Status is
// "success".
type QueryResult struct {
	Status  QueryStatus `json:"status"`
	BotInfo *BotInfo    `json:"bot_info,omitempty"`
}

// BotInfo carries the credentials WeCom returns on success. `botid` is the
// wire's `aibotid`; `secret` is the long-connection auth secret (spec §4.3).
// Neither is logged.
type BotInfo struct {
	BotID  string `json:"botid"`
	Secret string `json:"secret"`
}

// HTTPProviderConfig configures the real client. BaseURL defaults to the
// documented endpoint; tests point it at an httptest server so no real
// WeCom traffic ever leaves the machine.
type HTTPProviderConfig struct {
	BaseURL    string
	SourceID   string
	HTTPClient *http.Client
	Logger     *slog.Logger
	Timeout    time.Duration
}

// DefaultProviderBaseURL is the documented origin. It is a constant here so
// a stray env misconfiguration cannot silently point the install path at
// somebody's private mirror.
const DefaultProviderBaseURL = "https://work.weixin.qq.com"

// HTTPProvider talks to the real WeCom endpoints. It is safe for concurrent
// use — every call constructs a fresh request from cfg.
type HTTPProvider struct {
	cfg HTTPProviderConfig
}

// NewHTTPProvider validates the source id and returns a ready client. An
// empty source id is a hard error; callers gate on Configured() before
// constructing the provider.
func NewHTTPProvider(cfg HTTPProviderConfig) (*HTTPProvider, error) {
	if cfg.SourceID == "" {
		return nil, errors.New("wecom: HTTPProvider requires SourceID")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultProviderBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: defaultUpstreamTimeout}
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = defaultUpstreamTimeout
	}
	return &HTTPProvider{cfg: cfg}, nil
}

// Generate hits /ai/qc/generate?source=<source>.
func (p *HTTPProvider) Generate(ctx context.Context) (GenerateResult, error) {
	u := p.cfg.BaseURL + "/ai/qc/generate?source=" + url.QueryEscape(p.cfg.SourceID)
	var out GenerateResult
	if err := p.getJSON(ctx, u, &out); err != nil {
		return GenerateResult{}, err
	}
	if out.Scode == "" || out.AuthURL == "" {
		return GenerateResult{}, errors.New("wecom generate: response missing scode / auth_url")
	}
	return out, nil
}

// QueryResult hits /ai/qc/query_result?scode=<scode>&source=<source>. The
// worker guards the polling cadence.
func (p *HTTPProvider) QueryResult(ctx context.Context, scode string) (QueryResult, error) {
	if scode == "" {
		return QueryResult{}, errors.New("wecom query_result: scode is required")
	}
	u := p.cfg.BaseURL + "/ai/qc/query_result?scode=" + url.QueryEscape(scode) +
		"&source=" + url.QueryEscape(p.cfg.SourceID)
	var out QueryResult
	if err := p.getJSON(ctx, u, &out); err != nil {
		return QueryResult{}, err
	}
	switch out.Status {
	case QueryStatusInit, QueryStatusPending, QueryStatusSuccess:
	default:
		return QueryResult{}, fmt.Errorf("wecom query_result: unknown status %q", out.Status)
	}
	if out.Status == QueryStatusSuccess {
		if out.BotInfo == nil || out.BotInfo.BotID == "" || out.BotInfo.Secret == "" {
			return QueryResult{}, errors.New("wecom query_result: success without bot_info")
		}
	}
	return out, nil
}

func (p *HTTPProvider) getJSON(ctx context.Context, requestURL string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.Timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fmt.Errorf("wecom provider: build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := p.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("wecom provider: http: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return fmt.Errorf("wecom provider: read: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("wecom provider: http %d", resp.StatusCode)
	}
	return decodeProviderBody(body, out)
}

// decodeProviderBody unwraps the `data` object generate and query_result nest
// their payload in. The vendored reference doc shows these fields at the top
// level, which the first implementation trusted; both endpoints in fact wrap.
// A missing `data` is reported as such rather than decoding into a zero value,
// so a future shape change surfaces here instead of as an empty-field error
// further up.
func decodeProviderBody(body []byte, out any) error {
	var envelope struct {
		Data json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		return fmt.Errorf("wecom provider: decode: %w", err)
	}
	if len(envelope.Data) == 0 || string(envelope.Data) == "null" {
		return errors.New("wecom provider: response carries no data object")
	}
	if err := json.Unmarshal(envelope.Data, out); err != nil {
		return fmt.Errorf("wecom provider: decode data: %w", err)
	}
	return nil
}
