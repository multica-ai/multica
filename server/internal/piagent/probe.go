package piagent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Outcome classifies a model-connection probe so the UI can tell a wrong key
// apart from an exhausted balance, an unreachable endpoint, or a model the
// account cannot use. Each maps to a different user action.
type Outcome string

const (
	OutcomeOK                 Outcome = "ok"
	OutcomeInvalidKey         Outcome = "invalid_key"
	OutcomeInsufficientQuota  Outcome = "insufficient_quota"
	OutcomeRateLimited        Outcome = "rate_limited"
	OutcomeModelNotFound      Outcome = "model_not_found"
	OutcomeEndpointNotFound   Outcome = "endpoint_not_found"
	OutcomeProviderError      Outcome = "provider_error"
	OutcomeNetworkUnreachable Outcome = "network_unreachable"
)

// ProbeResult carries the classification plus the provider's own message so a
// user can act on a failure without reading server logs. Detail is truncated
// and scrubbed of the submitted key before it leaves this package.
type ProbeResult struct {
	Outcome Outcome `json:"outcome"`
	Status  int     `json:"status,omitempty"`
	Detail  string  `json:"detail,omitempty"`
}

const (
	probeTimeout        = 12 * time.Second
	probeMaxBodyBytes   = 16 << 10
	probeMaxDetailRunes = 400
)

// Prober performs one minimal, billable-but-negligible request against a
// provider to prove that provider + endpoint + model + key work together.
type Prober struct {
	Client *http.Client
}

// NewProber builds a Prober whose transport refuses to connect to any address
// that is not a public unicast IP. This endpoint takes a user-supplied URL and
// dials it from the server, so without this guard it would be an SSRF gadget
// pointed at cloud metadata and internal services.
func NewProber() *Prober {
	dialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 10 * time.Second}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, _, err := net.SplitHostPort(addr)
			if err != nil {
				return nil, fmt.Errorf("invalid address %q", addr)
			}
			// Resolve first, then check every candidate: a name that resolves
			// to a mix of public and private addresses must not slip through
			// on the private one.
			ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
			if err != nil {
				return nil, fmt.Errorf("resolve %q: %w", host, err)
			}
			for _, ip := range ips {
				if !isPublicUnicast(ip.IP) {
					return nil, fmt.Errorf("base_url resolves to a non-public address")
				}
			}
			return dialer.DialContext(ctx, network, addr)
		},
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: probeTimeout,
		DisableKeepAlives:     true,
	}
	return &Prober{
		Client: &http.Client{
			Transport: transport,
			Timeout:   probeTimeout,
			// A redirect can point anywhere, including back at a private
			// address the DialContext guard already rejected. Refuse outright
			// rather than re-validating a moving target.
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

// isPublicUnicast reports whether ip is safe for the server to dial on behalf
// of a user. Everything loopback, private, link-local (which covers the
// 169.254.169.254 metadata address), CGNAT, multicast, or unspecified is out.
func isPublicUnicast(ip net.IP) bool {
	if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsMulticast() {
		return false
	}
	// RFC 6598 carrier-grade NAT (100.64.0.0/10) is not covered by IsPrivate
	// but is just as much an internal range from our perspective.
	if v4 := ip.To4(); v4 != nil && v4[0] == 100 && v4[1] >= 64 && v4[1] <= 127 {
		return false
	}
	return true
}

// ValidateForRemoteProbe is stricter than Validate: the daemon may legitimately
// point Pi at a loopback model server on the user's own machine, but the server
// must never dial one on the user's behalf.
func ValidateForRemoteProbe(cfg Config) error {
	if err := Validate(cfg); err != nil {
		return err
	}
	u, err := url.ParseRequestURI(Normalize(cfg).BaseURL)
	if err != nil {
		return fmt.Errorf("base_url must be an absolute URL")
	}
	if u.Scheme != "https" {
		return fmt.Errorf("base_url must use HTTPS to be verified from the server")
	}
	host := u.Hostname()
	// Two separate checks: an IP literal is judged directly, while a name
	// like "localhost" parses as no IP at all and has to be caught by name.
	// The dialer re-checks the resolved address either way; failing here
	// gives the user a fixable message instead of a generic network error.
	if ip := net.ParseIP(host); ip != nil {
		if !isPublicUnicast(ip) {
			return fmt.Errorf("base_url must not point at a private or loopback address")
		}
	} else if isLoopbackName(host) {
		return fmt.Errorf("base_url must not point at a private or loopback address")
	}
	return nil
}

// isLoopbackName covers the RFC 6761 special-use names that always resolve
// back to the host running this check.
func isLoopbackName(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	return host == "localhost" || strings.HasSuffix(host, ".localhost")
}

// Probe sends the smallest request each API shape allows — one token of output
// where the API permits capping it — so verifying a key costs effectively
// nothing while still exercising auth, endpoint, and model together.
func (p *Prober) Probe(ctx context.Context, cfg Config, apiKey string) ProbeResult {
	cfg = Normalize(cfg)
	apiKey = strings.TrimSpace(apiKey)

	req, err := p.buildRequest(ctx, cfg, apiKey)
	if err != nil {
		return ProbeResult{Outcome: OutcomeProviderError, Detail: err.Error()}
	}

	resp, err := p.Client.Do(req)
	if err != nil {
		return ProbeResult{
			Outcome: OutcomeNetworkUnreachable,
			Detail:  scrubKey(networkErrorDetail(err), apiKey),
		}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, probeMaxBodyBytes))

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return ProbeResult{Outcome: OutcomeOK, Status: resp.StatusCode}
	}
	detail := scrubKey(providerErrorMessage(body), apiKey)
	return ProbeResult{
		Outcome: classifyStatus(resp.StatusCode, detail),
		Status:  resp.StatusCode,
		Detail:  detail,
	}
}

