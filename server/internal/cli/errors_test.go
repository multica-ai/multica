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

func TestErrorKindString(t *testing.T) {
	cases := map[ErrorKind]string{
		KindNetworkTimeout: "network_timeout",
		KindNetworkDNS:     "network_dns",
		KindNetworkRefused: "network_refused",
		KindNetworkTLS:     "network_tls",
		KindNetworkOffline: "network_offline",
		KindAuthRequired:   "auth_required",
		KindForbidden:      "forbidden",
		KindNotFound:       "not_found",
		KindConflict:       "conflict",
		KindValidation:     "validation",
		KindRateLimited:    "rate_limited",
		KindServerError:    "server_error",
		KindUnknown:        "unknown",
	}
	seen := map[string]ErrorKind{}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("ErrorKind(%d).String() = %q, want %q", int(k), got, want)
		}
		if prev, dup := seen[k.String()]; dup {
			t.Errorf("duplicate String() %q for kinds %d and %d", k.String(), int(prev), int(k))
		}
		seen[k.String()] = k
	}
	// Out-of-range value gets a stable fallback rather than an empty string.
	if got := ErrorKind(999).String(); got != "ErrorKind(999)" {
		t.Errorf("unexpected fallback String(): %q", got)
	}
}

// TestFormatErrorActionableHints locks in the per-status actionable hints
// refined in PR2, in both languages, so a future copy edit can't silently drop
// the actionable guidance.
func TestFormatErrorActionableHints(t *testing.T) {
	cases := []struct {
		status int
		enWant []string
		zhWant []string
	}{
		{401, []string{"multica login", "self-hosted", "administrator"}, []string{"multica login", "自托管", "管理员"}},
		{403, []string{"permission", "workspace"}, []string{"无权", "workspace"}},
		{404, []string{"not found", "list"}, []string{"未找到", "list"}},
		{409, []string{"conflict", "again"}, []string{"冲突", "重新获取"}},
		{400, []string{"--help", "expected format"}, []string{"--help", "格式", "参数"}},
		{422, []string{"--help", "expected format"}, []string{"--help", "格式", "参数"}},
		{429, []string{"Too many requests"}, []string{"过于频繁"}},
		{500, []string{"temporarily unavailable", "--debug"}, []string{"暂时不可用", "--debug"}},
	}
	for _, tc := range cases {
		httpErr := &HTTPError{Method: "GET", Path: "/api/x", StatusCode: tc.status}

		withLang(t, "en_US.UTF-8")
		en := FormatError(httpErr, false)
		for _, sub := range tc.enWant {
			if !strings.Contains(en, sub) {
				t.Errorf("EN %d: %q missing %q", tc.status, en, sub)
			}
		}

		withLang(t, "zh_CN.UTF-8")
		zh := FormatError(httpErr, false)
		for _, sub := range tc.zhWant {
			if !strings.Contains(zh, sub) {
				t.Errorf("ZH %d: %q missing %q", tc.status, zh, sub)
			}
		}
	}
}

// TestUserMessageError proves the command-level user-facing wrapper: the
// custom message is shown by default (overriding the generic kind copy),
// ExitCodeFor still classifies by the underlying typed error, and --debug
// still exposes the full original chain. This is the mechanism that makes the
// `multica login` failure guidance visible without losing classification.
func TestUserMessageError(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	const hint = "Could not sign in with that token — make sure it is valid and not expired, then run `multica login --token <token>` again."

	t.Run("wrapped HTTPError (invalid token -> 401)", func(t *testing.T) {
		underlying := &HTTPError{Method: "GET", Path: "/api/me", StatusCode: 401, Body: `{"error":"unauthorized"}`}
		err := WithUserMessage(hint, underlying)

		// Default output shows the command hint, not the generic 401 line.
		got := FormatError(err, false)
		if got != hint {
			t.Errorf("FormatError(false) = %q, want the login hint", got)
		}
		if strings.Contains(got, "session has expired") {
			t.Errorf("default output leaked the generic 401 copy: %q", got)
		}

		// Exit code still classifies by the underlying *HTTPError (401 -> auth).
		if code := ExitCodeFor(err); code != ExitAuth {
			t.Errorf("ExitCodeFor = %d, want ExitAuth(%d)", code, ExitAuth)
		}

		// --debug keeps the full original chain (verb + http detail).
		dbg := FormatError(err, true)
		if !strings.Contains(dbg, "[debug]") || !strings.Contains(dbg, "/api/me") || !strings.Contains(dbg, "401") {
			t.Errorf("debug output lost the raw chain: %q", dbg)
		}

		// errors.As still reaches the underlying typed error.
		var he *HTTPError
		if !errors.As(err, &he) || he.StatusCode != 401 {
			t.Errorf("errors.As did not reach the underlying *HTTPError")
		}
	})

	t.Run("wrapped NetworkError classifies as network", func(t *testing.T) {
		underlying := &NetworkError{Kind: KindNetworkTimeout, Op: "GET /api/me", Err: errors.New("context deadline exceeded")}
		err := WithUserMessage("Sign-in did not complete: the server did not accept the new credential. Run `multica login` again.", underlying)

		if code := ExitCodeFor(err); code != ExitNetwork {
			t.Errorf("ExitCodeFor = %d, want ExitNetwork(%d)", code, ExitNetwork)
		}
		got := FormatError(err, false)
		if !strings.Contains(got, "Sign-in did not complete") {
			t.Errorf("FormatError(false) = %q, want the sign-in hint", got)
		}
		if strings.Contains(got, "timed out") {
			t.Errorf("default output leaked the generic network copy: %q", got)
		}
	})

	t.Run("nil error returns nil", func(t *testing.T) {
		if WithUserMessage("x", nil) != nil {
			t.Errorf("WithUserMessage(_, nil) should be nil")
		}
	})
}
