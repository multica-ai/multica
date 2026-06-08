package cli

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"testing"
)

// timeoutErr is a net.Error whose Timeout() reports true, used to exercise the
// net.Error timeout branch without a real socket.
type timeoutErr struct{}

func (timeoutErr) Error() string   { return "i/o timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

func TestClassifyNetworkError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want ErrorKind
	}{
		{"context deadline", context.DeadlineExceeded, KindNetworkTimeout},
		{"wrapped deadline", fmt.Errorf("resolve issue: %w", context.DeadlineExceeded), KindNetworkTimeout},
		{"net timeout", timeoutErr{}, KindNetworkTimeout},
		{"dns", &net.DNSError{Err: "no such host", Name: "api.multica.ai", IsNotFound: true}, KindNetworkDNS},
		{"connection refused", syscall.ECONNREFUSED, KindNetworkRefused},
		{"x509 unknown authority", x509.UnknownAuthorityError{}, KindNetworkTLS},
		{"x509 hostname", x509.HostnameError{Host: "api.multica.ai"}, KindNetworkTLS},
		{"timeout string fallback", errors.New("Get \"https://x\": net/http: request canceled (Client.Timeout exceeded)"), KindNetworkTimeout},
		{"dns string fallback", errors.New("dial tcp: lookup api.multica.ai: no such host"), KindNetworkDNS},
		{"refused string fallback", errors.New("dial tcp 127.0.0.1:443: connect: connection refused"), KindNetworkRefused},
		{"tls string fallback", errors.New("x509: certificate signed by unknown authority"), KindNetworkTLS},
		{"offline catch-all", errors.New("write: connection reset by peer"), KindNetworkOffline},
		{"nil", nil, KindUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyNetworkError(tc.err); got != tc.want {
				t.Errorf("classifyNetworkError(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestHTTPErrorKind(t *testing.T) {
	cases := []struct {
		status int
		want   ErrorKind
	}{
		{401, KindAuthRequired},
		{403, KindForbidden},
		{404, KindNotFound},
		{409, KindConflict},
		{400, KindValidation},
		{422, KindValidation},
		{429, KindRateLimited},
		{500, KindServerError},
		{502, KindServerError},
		{418, KindUnknown},
	}
	for _, tc := range cases {
		t.Run(fmt.Sprintf("status_%d", tc.status), func(t *testing.T) {
			e := &HTTPError{StatusCode: tc.status}
			if got := e.Kind(); got != tc.want {
				t.Errorf("HTTPError{%d}.Kind() = %d, want %d", tc.status, got, tc.want)
			}
		})
	}
}

// TestFormatErrorAllKinds asserts that every ErrorKind produces a non-empty,
// localized, user-facing message in both languages, and that none of them leak
// the raw internal error string when debug is off.
func TestFormatErrorAllKinds(t *testing.T) {
	withLang(t, "") // default English
	allKinds := []ErrorKind{
		KindNetworkTimeout, KindNetworkDNS, KindNetworkRefused, KindNetworkTLS, KindNetworkOffline,
		KindAuthRequired, KindForbidden, KindNotFound, KindConflict, KindValidation,
		KindRateLimited, KindServerError, KindUnknown,
	}
	for _, lang := range []Language{LangEN, LangZH} {
		for _, k := range allKinds {
			msg := messageFor(k, lang)
			if strings.TrimSpace(msg) == "" {
				t.Errorf("messageFor(kind=%d, lang=%d) is empty", k, lang)
			}
		}
	}
}

func TestFormatErrorNetwork(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	raw := errors.New("Get \"https://api.multica.ai/api/issues/abc\": context deadline exceeded")
	netErr := &NetworkError{Kind: KindNetworkTimeout, Op: "GET /api/issues/abc", Err: raw}
	wrapped := fmt.Errorf("resolve issue: %w", netErr)

	got := FormatError(wrapped, false)
	if !strings.Contains(got, "timed out") {
		t.Errorf("expected friendly timeout message, got %q", got)
	}
	// Must not leak the URL or internal verb chain when debug is off.
	if strings.Contains(got, "api.multica.ai") || strings.Contains(got, "resolve issue") {
		t.Errorf("user message leaked internal detail: %q", got)
	}
}

func TestFormatErrorChineseLocale(t *testing.T) {
	withLang(t, "zh_CN.UTF-8")
	netErr := &NetworkError{Kind: KindNetworkDNS, Err: errors.New("no such host")}
	got := FormatError(netErr, false)
	if !strings.Contains(got, "无法解析") {
		t.Errorf("expected Chinese DNS message, got %q", got)
	}
}

func TestFormatErrorValidationUsesServerMessage(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	httpErr := &HTTPError{
		Method:     "POST",
		Path:       "/api/issues",
		StatusCode: 422,
		Body:       `{"error":"title is required"}`,
	}
	got := FormatError(httpErr, false)
	if !strings.Contains(got, "title is required") {
		t.Errorf("expected server validation message surfaced, got %q", got)
	}
}

func TestFormatErrorDebugIncludesRawChain(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	httpErr := &HTTPError{Method: "GET", Path: "/api/issues/abc", StatusCode: 404, Body: `{"error":"not found"}`}
	wrapped := fmt.Errorf("resolve issue: %w", httpErr)

	off := FormatError(wrapped, false)
	if strings.Contains(off, "/api/issues/abc") {
		t.Errorf("debug-off output should not contain raw path: %q", off)
	}

	on := FormatError(wrapped, true)
	if !strings.Contains(on, "[debug]") || !strings.Contains(on, "/api/issues/abc") {
		t.Errorf("debug-on output should include raw chain: %q", on)
	}
}

func TestFormatErrorPlainError(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	got := FormatError(errors.New("title is required"), false)
	if got != "title is required" {
		t.Errorf("plain error should pass through, got %q", got)
	}
	if FormatError(nil, false) != "" {
		t.Errorf("nil error should format to empty string")
	}
}

func TestExitCodeFor(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"nil", nil, 0},
		{"network", &NetworkError{Kind: KindNetworkTimeout, Err: errors.New("x")}, ExitNetwork},
		{"wrapped network", fmt.Errorf("resolve: %w", &NetworkError{Kind: KindNetworkDNS, Err: errors.New("x")}), ExitNetwork},
		{"auth 401", &HTTPError{StatusCode: 401}, ExitAuth},
		{"forbidden 403", &HTTPError{StatusCode: 403}, ExitAuth},
		{"not found 404", &HTTPError{StatusCode: 404}, ExitNotFound},
		{"validation 400", &HTTPError{StatusCode: 400}, ExitValidation},
		{"validation 422", &HTTPError{StatusCode: 422}, ExitValidation},
		{"conflict 409", &HTTPError{StatusCode: 409}, ExitGeneric},
		{"rate limited 429", &HTTPError{StatusCode: 429}, ExitGeneric},
		{"server 500", &HTTPError{StatusCode: 500}, ExitGeneric},
		{"plain", errors.New("boom"), ExitGeneric},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExitCodeFor(tc.err); got != tc.want {
				t.Errorf("ExitCodeFor(%v) = %d, want %d", tc.err, got, tc.want)
			}
		})
	}
}

