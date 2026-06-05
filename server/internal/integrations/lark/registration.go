package lark

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Lark PersonalAgent registration is a 1:1 implementation of RFC 8628
// (OAuth 2.0 Device Authorization Grant) against accounts.feishu.cn
// (mainland) / accounts.larksuite.com (international). The protocol
// has only two phases:
//
//  1. begin — POST <domain>/oauth/v1/app/registration with
//     action=begin and (archetype=PersonalAgent / auth_method=client_secret
//     / request_user_info=open_id). Lark returns a device_code, a
//     verification_uri_complete (the QR target), a polling interval,
//     and an expiry. Multica renders the QR, the user scans it in the
//     Lark app, walks the "create a PersonalAgent for this account"
//     flow, and authorizes.
//
//  2. poll — POST the same URL with action=poll and the device_code.
//     The server replies with one of:
//       - {error: "authorization_pending"} — keep polling.
//       - {error: "slow_down"}             — bump the interval +5s, then poll.
//       - {user_info: {tenant_brand: "lark"}}
//                                          — user authorized via the
//                                            international tenant; we
//                                            switch the polling host to
//                                            accounts.larksuite.com and
//                                            keep going.
//       - {client_id, client_secret, user_info: {open_id}}
//                                          — terminal success.
//       - {error: "expired_token"|"access_denied"}
//                                          — terminal failure.
//
// We deliberately inline this client (rather than depend on
// github.com/larksuite/oapi-sdk-go/scene/registration) so the registration
// surface ships with the same go.mod footprint as the rest of the lark
// package — a ~270-line single-file client is the right size to own
// when the alternative is dragging a full SDK + its transitive deps
// for one endpoint.

const (
	registrationDefaultFeishuDomain = "https://accounts.feishu.cn"
	registrationDefaultLarkDomain   = "https://accounts.larksuite.com"

	registrationEndpoint = "/oauth/v1/app/registration"

	// Default polling cadence Lark uses when the server omits `interval`.
	// 5s matches the Lark SDK; smaller would risk slow_down responses
	// without buying any latency improvement.
	registrationDefaultPollSeconds = 5

	// Default registration window (10 minutes) — long enough for a user
	// to scan, switch apps, walk the create-bot flow, and authorize on
	// their phone, short enough that an abandoned session does not pin
	// resources for hours.
	registrationDefaultExpireSeconds = 600

	// Internal-tenant brand label Lark uses to flag "you scanned with a
	// Lark (international) account, not a Feishu (mainland) one". When
	// we see this we re-aim polling at accounts.larksuite.com and
	// re-issue the very next poll WITHOUT first waiting for the polling
	// interval — the upstream SDK shows that Lark's server emits the
	// tenant_brand hint exactly once during the polling stream and the
	// subsequent poll must reach the new domain to learn the credentials.
	registrationTenantBrandLark = "lark"
)

// RegistrationConfig configures the device-flow client. All fields are
// optional; the zero value targets accounts.feishu.cn over the standard
// http.Client (with a 30s per-call timeout so a stalled poll cannot
// silently pin a session goroutine for the entire expiry window).
type RegistrationConfig struct {
	// Domain is the initial polling host. Default
	// "https://accounts.feishu.cn"; staging deployments can point this
	// at a mock or at the Lark beta endpoint.
	Domain string

	// LarkDomain is the international-tenant polling host the client
	// switches to when Lark's poll response surfaces
	// user_info.tenant_brand="lark". Default
	// "https://accounts.larksuite.com".
	LarkDomain string

	// HTTPClient is the transport for every request the client makes.
	// Empty defaults to a fresh *http.Client with a 30s timeout — the
	// device-flow endpoint is normally a sub-second call but we add
	// headroom for cross-region paths.
	HTTPClient *http.Client

	// Source labels the QR-code URL's `source` query param so Lark's
	// telemetry can attribute installs back to Multica. Empty defaults
	// to "multica".
	Source string

	// Now is overridable for deterministic expiry-bound tests.
	Now func() time.Time
}