func (p *Prober) buildRequest(ctx context.Context, cfg Config, apiKey string) (*http.Request, error) {
	base := strings.TrimRight(cfg.BaseURL, "/")

	var endpoint string
	var payload any
	header := http.Header{}
	header.Set("Content-Type", "application/json")

	switch cfg.API {
	case "openai-completions":
		endpoint = base + "/chat/completions"
		payload = map[string]any{
			"model":      cfg.Model,
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 1,
		}
		header.Set("Authorization", "Bearer "+apiKey)
	case "openai-responses":
		endpoint = base + "/responses"
		// The Responses API rejects a cap below 16, so this is the floor.
		payload = map[string]any{
			"model":             cfg.Model,
			"input":             "hi",
			"max_output_tokens": 16,
		}
		header.Set("Authorization", "Bearer "+apiKey)
	case "anthropic-messages":
		endpoint = base + "/v1/messages"
		payload = map[string]any{
			"model":      cfg.Model,
			"messages":   []any{map[string]any{"role": "user", "content": "hi"}},
			"max_tokens": 1,
		}
		header.Set("x-api-key", apiKey)
		header.Set("anthropic-version", "2023-06-01")
	case "google-generative-ai":
		endpoint = base + "/models/" + url.PathEscape(cfg.Model) + ":generateContent"
		payload = map[string]any{
			"contents": []any{
				map[string]any{"parts": []any{map[string]any{"text": "hi"}}},
			},
			"generationConfig": map[string]any{"maxOutputTokens": 1},
		}
		header.Set("x-goog-api-key", apiKey)
	default:
		return nil, fmt.Errorf("unsupported api %q", cfg.API)
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode probe request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(encoded))
	if err != nil {
		return nil, fmt.Errorf("build probe request: %w", err)
	}
	req.Header = header
	return req, nil
}

// classifyStatus maps a provider's HTTP status onto an action the user can
// take. Providers disagree on which status means "out of credit", so the
// message is consulted for the statuses that are genuinely ambiguous.
func classifyStatus(status int, detail string) Outcome {
	lowered := strings.ToLower(detail)
	quotaWorded := strings.Contains(lowered, "quota") ||
		strings.Contains(lowered, "billing") ||
		strings.Contains(lowered, "credit") ||
		strings.Contains(lowered, "insufficient") ||
		strings.Contains(lowered, "balance")

	switch {
	case status == http.StatusUnauthorized:
		return OutcomeInvalidKey
	case status == http.StatusForbidden:
		if quotaWorded {
			return OutcomeInsufficientQuota
		}
		return OutcomeInvalidKey
	case status == http.StatusPaymentRequired:
		return OutcomeInsufficientQuota
	case status == http.StatusTooManyRequests:
		// Several providers report an exhausted balance as 429 rather than 402.
		if quotaWorded {
			return OutcomeInsufficientQuota
		}
		return OutcomeRateLimited
	case status == http.StatusNotFound:
		if strings.Contains(lowered, "model") {
			return OutcomeModelNotFound
		}
		return OutcomeEndpointNotFound
	case status == http.StatusBadRequest:
		if strings.Contains(lowered, "model") {
			return OutcomeModelNotFound
		}
		return OutcomeProviderError
	default:
		return OutcomeProviderError
	}
}

// providerErrorMessage pulls the human-readable message out of the error
// envelope every supported provider happens to share (`error.message`),
// falling back to the raw body so an unrecognized shape still says something.
func providerErrorMessage(body []byte) string {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var envelope struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil {
		if msg := strings.TrimSpace(envelope.Error.Message); msg != "" {
			return truncateRunes(msg, probeMaxDetailRunes)
		}
		if msg := strings.TrimSpace(envelope.Message); msg != "" {
			return truncateRunes(msg, probeMaxDetailRunes)
		}
		if msg := strings.TrimSpace(envelope.Error.Type); msg != "" {
			return truncateRunes(msg, probeMaxDetailRunes)
		}
	}
	return truncateRunes(trimmed, probeMaxDetailRunes)
}

// networkErrorDetail keeps the useful part of a transport failure without
// leaking the full request URL back to the client.
func networkErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		return truncateRunes(urlErr.Err.Error(), probeMaxDetailRunes)
	}
	return truncateRunes(err.Error(), probeMaxDetailRunes)
}

// scrubKey guarantees a submitted secret can never travel back out inside a
// provider's echoed error message.
func scrubKey(detail, apiKey string) string {
	if detail == "" || len(apiKey) < 8 {
		return detail
	}
	return strings.ReplaceAll(detail, apiKey, "***")
}

func truncateRunes(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit]) + "…"
}
