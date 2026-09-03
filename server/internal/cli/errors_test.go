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
		KindAuthRequired, KindTaskTokenRejected, KindForbidden, KindNotFound, KindConflict,
		KindValidation, KindRateLimited, KindServerError, KindUnknown,
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

// TestFormatErrorConflictUsesServerMessage pins GH #6264: a 409 body carries a
// hand-written fix ("reply under this comment", "a skill with that name
// exists"), and the generic conflict template actively misdirects by telling
// the caller to retry. The server message must reach the user by default, with
// no --debug required.
func TestFormatErrorConflictUsesServerMessage(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	httpErr := &HTTPError{
		Method:     "POST",
		Path:       "/api/issues/abc/comments",
		StatusCode: 409,
		Body:       `{"error":"parent_id 11111111-1111-1111-1111-111111111111 is not a comment this task may reply under; set parent_id (--parent) to 22222222-2222-2222-2222-222222222222 or a coalesced comment id"}`,
	}
	wrapped := fmt.Errorf("add comment: %w", httpErr)

	got := FormatError(wrapped, false)
	if !strings.Contains(got, "set parent_id (--parent) to 22222222-2222-2222-2222-222222222222") {
		t.Errorf("expected server conflict message surfaced, got %q", got)
	}
	// The retry advice is what sent agents into multi-hour loops; it must not
	// be what they see when the server already named the fix.
	if strings.Contains(got, "Re-fetch the latest state") {
		t.Errorf("generic retry template should be replaced by the server message, got %q", got)
	}
	if strings.Contains(got, "/api/issues/abc/comments") {
		t.Errorf("user message leaked the request path: %q", got)
	}
}

func TestFormatErrorConflictChineseLocale(t *testing.T) {
	withLang(t, "zh_CN.UTF-8")
	httpErr := &HTTPError{StatusCode: 409, Body: `{"error":"a skill with this name already exists"}`}
	got := FormatError(httpErr, false)
	if !strings.Contains(got, "请求冲突：") || !strings.Contains(got, "a skill with this name already exists") {
		t.Errorf("expected Chinese conflict prefix with server message, got %q", got)
	}
}

// TestFormatErrorConflictFallsBackToGenericTemplate is the safety half: only a
// JSON body with a known message field is surfaced, so an HTML error page or a
// proxy response never gets dumped at the user.
func TestFormatErrorConflictFallsBackToGenericTemplate(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	for _, body := range []string{
		`<html><body>409 Conflict</body></html>`,
		`{"code":"runtime_has_active_agents"}`,
		``,
	} {
		got := FormatError(&HTTPError{StatusCode: 409, Body: body}, false)
		if !strings.Contains(got, "Re-fetch the latest state") {
			t.Errorf("body %q: expected generic conflict template, got %q", body, got)
		}
		if body != "" && strings.Contains(got, body) {
			t.Errorf("body %q: raw body leaked into user message %q", body, got)
		}
	}
}

// TestExtractServerMessagePrefersProseOverMachineCode covers the endpoints that
// put a stable code in "error" and the sentence in "message" (issue-table
// cursor responses). The sentence is what helps a person.
func TestExtractServerMessagePrefersProseOverMachineCode(t *testing.T) {
	got := extractServerMessage(`{"error":"cursor_query_mismatch","message":"cursor does not belong to this table branch"}`)
	if got != "cursor does not belong to this table branch" {
		t.Errorf("expected the prose message, got %q", got)
	}

	// With no sentence available the code is still better than nothing.
	if got := extractServerMessage(`{"error":"cursor_query_mismatch"}`); got != "cursor_query_mismatch" {
		t.Errorf("expected the bare code as fallback, got %q", got)
	}

	// Prose in a language without ASCII spaces must not be mistaken for a code.
	if got := extractServerMessage(`{"error":"该名称已被占用"}`); got != "该名称已被占用" {
		t.Errorf("expected non-ASCII prose preserved, got %q", got)
	}
}

