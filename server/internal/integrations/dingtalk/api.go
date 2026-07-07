package dingtalk

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Shared DingTalk API plumbing for the channel integration: host
// defaults, the error shape, the cached app access token, and the legacy
// oapi response envelope.

const (
	defaultOpenAPIBase = "https://api.dingtalk.com"
	defaultOAPIBase    = "https://oapi.dingtalk.com"
)

// tokenCache is one cached app access token (~2h TTL server-side).
type tokenCache struct {
	value     string
	expiresAt time.Time
}

// APIError is a DingTalk API failure: HTTP status when the transport
// failed, or the platform's errcode/errmsg when the envelope did.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Status > 0 {
		return fmt.Sprintf("dingtalk api error: HTTP %d %s %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("dingtalk api error: %s %s", e.Code, e.Message)
}

// legacyEnvelope is the oapi.dingtalk.com response wrapper: HTTP 200 with
// a non-zero errcode is still a failure.
type legacyEnvelope struct {
	ErrCode json.RawMessage `json:"errcode"`
	ErrMsg  string          `json:"errmsg"`
	Result  json.RawMessage `json:"result"`
}

func (e legacyEnvelope) OK() bool {
	return e.CodeString() == "0" || e.CodeString() == ""
}

// CodeString normalizes errcode, which the platform serializes as either
// a number or a quoted string depending on the endpoint.
func (e legacyEnvelope) CodeString() string {
	raw := strings.TrimSpace(string(e.ErrCode))
	if raw == "" || raw == "null" {
		return ""
	}
	raw = strings.Trim(raw, `"`)
	return raw
}
