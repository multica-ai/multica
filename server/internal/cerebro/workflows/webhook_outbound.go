package workflows

// Phase-3 outbound webhook delivery (JEH-1108, PR 2). Split out of
// actions.go because the SSRF guard + retry-aware HTTP path is ~150 lines
// on its own and grouping it here keeps actions.go readable.
//
// Replaces the PR-1 placeholder in actionWebhookOutbound. Retry semantics
// stay in Service.attempt — this file just returns an error on any
// non-success status, network error, or timeout, and the existing retry
// ladder takes over.

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// outboundTimeout — per-call HTTP timeout. Picked above typical webhook
	// processing time (a few hundred ms) but below the 1-minute first-retry
	// gap, so a slow endpoint doesn't pile up multiple in-flight retries.
	outboundTimeout = 10 * time.Second
	// outboundReadCap — limit on the response body we slurp before giving
	// up. The body is only used for error logging; large responses are
	// truncated.
	outboundReadCap = 4 * 1024
)

// outboundHeaderNameRe is the whitelist for caller-supplied custom header
// names. Must be conservative: HTTP token chars per RFC 7230 are wider, but
// allowing the punctuation set surfaces hard-to-debug splitting attacks if
// the caller is clever.
var outboundHeaderNameRe = regexp.MustCompile(`^[A-Za-z0-9-]+$`)

// outboundHeaderForbiddenExact and outboundHeaderForbiddenPrefix are the
// exact names and case-insensitive prefixes a caller MAY NOT supply.
// Authorization / Cookie are too easy to abuse for SSRF-bridging; anything
// starting with X-Multica- belongs to us.
var (
	outboundHeaderForbiddenExact = map[string]struct{}{
		"authorization": {},
		"cookie":        {},
		"set-cookie":    {},
	}
	outboundHeaderForbiddenPrefixes = []string{"x-multica-"}
)

// ErrOutboundForbiddenHeader is returned by validateOutboundHeaders so the
// handler write-time validator can map it to a 400 with a clear message.
var ErrOutboundForbiddenHeader = errors.New("forbidden header")

// ErrOutboundBadURL is returned when the configured URL parses as
// non-HTTPS (or non-HTTP-with-bypass) or resolves to a private/loopback
// address.
var ErrOutboundBadURL = errors.New("bad outbound url")

// envAllowLocalhost — when set, the SSRF guard accepts http:// URLs and
// loopback/RFC1918 destinations. Strictly a dev/test escape hatch.
func envAllowLocalhost() bool { return envFlagEnabled("CEREBRO_WORKFLOWS_ALLOW_LOCALHOST") }