func (c RegistrationConfig) withDefaults() RegistrationConfig {
	if c.Domain == "" {
		c.Domain = registrationDefaultFeishuDomain
	}
	if c.LarkDomain == "" {
		c.LarkDomain = registrationDefaultLarkDomain
	}
	if c.HTTPClient == nil {
		c.HTTPClient = &http.Client{Timeout: 30 * time.Second}
	}
	if c.Source == "" {
		c.Source = "multica"
	}
	if c.Now == nil {
		c.Now = time.Now
	}
	return c
}

// RegistrationClient runs the device-flow protocol but does NOT own
// session state or installation provisioning — RegistrationService
// composes the client with the session store and the DB write path.
// Splitting these lets the protocol client be deterministic and easy
// to test against an httptest fake without involving the database.
type RegistrationClient struct {
	cfg RegistrationConfig
}

// NewRegistrationClient constructs the device-flow client.
func NewRegistrationClient(cfg RegistrationConfig) *RegistrationClient {
	return &RegistrationClient{cfg: cfg.withDefaults()}
}

// BeginResult is what Begin returns to RegistrationService.
type BeginResult struct {
	DeviceCode string
	// QRCodeURL is the verification_uri_complete with Multica's `source`
	// telemetry params appended; render this as a QR image client-side.
	QRCodeURL string
	// Domain is the polling host this session opened against. The
	// service must pass it back to Poll so the international-tenant
	// switch (StatusDomainSwitched) can re-aim subsequent polls.
	Domain string
	// Interval is Lark's suggested polling cadence. Slow_down responses
	// add 5s; the service is responsible for honoring the updated
	// cadence.
	Interval time.Duration
	// ExpiresIn is the absolute lifetime of the device_code. A poll
	// after this window returns expired_token; the session goroutine
	// uses this to size its context.WithTimeout.
	ExpiresIn time.Duration
}

// PollResult is the discriminated union of every terminal and
// non-terminal poll outcome. The caller branches on the populated
// fields:
//   - Success         (ClientID + ClientSecret + OpenID) → install
//   - SwitchedDomain  (new domain string)               → swap host, re-poll immediately
//   - Status          ("authorization_pending" / "slow_down") → wait, poll again
//   - Err             (terminal error)                  → abort the session
type PollResult struct {
	ClientID     string
	ClientSecret string
	OpenID       OpenID

	// SwitchedDomain is non-empty when Lark told us "this is a Lark
	// international account, re-poll over there." RegistrationService
	// must update its session's stored domain and re-poll WITHOUT
	// honoring the interval (the SDK does the same — the upstream
	// behaviour is that the very next poll lands on the new domain and
	// returns the actual credentials).
	SwitchedDomain string

	// Status carries non-terminal protocol signals — typically
	// "authorization_pending" or "slow_down". The service uses these
	// to decide whether to bump the polling interval.
	Status string

	// Err is the terminal error code (e.g. "access_denied",
	// "expired_token", or a free-form Lark code we did not anticipate).
	// On a non-terminal result this is nil.
	Err *RegistrationError
}

// RegistrationError is the typed Lark protocol error. The handler
// pipeline maps `Code` to a stable user-facing reason so the UI can
// render the right copy without parsing prose.
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

