package wecom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"unicode"
)

const inboxWebhookBaseURL = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
const inboxWebhookMaxResponseBytes = 8 * 1024

var (
	errWebhookInvalidURL      = errors.New("wecom webhook: invalid URL")
	errWebhookTransport       = errors.New("wecom webhook: transport failed")
	errWebhookHTTPRejected    = errors.New("wecom webhook: unexpected HTTP status")
	errWebhookInvalidResponse = errors.New("wecom webhook: invalid response")
	errWebhookRejected        = errors.New("wecom webhook: request rejected")
	errWebhookRateLimited     = errors.New("wecom webhook: rate limited")
)

func validateInboxWebhookURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.Contains(raw, "#") ||
		u.Scheme != "https" || u.Host != "qyapi.weixin.qq.com" || u.User != nil ||
		u.EscapedPath() != "/cgi-bin/webhook/send" || !strings.HasPrefix(u.RawQuery, "key=") ||
		strings.Contains(u.RawQuery, "&") {
		return "", errWebhookInvalidURL
	}
	query, err := url.ParseQuery(u.RawQuery)
	if err != nil || len(query) != 1 || len(query["key"]) != 1 || query.Get("key") == "" ||
		strings.IndexFunc(query.Get("key"), unicode.IsControl) >= 0 {
		return "", errWebhookInvalidURL
	}
	return inboxWebhookBaseURL + "?key=" + url.QueryEscape(query.Get("key")), nil
}

func sendInboxWebhook(ctx context.Context, client *http.Client, endpoint string, payload []byte) error {
	endpoint, err := validateInboxWebhookURL(endpoint)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return errWebhookTransport
	}
	req.Header.Set("Content-Type", "application/json")
	if client == nil {
		client = http.DefaultClient
	}
	safeClient := *client
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := safeClient.Do(req)
	if err != nil {
		return errWebhookTransport
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusTooManyRequests {
		return errWebhookRateLimited
	}
	if resp.StatusCode != http.StatusOK {
		return errWebhookHTTPRejected
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, inboxWebhookMaxResponseBytes+1))
	if err != nil || len(body) > inboxWebhookMaxResponseBytes {
		return errWebhookInvalidResponse
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(body, &result); err != nil {
		return errWebhookInvalidResponse
	}
	var code *int
	if err := json.Unmarshal(result["errcode"], &code); err != nil || code == nil {
		return errWebhookInvalidResponse
	}
	if *code == 45009 {
		return errWebhookRateLimited
	}
	if *code != 0 {
		return errWebhookRejected
	}
	return nil
}