// actionWebhookOutbound — replaces the PR-1 placeholder. Validates the URL
// + headers, runs the SSRF guard, computes the optional HMAC signature,
// and posts. Non-2xx / network errors / timeouts return non-nil so
// Service.attempt schedules a retry.
func (s *Service) actionWebhookOutbound(ctx context.Context, wf workflow, te TriggerEvent) error {
	var cfg ActionConfigWebhookOutbound
	if err := json.Unmarshal(wf.actionConfig, &cfg); err != nil {
		return fmt.Errorf("webhook_outbound: parse config: %w", err)
	}
	if cfg.URL == "" {
		return errors.New("webhook_outbound: url is required")
	}

	allowLoopback := envAllowLocalhost()
	if err := validateOutboundURL(cfg.URL, allowLoopback); err != nil {
		return fmt.Errorf("webhook_outbound: %w", err)
	}
	if err := validateOutboundHeaders(cfg.Headers); err != nil {
		return fmt.Errorf("webhook_outbound: %w", err)
	}

	body, err := buildOutboundBody(wf, te, cfg.IncludeIssueSnapshot)
	if err != nil {
		return fmt.Errorf("webhook_outbound: build body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("webhook_outbound: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Multica-Workflow-Id", uuidString(wf.id))
	req.Header.Set("X-Multica-Trigger", te.Type)

	// Optional HMAC signature. Secret comes off the workflow projection
	// (the Dispatch path stitches it in from the row's
	// outbound_webhook_secret column). When unset, deliver unsigned and
	// emit a single warn-level so the operator sees it.
	if secret := strings.TrimSpace(wf.outboundSecret); secret != "" {
		req.Header.Set("X-Multica-Signature", computeOutboundSignature(secret, body))
	} else {
		slog.Warn("workflow webhook_outbound: no outbound_webhook_secret set; delivering unsigned",
			"workflow_id", uuidString(wf.id),
		)
	}

	// Apply custom caller headers AFTER the framework headers so caller
	// can't override Content-Type or our X-Multica-* set. The header-name
	// whitelist also catches a sneaky caller that tries.
	for k, v := range cfg.Headers {
		if headerIsForbidden(k) {
			// Defense in depth — validateOutboundHeaders should have
			// caught this at write time, but we re-check at delivery so a
			// pre-existing row whose validator was looser still loses.
			slog.Warn("workflow webhook_outbound: dropping forbidden header at delivery time",
				"workflow_id", uuidString(wf.id),
				"header", k,
			)
			continue
		}
		req.Header.Set(k, v)
	}

	client := &http.Client{Timeout: outboundTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("webhook_outbound: request failed: %w", err)
	}
	defer resp.Body.Close()
	// Drain a small portion of the body for log diagnostics, then bail.
	respSnippet, _ := io.ReadAll(io.LimitReader(resp.Body, outboundReadCap))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("webhook_outbound: non-2xx %d: %s", resp.StatusCode, truncate(respSnippet, 256))
	}
	return nil
}

// buildOutboundBody — canonical wire shape:
//
//	{
//	  "trigger":  { type, event_id, from_status, to_status },
//	  "workflow": { id, name },
//	  "issue":    full snapshot if IncludeIssueSnapshot, else { id: <iid> }
//	  "fired_at": RFC3339
//	}
func buildOutboundBody(wf workflow, te TriggerEvent, includeSnapshot bool) ([]byte, error) {
	trigger := map[string]any{
		"type":     te.Type,
		"event_id": te.EventID,
	}
	if te.FromStatus != "" {
		trigger["from_status"] = te.FromStatus
	}
	if te.ToStatus != "" {
		trigger["to_status"] = te.ToStatus
	}
	wfBlock := map[string]any{
		"id":   uuidString(wf.id),
		"name": workflowNameFromMeta(wf),
	}
	var issueBlock map[string]any
	if includeSnapshot {
		if raw, ok := te.Raw["issue"].(map[string]any); ok {
			issueBlock = raw
		} else if te.IssueID != "" {
			issueBlock = map[string]any{"id": te.IssueID}
		}
	} else if te.IssueID != "" {
		issueBlock = map[string]any{"id": te.IssueID}
	}

	payload := map[string]any{
		"trigger":  trigger,
		"workflow": wfBlock,
		"fired_at": nowFunc().UTC().Format(time.RFC3339),
	}
	if issueBlock != nil {
		payload["issue"] = issueBlock
	}
	return json.Marshal(payload)
}

// computeOutboundSignature — `sha256=<hex>` format mirroring the inbound
// path. Body-only (no timestamp), since we control the delivery and the
// receiver can rely on TLS + signed-body for integrity.
func computeOutboundSignature(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

// validateOutboundURL parses the URL, enforces scheme, and resolves the
// hostname to ensure no resolved IP is loopback/private/link-local/CGNAT/
// ULA. Allows http+loopback when CEREBRO_WORKFLOWS_ALLOW_LOCALHOST=1.
func validateOutboundURL(raw string, allowLoopback bool) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrOutboundBadURL, err)
	}
	scheme := strings.ToLower(u.Scheme)
	switch {
	case scheme == "https":
		// allowed
	case scheme == "http" && allowLoopback:
		// dev/test bypass
	default:
		return fmt.Errorf("%w: scheme must be https", ErrOutboundBadURL)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("%w: missing host", ErrOutboundBadURL)
	}
	hostLower := strings.ToLower(host)
	if !allowLoopback {
		if hostLower == "localhost" || strings.HasSuffix(hostLower, ".internal") || strings.HasSuffix(hostLower, ".local") {
			return fmt.Errorf("%w: forbidden hostname %q", ErrOutboundBadURL, host)
		}
	}

	// Resolve and inspect all returned IPs.
	ips, err := net.DefaultResolver.LookupIP(context.Background(), "ip", host)
	if err != nil {
		// If DNS fails AND we're not allowing loopback, refuse — we can't
		// prove the target isn't private.
		return fmt.Errorf("%w: resolve %q: %v", ErrOutboundBadURL, host, err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("%w: resolve %q: no addresses", ErrOutboundBadURL, host)
	}
	for _, ip := range ips {
		if ipForbidden(ip, allowLoopback) {
			return fmt.Errorf("%w: resolved %q to forbidden address %s", ErrOutboundBadURL, host, ip)
		}
	}
	return nil
}

// CIDRs we reject when allowLoopback is false. Lazy-parsed once.
var forbiddenCIDRs = mustParseCIDRs(
	"10.0.0.0/8",      // RFC 1918
	"172.16.0.0/12",   // RFC 1918
	"192.168.0.0/16",  // RFC 1918
	"169.254.0.0/16",  // link-local
	"100.64.0.0/10",   // CGNAT (RFC 6598)
	"fc00::/7",        // IPv6 ULA
	"fe80::/10",       // IPv6 link-local
)

func mustParseCIDRs(cidrs ...string) []*net.IPNet {
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		_, n, err := net.ParseCIDR(c)
		if err != nil {
			panic(fmt.Sprintf("workflows: parse cidr %q: %v", c, err))
		}
		out = append(out, n)
	}
	return out
}

func ipForbidden(ip net.IP, allowLoopback bool) bool {
	if ip.IsLoopback() {
		return !allowLoopback
	}
	if ip.IsUnspecified() {
		return true
	}
	if allowLoopback {
		// In dev/test mode skip everything below.
		return false
	}
	if ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	for _, n := range forbiddenCIDRs {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// validateOutboundHeaders enforces the name allowlist and the forbidden
// names. Used both by the write-time handler validator and the runtime
// delivery path.
func validateOutboundHeaders(headers map[string]string) error {
	for k := range headers {
		if !outboundHeaderNameRe.MatchString(k) {
			return fmt.Errorf("%w: %q (allowed: %s)", ErrOutboundForbiddenHeader, k, outboundHeaderNameRe.String())
		}
		if headerIsForbidden(k) {
			return fmt.Errorf("%w: %q", ErrOutboundForbiddenHeader, k)
		}
	}
	return nil
}

func headerIsForbidden(name string) bool {
	lower := strings.ToLower(name)
	if _, ok := outboundHeaderForbiddenExact[lower]; ok {
		return true
	}
	for _, p := range outboundHeaderForbiddenPrefixes {
		if strings.HasPrefix(lower, p) {
			return true
		}
	}
	return false
}

// truncate clips a byte slice to at most n bytes for safe logging.
func truncate(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "…"
}

// workflowNameFromMeta returns the workflow row name when the projection
// carries one. Falls back to a workflow-id placeholder so the outbound
// payload always has a non-empty `workflow.name` field — receivers often
// key correlations on it.
func workflowNameFromMeta(wf workflow) string {
	if wf.name != "" {
		return wf.name
	}
	if wf.id.Valid {
		return "workflow:" + uuidString(wf.id)
	}
	return ""
}
