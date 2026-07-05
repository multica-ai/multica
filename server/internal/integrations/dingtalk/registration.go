package dingtalk

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// DingTalk app registration ("一键创建钉钉应用扫码接入流程") is a
// device-flow protocol against oapi.dingtalk.com. It differs from the
// RFC 8628 flow Lark uses in three ways:
//
//  1. An extra init phase — POST /app/registration/init returns a
//     one-shot nonce (5-minute TTL) that the begin call consumes. The
//     optional `source` field labels the authorize page's copy and is
//     a DingTalk-assigned value; empty omits it.
//
//  2. begin — POST /app/registration/begin with the nonce. DingTalk
//     returns a device_code, a verification_uri_complete (the QR
//     target), a user_code, a polling interval, and an expiry. Multica
//     renders the QR, the user scans it in the DingTalk app and
//     authorizes; DingTalk then creates an app in the user's org.
//
//  3. poll — POST /app/registration/poll with the device_code. The
//     response's `status` field is the discriminator:
//       - "WAITING"  — keep polling at the suggested interval.
//       - "SUCCESS"  — terminal; client_id + client_secret are set.
//       - "FAIL"     — terminal failure; fail_reason is prose.
//       - "EXPIRED"  — the device_code lapsed; terminal.
//
// Every response additionally carries the classic oapi errcode/errmsg
// envelope; errcode != 0 means a server-side error and is terminal.
// Unlike Lark's flow, the success payload does NOT identify the user
// who scanned, so there is no installer auto-bind step downstream.
//
// We inline the client (no DingTalk SDK dependency) for the same
// reason the lark package inlines its registration client: three JSON
// POSTs are the wrong trade for an SDK's transitive dependency
// footprint.

const (
	registrationDefaultBase = "https://oapi.dingtalk.com"

	registrationInitPath  = "/app/registration/init"
	registrationBeginPath = "/app/registration/begin"
	registrationPollPath  = "/app/registration/poll"

	// Default polling cadence when the server omits `interval`. The doc
	// suggests 5s; smaller buys no latency and risks throttling.
	registrationDefaultPollSeconds = 5

	// Default device_code lifetime when the server omits `expires_in`.
	registrationDefaultExpireSeconds = 600

	// Cap on the polling window. begin currently advertises
	// expires_in=7200 (2 hours), but the doc's 注意事项 states the
	// device_code / user_code pair is practically valid for ~10 minutes
	// — and an abandoned install dialog must not pin a polling
	// goroutine for two hours either way. 15 minutes comfortably covers
	// a slow scan-and-authorize round trip.
	registrationMaxPollWindow = 15 * time.Minute

	registrationStatusWaiting = "WAITING"
	registrationStatusSuccess = "SUCCESS"
	registrationStatusFail    = "FAIL"
	registrationStatusExpired = "EXPIRED"
)

// RegistrationConfig configures the device-flow client. All fields are
// optional; the zero value targets oapi.dingtalk.com over a standard
// http.Client (with a 30s per-call timeout so a stalled poll cannot
// silently pin a session goroutine for the entire expiry window).
type RegistrationConfig struct {
	// BaseURL is the oapi host. Default "https://oapi.dingtalk.com";
	// staging deployments can point this at a mock.
	BaseURL string

	// HTTPClient is the transport for every request the client makes.
	// Empty defaults to a fresh *http.Client with a 30s timeout.
	HTTPClient *http.Client

	// Source is the DingTalk-assigned label forwarded on init so the
	// authorize page renders partner-specific copy. It is optional and
	// requires manual allocation from DingTalk; empty omits the field,
	// which the doc explicitly supports.
	Source string
}