// Begin opens a new device-flow session against the configured Feishu
// (mainland) domain. Lark may surface a Lark-international tenant on
// the FIRST poll — we don't try to predict it here; the polling loop
// in RegistrationService handles the domain swap.
//
// namePreset pre-fills the bot/app name on Lark's "create a
// PersonalAgent" form so the installed bot defaults to e.g.
// "<agent> - Multica" instead of Lark's auto-generated
// "{用户姓名}的智能助手". It is a user-editable default (the user can
// still change it on the form), and it rides on the QR URL — not the
// begin POST body, which has no name field. Empty omits the pre-fill.
func (c *RegistrationClient) Begin(ctx context.Context, namePreset string) (*BeginResult, error) {
	var resp struct {
		DeviceCode              string `json:"device_code"`
		VerificationURIComplete string `json:"verification_uri_complete"`
		VerificationURI         string `json:"verification_uri"`
		UserCode                string `json:"user_code"`
		Interval                int    `json:"interval"`
		ExpireIn                int    `json:"expire_in"`
		Error                   string `json:"error"`
		ErrorDescription        string `json:"error_description"`
	}
	form := url.Values{
		"action":            []string{"begin"},
		"archetype":         []string{"PersonalAgent"},
		"auth_method":       []string{"client_secret"},
		"request_user_info": []string{"open_id"},
	}
	if err := c.doForm(ctx, c.cfg.Domain, form, &resp); err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return nil, &RegistrationError{Code: resp.Error, Description: resp.ErrorDescription}
	}
	if resp.DeviceCode == "" {
		return nil, &RegistrationError{Code: "invalid_response", Description: "device_code is empty"}
	}
	if resp.VerificationURIComplete == "" {
		return nil, &RegistrationError{Code: "invalid_response", Description: "verification_uri_complete is empty"}
	}
	qr, err := decorateQRCodeURL(resp.VerificationURIComplete, c.cfg.Source, namePreset)
	if err != nil {
		return nil, &RegistrationError{Code: "invalid_response", Description: "verification_uri_complete is not a URL: " + err.Error()}
	}
	interval := registrationDefaultPollSeconds
	if resp.Interval > 0 {
		interval = resp.Interval
	}
	expireIn := registrationDefaultExpireSeconds
	if resp.ExpireIn > 0 {
		expireIn = resp.ExpireIn
	}
	return &BeginResult{
		DeviceCode: resp.DeviceCode,
		QRCodeURL:  qr,
		Domain:     c.cfg.Domain,
		Interval:   time.Duration(interval) * time.Second,
		ExpiresIn:  time.Duration(expireIn) * time.Second,
	}, nil
}

// Poll runs a single poll round-trip against the supplied domain (which
// the caller may have updated mid-session via SwitchedDomain from a
// prior PollResult). Domain selection lives outside the client so the
// session state machine in RegistrationService is the single source of
// truth for which host the next call must hit.
func (c *RegistrationClient) Poll(ctx context.Context, domain, deviceCode string) (*PollResult, error) {
	if deviceCode == "" {
		return nil, &RegistrationError{Code: "invalid_argument", Description: "device_code is required"}
	}
	if domain == "" {
		domain = c.cfg.Domain
	}
	var resp struct {
		ClientID     string `json:"client_id,omitempty"`
		ClientSecret string `json:"client_secret,omitempty"`
		UserInfo     *struct {
			OpenID      string `json:"open_id,omitempty"`
			TenantBrand string `json:"tenant_brand,omitempty"`
		} `json:"user_info,omitempty"`
		Error            string `json:"error,omitempty"`
		ErrorDescription string `json:"error_description,omitempty"`
	}
	form := url.Values{
		"action":      []string{"poll"},
		"device_code": []string{deviceCode},
	}
	if err := c.doForm(ctx, domain, form, &resp); err != nil {
		return nil, err
	}

	// Tenant-brand-driven domain swap. Lark emits this exactly once
	// when a Lark-international account authorized; the next poll must
	// hit accounts.larksuite.com to learn the credentials. We surface
	// the swap as a typed signal so the service does not have to know
	// the brand string.
	if resp.UserInfo != nil &&
		resp.UserInfo.TenantBrand == registrationTenantBrandLark &&
		!strings.HasPrefix(domain, c.cfg.LarkDomain) {
		return &PollResult{SwitchedDomain: c.cfg.LarkDomain}, nil
	}

	// Success: both client_id AND client_secret AND the installer
	// open_id must be present. Partial responses are treated as a
	// protocol error so RegistrationService never writes a
	// half-populated lark_installation row.
	if resp.ClientID != "" && resp.ClientSecret != "" {
		if resp.UserInfo == nil || resp.UserInfo.OpenID == "" {
			return nil, &RegistrationError{
				Code:        "invalid_response",
				Description: "success response missing installer open_id",
			}
		}
		return &PollResult{
			ClientID:     resp.ClientID,
			ClientSecret: resp.ClientSecret,
			OpenID:       OpenID(resp.UserInfo.OpenID),
		}, nil
	}

	switch resp.Error {
	case "authorization_pending", "slow_down":
		return &PollResult{Status: resp.Error}, nil
	case "access_denied", "expired_token":
		return &PollResult{
			Err: &RegistrationError{Code: resp.Error, Description: resp.ErrorDescription},
		}, nil
	case "":
		// Empty error AND empty credentials = keep polling; this
		// matches the upstream SDK's tolerant handling for the case
		// where the server briefly returns an empty body during the
		// authorize-redirect window.
		return &PollResult{Status: "authorization_pending"}, nil
	default:
		return &PollResult{
			Err: &RegistrationError{Code: resp.Error, Description: resp.ErrorDescription},
		}, nil
	}
}