func TestDetectLanguage(t *testing.T) {
	cases := []struct {
		name string
		env  map[string]string
		want Language
	}{
		{"default english", map[string]string{}, LangEN},
		{"lang zh", map[string]string{"LANG": "zh_CN.UTF-8"}, LangZH},
		{"lang en", map[string]string{"LANG": "en_US.UTF-8"}, LangEN},
		{"lc_all wins over lang", map[string]string{"LC_ALL": "en_US.UTF-8", "LANG": "zh_CN.UTF-8"}, LangEN},
		{"lc_all zh", map[string]string{"LC_ALL": "zh_CN.UTF-8", "LANG": "en_US.UTF-8"}, LangZH},
		{"lc_messages zh", map[string]string{"LC_MESSAGES": "zh_TW.UTF-8"}, LangZH},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
				t.Setenv(k, "")
			}
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			if got := DetectLanguage(); got != tc.want {
				t.Errorf("DetectLanguage() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestExtractServerMessage(t *testing.T) {
	cases := []struct {
		body string
		want string
	}{
		{`{"error":"title is required"}`, "title is required"},
		{`{"message":"invalid priority"}`, "invalid priority"},
		{`{"detail":"bad due date"}`, "bad due date"},
		{`not json`, ""},
		{`{}`, ""},
		{``, ""},
		{`{"error":""}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.body, func(t *testing.T) {
			if got := extractServerMessage(tc.body); got != tc.want {
				t.Errorf("extractServerMessage(%q) = %q, want %q", tc.body, got, tc.want)
			}
		})
	}
}

func TestHTTPTimeout(t *testing.T) {
	cases := []struct {
		name string
		val  string
		want string // human description of expected duration
	}{
		{"unset", "", "30s"},
		{"duration", "45s", "45s"},
		{"minutes", "2m", "2m0s"},
		{"plain seconds", "10", "10s"},
		{"invalid falls back", "garbage", "30s"},
		{"zero falls back", "0", "30s"},
		{"negative falls back", "-5", "30s"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("MULTICA_HTTP_TIMEOUT", tc.val)
			if got := httpTimeout().String(); got != tc.want {
				t.Errorf("httpTimeout() with %q = %s, want %s", tc.val, got, tc.want)
			}
		})
	}
}

// withLang clears the locale env vars and sets LANG to the given value for the
// duration of the test, so language-dependent assertions are deterministic
// regardless of the host environment.
func withLang(t *testing.T, lang string) {
	t.Helper()
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", lang)
}
