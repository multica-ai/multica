package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

const webhookMaxResponseBytes = 4096

type WebhookSender struct {
	HTTPClient   *http.Client
	AllowPrivate bool
}

func (s WebhookSender) SendJSON(ctx context.Context, endpointURL, secret string, payload []byte) error {
	if err := ValidateWebhookURL(ctx, endpointURL, s.AllowPrivate); err != nil {
		return err
	}

	client := s.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpointURL, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Multica-Webhook/1.0")
	if strings.TrimSpace(secret) != "" {
		req.Header.Set("X-Multica-Signature", webhookSignature(secret, payload))
	}

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, webhookMaxResponseBytes))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("webhook returned %d: %s", resp.StatusCode, msg)
	}
	return nil
}

func ValidateWebhookURL(ctx context.Context, raw string, allowPrivate bool) error {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Host == "" {
		return errors.New("invalid webhook url")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("webhook url must use http or https")
	}
	if !allowPrivate {
		if err := rejectPrivateWebhookHost(ctx, parsed.Hostname()); err != nil {
			return err
		}
	}
	return nil
}

func rejectPrivateWebhookHost(ctx context.Context, host string) error {
	host = strings.TrimSpace(strings.ToLower(host))
	if host == "" || host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("webhook url host is not allowed")
	}

	if addr, err := netip.ParseAddr(host); err == nil {
		if isBlockedWebhookAddr(addr) {
			return errors.New("webhook url resolves to a private address")
		}
		return nil
	}

	resolver := net.DefaultResolver
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("failed to resolve webhook url host: %w", err)
	}
	if len(ips) == 0 {
		return errors.New("webhook url host did not resolve")
	}
	for _, ip := range ips {
		addr, ok := netip.AddrFromSlice(ip.IP)
		if !ok || isBlockedWebhookAddr(addr.Unmap()) {
			return errors.New("webhook url resolves to a private address")
		}
	}
	return nil
}

func isBlockedWebhookAddr(addr netip.Addr) bool {
	return addr.IsPrivate() ||
		addr.IsLoopback() ||
		addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() ||
		addr.IsMulticast() ||
		addr.IsUnspecified()
}

func webhookSignature(secret string, payload []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