// TestFormatErrorValidationPrefersProseOverMachineCode pins an intentional
// behavior change that rides along with the conflict fix: the prose preference
// lives in the shared extractor, so a 400/422 whose "error" holds a machine code
// now shows its "message" instead of the code. Only the issue-table endpoints
// are shaped this way and none of them is reachable from the CLI today, but the
// change is deliberate and should fail loudly if someone reverts it by accident.
func TestFormatErrorValidationPrefersProseOverMachineCode(t *testing.T) {
	withLang(t, "en_US.UTF-8")
	got := FormatError(&HTTPError{
		StatusCode: 422,
		Body:       `{"error":"unsupported_group","code":"group_kind_unsupported","message":"This group type is not supported."}`,
	}, false)
	if !strings.Contains(got, "This group type is not supported.") {
		t.Errorf("expected the prose message, got %q", got)
	}

	// A validation body carrying only a code is unchanged.
	only := FormatError(&HTTPError{StatusCode: 422, Body: `{"error":"title_is_required"}`}, false)
	if !strings.Contains(only, "title_is_required") {
		t.Errorf("code-only validation body should still surface the code, got %q", only)
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
		KindNetworkTimeout:    "network_timeout",
		KindNetworkDNS:        "network_dns",
		KindNetworkRefused:    "network_refused",
		KindNetworkTLS:        "network_tls",
		KindNetworkOffline:    "network_offline",
		KindAuthRequired:      "auth_required",
		KindTaskTokenRejected: "task_token_rejected",
		KindForbidden:         "forbidden",
		KindNotFound:          "not_found",
		KindConflict:          "conflict",
		KindValidation:        "validation",
		KindRateLimited:       "rate_limited",
		KindServerError:       "server_error",
		KindUnknown:           "unknown",
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

// TestFormatErrorRejectedTaskTokenDoesNotSuggestAnotherCredential is GH #7522
// in one assertion. The generic 401 copy tells the reader to run `multica
// login` or ask an administrator for valid credentials. That is right for a
// person and wrong for an agent: after its task token stopped working mid-run,
// one read the daemon owner's profile PAT and kept working under the member's
// identity. A 401 on a task token has to read as "stop", never as "find a
// working credential".
//
// It must also not guess why the token was rejected. A terminal task is the
// usual cause, but the same 401 covers a malformed token, one sent to the wrong
// server, and one dropped by an unrelated cleanup — so a message that asserts
// the task finished is a claim the CLI cannot make.
//
// The wiring — which requests are marked task-scoped in the first place — is
// covered by TestHTTPErrorTaskScopedFollowsTheRequestNotTheClient, which drives
// a real client instead of hand-building the error.
func TestFormatErrorRejectedTaskTokenDoesNotSuggestAnotherCredential(t *testing.T) {
	httpErr := &HTTPError{Method: "GET", Path: "/api/me", StatusCode: 401, TaskScoped: true}

	for _, tc := range []struct {
		lang    string
		want    []string
		refuted []string
	}{
		{"en_US.UTF-8",
			[]string{"rejected", "no longer usable", "Stop here", "do not retry",
				"do not fall back to a profile or member credential"},
			// No sign-in advice, and no claim about a cause it cannot verify.
			[]string{"multica login", "administrator", "cancelled", "finished", "terminal state"}},
		{"zh_CN.UTF-8",
			[]string{"已被拒绝", "不再可用", "不要重试", "不要改用 profile 或成员凭证"},
			[]string{"multica login", "管理员", "已完成", "被取消", "终态"}},
	} {
		withLang(t, tc.lang)
		got := FormatError(httpErr, false)
		for _, sub := range tc.want {
			if !strings.Contains(got, sub) {
				t.Errorf("%s: %q missing %q", tc.lang, got, sub)
			}
		}
		for _, sub := range tc.refuted {
			if strings.Contains(got, sub) {
				t.Errorf("%s: %q must not contain %q", tc.lang, got, sub)
			}
		}
	}

	// Glossary: an agent execution is `task` in Chinese; 任务 is the product
	// entity a user files (an issue). Writing this message with 任务 would say
	// the user's issue was rejected. See conventions.zh.mdx.
	withLang(t, "zh_CN.UTF-8")
	zh := FormatError(httpErr, false)
	if strings.Contains(zh, "任务") {
		t.Errorf("zh copy used 任务 for an agent execution; it must stay `task`: %q", zh)
	}
	if !strings.Contains(zh, "task") {
		t.Errorf("zh copy dropped the `task` term entirely: %q", zh)
	}

	// A member 401 is unchanged: that really is an expired login.
	withLang(t, "en_US.UTF-8")
	member := FormatError(&HTTPError{Method: "GET", Path: "/api/me", StatusCode: 401}, false)
	if !strings.Contains(member, "multica login") {
		t.Errorf("a member 401 should still point at sign-in, got %q", member)
	}

	// Exit classification is unchanged — still an auth failure.
	if got := ExitCodeFor(httpErr); got != ExitAuth {
		t.Errorf("ExitCodeFor(rejected task token) = %d, want %d", got, ExitAuth)
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