func (c RegistrationConfig) withDefaults() RegistrationConfig {
	if c.BaseURL == "" {
		c.BaseURL = registrationDefaultBase
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	return c
}

// RegistrationClient runs the init/begin/poll protocol but does NOT own
// session state or installation provisioning — RegistrationService
// composes the client with the session store and the DB write path.
// Splitting these keeps the protocol client deterministic and testable
// against an httptest fake without involving the database.
type RegistrationClient struct {
	cfg RegistrationConfig
}

// NewRegistrationClient constructs the device-flow client.
func NewRegistrationClient(cfg RegistrationConfig) *RegistrationClient {
	return &RegistrationClient{cfg: cfg.withDefaults()}
}

// RegistrationBeginResult is what Begin returns to RegistrationService.
type RegistrationBeginResult struct {
	DeviceCode string
	// QRCodeURL is the verification_uri_complete; render it as a QR
	// image client-side. When a source was configured, DingTalk has
	// already embedded it in the URL — no extra decoration is needed.
	QRCodeURL string
	// UserCode is the short human-readable code (XXXX-XXXX-XXXX) a user
	// can type instead of scanning. Currently informational.
	UserCode string
	// Interval is DingTalk's suggested polling cadence.
	Interval time.Duration
	// ExpiresIn is the polling window the session goroutine sizes its
	// context to. Already capped at registrationMaxPollWindow — see the
	// constant's comment for why we do not honor the advertised 2 hours.
	ExpiresIn time.Duration
}

// RegistrationPollResult is the discriminated union of every terminal
// and non-terminal poll outcome. The caller branches on the populated
// fields:
//   - ClientID + ClientSecret → install (terminal success)
//   - Pending                 → wait an interval, poll again
//   - Err                     → abort the session (terminal)
type RegistrationPollResult struct {
	ClientID     string
	ClientSecret string
	Pending      bool
	Err          *RegistrationError
}

// RegistrationError is the typed protocol error. `Code` is a stable
// value the service maps to a user-facing reason: "expired", "fail",
// "invalid_response", "errcode_<n>", or "http_<status>".
type RegistrationError struct {
	Code        string
	Description string
}

func (e *RegistrationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Description == "" {
		return fmt.Sprintf("registration: %s", e.Code)
	}
	return fmt.Sprintf("registration: %s: %s", e.Code, e.Description)
}

// registrationEnvelope is the errcode/errmsg pair every endpoint wraps
// its payload in. errcode != 0 is a terminal server-side error.
type registrationEnvelope struct {
	Errcode int    `json:"errcode"`
	Errmsg  string `json:"errmsg"`
}

func (e registrationEnvelope) err() *RegistrationError {
	if e.Errcode == 0 {
		return nil
	}
	return &RegistrationError{
		Code:        fmt.Sprintf("errcode_%d", e.Errcode),
		Description: e.Errmsg,
	}
}

// Begin opens a new device-flow session: init mints the one-shot nonce,
// begin consumes it and returns the QR target plus polling parameters.
// The two calls are fused here because the nonce has no meaning outside
// this pairing (5-minute TTL, single use) — no caller ever needs to
// hold one across requests.
func (c *RegistrationClient) Begin(ctx context.Context) (*RegistrationBeginResult, error) {
	var initResp struct {
		registrationEnvelope
		Nonce     string `json:"nonce"`
		ExpiresIn int    `json:"expires_in"`
	}
	initReq := map[string]string{}
	if c.cfg.Source != "" {
		initReq["source"] = c.cfg.Source
	}
	if err := c.doJSON(ctx, registrationInitPath, initReq, &initResp); err != nil {
		return nil, err
	}
	if err := initResp.err(); err != nil {
		return nil, err
	}
	if initResp.Nonce == "" {
		return nil, &RegistrationError{Code: "invalid_response", Description: "init: nonce is empty"}
	}

	var beginResp struct {
		registrationEnvelope
		DeviceCode              string `json:"device_code"`
		UserCode                string `json:"user_code"`
		VerificationURI         string `json:"verification_uri"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		ExpiresIn               int    `json:"expires_in"`
		Interval                int    `json:"interval"`
	}
	if err := c.doJSON(ctx, registrationBeginPath, map[string]string{"nonce": initResp.Nonce}, &beginResp); err != nil {
		return nil, err
	}
	if err := beginResp.err(); err != nil {
		return nil, err
	}
	if beginResp.DeviceCode == "" {
		return nil, &RegistrationError{Code: "invalid_response", Description: "begin: device_code is empty"}
	}
	if beginResp.VerificationURIComplete == "" {
		return nil, &RegistrationError{Code: "invalid_response", Description: "begin: verification_uri_complete is empty"}
	}

	interval := registrationDefaultPollSeconds
	if beginResp.Interval > 0 {
		interval = beginResp.Interval
	}
	expireIn := registrationDefaultExpireSeconds
	if beginResp.ExpiresIn > 0 {
		expireIn = beginResp.ExpiresIn
	}
	expires := time.Duration(expireIn) * time.Second
	if expires > registrationMaxPollWindow {
		expires = registrationMaxPollWindow
	}
	return &RegistrationBeginResult{
		DeviceCode: beginResp.DeviceCode,
		QRCodeURL:  beginResp.VerificationURIComplete,
		UserCode:   beginResp.UserCode,
		Interval:   time.Duration(interval) * time.Second,
		ExpiresIn:  expires,
	}, nil
}

// Poll runs a single poll round-trip for the supplied device_code.
func (c *RegistrationClient) Poll(ctx context.Context, deviceCode string) (*RegistrationPollResult, error) {
	if deviceCode == "" {
		return nil, &RegistrationError{Code: "invalid_argument", Description: "device_code is required"}
	}
	var resp struct {
		registrationEnvelope
		Status       string `json:"status"`
		ClientID     string `json:"client_id"`
		ClientSecret string `json:"client_secret"`
		FailReason   string `json:"fail_reason"`
	}
	if err := c.doJSON(ctx, registrationPollPath, map[string]string{"device_code": deviceCode}, &resp); err != nil {
		return nil, err
	}
	if err := resp.err(); err != nil {
		return nil, err
	}

	switch resp.Status {
	case registrationStatusWaiting:
		return &RegistrationPollResult{Pending: true}, nil
	case registrationStatusSuccess:
		// Partial success payloads are treated as a protocol error so
		// RegistrationService never writes a half-populated installation.
		if resp.ClientID == "" || resp.ClientSecret == "" {
			return nil, &RegistrationError{
				Code:        "invalid_response",
				Description: "poll: SUCCESS without client_id/client_secret",
			}
		}
		return &RegistrationPollResult{ClientID: resp.ClientID, ClientSecret: resp.ClientSecret}, nil
	case registrationStatusFail:
		return &RegistrationPollResult{
			Err: &RegistrationError{Code: "fail", Description: resp.FailReason},
		}, nil
	case registrationStatusExpired:
		return &RegistrationPollResult{
			Err: &RegistrationError{Code: "expired"},
		}, nil
	case "":
		// Empty status with a clean envelope — tolerate as "keep
		// polling" the same way the lark client tolerates an empty
		// body during the authorize-redirect window.
		return &RegistrationPollResult{Pending: true}, nil
	default:
		return nil, &RegistrationError{
			Code:        "invalid_response",
			Description: "poll: unknown status " + resp.Status,
		}
	}
}

func (c *RegistrationClient) doJSON(ctx context.Context, path string, in any, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("registration: marshal request: %w", err)
	}
	endpoint := strings.TrimRight(c.cfg.BaseURL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("registration: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("registration: http do: %w", err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("registration: read body: %w", err)
	}
	// Decode the body first and let the errcode envelope route the
	// outcome — the doc pins all signals (including failures) to JSON
	// payloads, so a parseable body wins over the HTTP status.
	if len(payload) > 0 {
		if jsonErr := json.Unmarshal(payload, out); jsonErr == nil {
			return nil
		}
	}
	// Empty or unparseable body — surface the status + payload tail so
	// ops can tell a DingTalk outage / proxy interception apart from a
	// schema drift. The caller treats this as a terminal protocol error.
	return &RegistrationError{
		Code:        fmt.Sprintf("http_%d", resp.StatusCode),
		Description: truncate(string(payload), 256),
	}
}