func (c *RegistrationClient) doForm(ctx context.Context, domain string, form url.Values, out any) error {
	endpoint := strings.TrimRight(domain, "/") + registrationEndpoint
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("registration: new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.cfg.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("registration: http do: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("registration: read body: %w", err)
	}
	if len(body) == 0 {
		return &RegistrationError{
			Code:        fmt.Sprintf("http_%d", resp.StatusCode),
			Description: "empty body",
		}
	}
	// RFC 8628 device-flow servers return non-2xx with a JSON body whose
	// `error` field is the actual signal — `authorization_pending` and
	// `slow_down` arrive as HTTP 400, NOT 2xx. Decoding the body first
	// and letting the caller route on `resp.Error` is what the upstream
	// Go SDK does; treating any non-2xx as a hard protocol error (the
	// previous behaviour) killed every session on the first poll because
	// the user hasn't scanned the QR yet at that point.
	if jsonErr := json.Unmarshal(body, out); jsonErr == nil {
		return nil
	}
	// Body didn't parse — surface the raw status + payload tail so ops
	// can tell a Lark outage / proxy interception apart from a schema
	// drift. Caller treats this as a terminal protocol error.
	return &RegistrationError{
		Code:        fmt.Sprintf("http_%d", resp.StatusCode),
		Description: truncate(string(body), 256),
	}
}

// decorateQRCodeURL appends the SDK-style telemetry params Lark expects
// on the QR-image URL. Without `from=sdk&tp=sdk&source=<src>` the
// scanner UI on the user's phone shows a less polished prompt and Lark
// cannot attribute installs back to Multica in their analytics.
//
// namePreset, when non-empty, is appended as `name=<...>` to pre-fill
// the bot/app name on Lark's "create a PersonalAgent" form. This
// mirrors the upstream SDK's AppPreset.Name: Lark reads it from the
// verification/QR URL (the begin POST body carries no name field) and
// treats it as a user-editable default, not a locked final name.
func decorateQRCodeURL(raw, source, namePreset string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("from", "sdk")
	q.Set("tp", "sdk")
	q.Set("source", "go-sdk/"+source)
	if namePreset != "" {
		q.Set("name", namePreset)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// ErrRegistrationAccessDenied is returned by RegistrationService when
// the user explicitly denied the install in the Lark UI. Distinct from
// other terminal failures so the UI can render "you cancelled the
// install" instead of a generic error.
var ErrRegistrationAccessDenied = errors.New("lark registration: access denied by user")

// ErrRegistrationExpired is returned by RegistrationService when the
// device_code's expiry window elapsed without the user authorizing.
// Distinct so the UI can prompt "scan again — the previous QR expired".
var ErrRegistrationExpired = errors.New("lark registration: expired")
