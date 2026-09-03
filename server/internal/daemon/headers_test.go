package daemon

import (
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func TestExtraHeaderFromFlag(t *testing.T) {
	cases := []struct {
		name     string
		token    string
		wantName string
		wantVal  string
		wantErr  string // substring; "" means no error
	}{
		{
			name:     "basic Name: Value",
			token:    "Cf-Access-Client-Id: abc.def",
			wantName: "Cf-Access-Client-Id",
			wantVal:  "abc.def",
		},
		{
			name:    "reserved header rejected at parse time",
			token:   "Authorization: Bearer abc",
			wantErr: "reserved",
		},
		{
			name:    "empty token errors",
			token:   "",
			wantErr: "empty",
		},
		{
			name:    "whitespace-only token errors",
			token:   "   ",
			wantErr: "empty",
		},
		{
			name:    "missing colon errors",
			token:   "X-Auth",
			wantErr: "expected 'Name: Value'",
		},
		{
			name:    "Name=value (cobra form) is rejected",
			token:   "X-Auth=Bearer abc",
			wantErr: "expected 'Name: Value'",
		},
		{
			name:    "empty name errors",
			token:   ": Bearer",
			wantErr: "non-empty",
		},
		{
			name:     "empty value is allowed",
			token:    "X-Empty:",
			wantName: "X-Empty",
			wantVal:  "",
		},
		{
			name:    "CR in value rejected",
			token:   "X-Bad: foo\rInjected: 1",
			wantErr: "carriage return",
		},
		{
			name:    "LF in value rejected",
			token:   "X-Bad: foo\nInjected: 1",
			wantErr: "line feed",
		},
		{
			name:    "NUL in value rejected",
			token:   "X-Bad: foo\x00bar",
			wantErr: "NUL",
		},
		{
			name:    "CR in name rejected",
			token:   "X\rInj: foo",
			wantErr: "carriage return",
		},
		{
			name:    "LF in name rejected",
			token:   "X\nInj: foo",
			wantErr: "line feed",
		},
		{
			name:    "NUL in name rejected",
			token:   "X\x00: foo",
			wantErr: "NUL",
		},
		{
			name:    "name with space rejected (httpguts)",
			token:   "X Bad: foo",
			wantErr: "not a valid HTTP header field name",
		},
		{
			name:     "tabs and spaces in value allowed",
			token:    "X-Tab: foo\tbar baz",
			wantName: "X-Tab",
			wantVal:  "foo\tbar baz",
		},
		{
			name:     "first colon wins",
			token:    "X:val:more",
			wantName: "X",
			wantVal:  "val:more",
		},
		{
			name:     "no colon when value is empty",
			token:    "X-Empty:",
			wantName: "X-Empty",
			wantVal:  "",
		},
		{
			name:     "outer whitespace trimmed",
			token:    "   X-Outer:   value with edges   ",
			wantName: "X-Outer",
			wantVal:  "value with edges",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			name, value, err := ExtraHeaderFromFlag(tc.token)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (name=%q, value=%q)", tc.wantErr, name, value)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if name != tc.wantName {
				t.Errorf("name = %q, want %q", name, tc.wantName)
			}
			if value != tc.wantVal {
				t.Errorf("value = %q, want %q", value, tc.wantVal)
			}
		})
	}
}

func TestExtraHeaderFromFlag_RejectsReservedHeaders(t *testing.T) {
	// Every reserved header in IsReservedHeader's blocklist. The point
	// is that the daemon refuses to start rather than producing a
	// request with both an operator value and a Multica-managed value
	// (cf. SELF_HOSTING_ADVANCED.md:570 — the "Authorization" leak
	// review item).
	cases := []struct {
		name  string
		token string
	}{
		{"Authorization", "Authorization: Bearer real-token"},
		{"Host", "Host: evil.example.com"},
		{"Content-Length", "Content-Length: 999999"},
		{"Content-Type", "Content-Type: text/plain"},
		{"Connection", "Connection: close"},
		{"Upgrade", "Upgrade: websocket"},
		{"Sec-Websocket-Key", "Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ=="},
		{"Sec-Websocket-Accept", "Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbF+xOo="},
		{"Sec-Websocket-Version", "Sec-WebSocket-Version: 99"},
		{"Sec-Websocket-Protocol", "Sec-WebSocket-Protocol: evil"},
		{"Sec-Websocket-Extensions", "Sec-WebSocket-Extensions: evil"},
		{"X-Client-Platform", "X-Client-Platform: forged"},
		{"X-Client-Version", "X-Client-Version: 99.0.0"},
		{"X-Client-OS", "X-Client-OS: freebsd"},
		{"X-Client-Capabilities", "X-Client-Capabilities: bogus"},
		{"X-Workspace-Id", "X-Workspace-Id: ws-1"},
		{"X-Agent-Id", "X-Agent-Id: agent-1"},
		{"X-Task-Id", "X-Task-Id: task-1"},
		{"Forwarded", "Forwarded: for=1.2.3.4"},
		{"X-Forwarded-For", "X-Forwarded-For: 1.2.3.4"},
		{"X-Forwarded-Host", "X-Forwarded-Host: evil.example.com"},
		{"X-Forwarded-Proto", "X-Forwarded-Proto: http"},
		{"X-Forwarded-User", "X-Forwarded-User: admin"},
		{"X-Forwarded-Email", "X-Forwarded-Email: admin@evil.example.com"},
		{"X-Forwarded-Groups", "X-Forwarded-Groups: admins"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := ExtraHeaderFromFlag(tc.token)
			if err == nil {
				t.Fatalf("expected reserved-header rejection for %q, got nil", tc.token)
			}
			if !strings.Contains(err.Error(), "reserved") {
				t.Fatalf("err = %q, want substring %q", err.Error(), "reserved")
			}
		})
	}
}

func TestIsReservedHeader(t *testing.T) {
	// Every reserved name must match in any case. Operators who type
	// "authorization" or "X-WORKSPACE-ID" should see the same rejection.
	reserved := []string{
		"Authorization", "authorization", "AUTHORIZATION",
		"Host", "host",
		"Content-Length", "content-length", "CONTENT-LENGTH",
		"Content-Type", "content-type",
		"Connection", "connection",
		"Upgrade", "upgrade",
		"Sec-Websocket-Key", "sec-websocket-key", "SEC-WEBSOCKET-KEY",
		"Sec-Websocket-Accept", "Sec-Websocket-Version", "Sec-Websocket-Protocol", "Sec-Websocket-Extensions",
		"X-Client-Platform", "x-client-platform", "X-CLIENT-PLATFORM",
		"X-Client-Version", "X-Client-OS", "X-Client-Capabilities",
		"X-Workspace-Id", "x-workspace-id",
		"X-Agent-Id", "x-agent-id",
		"X-Task-Id", "x-task-id",
		"Forwarded", "forwarded",
		"X-Forwarded-For", "x-forwarded-for",
		"X-Forwarded-Host", "X-Forwarded-Proto", "X-Forwarded-User",
		"X-Forwarded-Email", "X-Forwarded-Groups", "X-Forwarded-Something-New",
	}
	for _, name := range reserved {
		if !IsReservedHeader(name) {
			t.Errorf("IsReservedHeader(%q) = false, want true", name)
		}
	}

	nonReserved := []string{
		"X-Custom", "Cf-Access-Client-Id", "Cf-Access-Client-Secret",
		"X-Trace-Id", "X-Request-Id", "Accept", "User-Agent",
	}
	for _, name := range nonReserved {
		if IsReservedHeader(name) {
			t.Errorf("IsReservedHeader(%q) = true, want false", name)
		}
	}
}

func TestExtraHeadersFromSpec(t *testing.T) {
	cases := []struct {
		name    string
		spec    string
		want    http.Header
		wantErr string // substring; "" means no error
	}{
		{
			name: "empty spec returns nil",
			spec: "",
			want: nil,
		},
		{
			name: "whitespace-only spec returns nil",
			spec: "  \n\t  \n",
			want: nil,
		},
		{
			name: "single header",
			spec: "X-Auth: bearer xyz",
			want: http.Header{"X-Auth": {"bearer xyz"}},
		},
		{
			name: "multi-line with comments and blank lines",
			spec: "# daemon auth header\nX-Custom: foo\n\n# another comment\nX-Other: bar\n",
			want: http.Header{
				"X-Custom": {"foo"},
				"X-Other":  {"bar"},
			},
		},
		{
			name: "duplicate headers append to slice",
			spec: "X-Multi: a\nX-Multi: b\n",
			want: http.Header{"X-Multi": {"a", "b"}},
		},
		{
			name:    "missing colon errors",
			spec:    "X-NoColon foo",
			wantErr: "expected 'Name: Value'",
		},
		{
			name: "empty value is allowed",
			spec: "X-Nocolon:",
			want: http.Header{"X-Nocolon": {""}},
		},
		{
			name:    "line with only colon errors with empty name",
			spec:    ": foo",
			wantErr: "non-empty",
		},
		{
			name:    "CR in value rejected",
			spec:    "X-Bad: foo\rInjected: 1",
			wantErr: "carriage return",
		},
		{
			name:    "bare name on first line errors before second line is parsed",
			spec:    "X-NoColon\nInjected: foo",
			wantErr: "expected 'Name: Value'",
		},
		{
			name:    "NUL in value rejected",
			spec:    "X-Bad: foo\x00bar",
			wantErr: "NUL",
		},
		{
			name: "CRLF line endings accepted",
			spec: "X-OK: foo\r\nX-Other: bar",
			want: http.Header{
				"X-Ok":    {"foo"},
				"X-Other": {"bar"},
			},
		},
		{
			name:    "first error wins (subsequent lines dropped)",
			spec:    "X-Bad: foo\rInj: 1\n\nX-Good: bar",
			wantErr: "carriage return",
		},
		{
			name:    "CR in name rejected",
			spec:    "X\rInj: foo",
			wantErr: "carriage return",
		},
		{
			name:    "reserved header rejected",
			spec:    "Authorization: Bearer real-token",
			wantErr: "reserved",
		},
		{
			name:    "X-Forwarded-For rejected",
			spec:    "X-Forwarded-For: 1.2.3.4",
			wantErr: "reserved",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			hdr, err := ExtraHeadersFromSpec(tc.spec)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (headers=%v)", tc.wantErr, hdr)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tc.want) == 0 {
				if len(hdr) != 0 {
					t.Fatalf("want empty headers, got %v", hdr)
				}
				return
			}
			if !reflect.DeepEqual(hdr, tc.want) {
				t.Fatalf("headers = %v, want %v", hdr, tc.want)
			}
		})
	}
}

// TestExtraHeadersFromSpec_RejectsHeaderCountOverLimit pins the
// MaxExtraHeaders bound at parse time. Build a spec one header over
// the limit and confirm ExtraHeadersFromSpec fails at the offending
// line, not after building a giant map.
func TestExtraHeadersFromSpec_RejectsHeaderCountOverLimit(t *testing.T) {
	var b strings.Builder
	for i := 0; i <= MaxExtraHeaders; i++ {
		b.WriteString("X-H")
		b.WriteByte(':')
		// Two digits is enough for 64; we want "i+1" worth of lines
		// but the bound is by *unique* names, so let each line be a
		// distinct name.
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("X-H")
		b.WriteByte('0' + byte(i/10))
		b.WriteByte('0' + byte(i%10))
		b.WriteString(": v")
		b.WriteByte('\n')
	}
	_, err := ExtraHeadersFromSpec(b.String())
	if err == nil {
		t.Fatalf("expected count-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "too many extra headers") {
		t.Fatalf("err = %q, want substring %q", err.Error(), "too many extra headers")
	}
}

// TestExtraHeadersFromSpec_RejectsAggregateBytesOverLimit pins the
// MaxExtraHeadersBytes bound. One header whose value fills the whole
// budget then a tiny second one that pushes us over must fail.
func TestExtraHeadersFromSpec_RejectsAggregateBytesOverLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("X-Big: ")
	for i := 0; i < MaxExtraHeadersBytes; i++ {
		b.WriteByte('a')
	}
	b.WriteByte('\n')
	b.WriteString("X-Tiny: v")
	_, err := ExtraHeadersFromSpec(b.String())
	if err == nil {
		t.Fatalf("expected aggregate-size error, got nil")
	}
	if !strings.Contains(err.Error(), "aggregate size") {
		t.Fatalf("err = %q, want substring %q", err.Error(), "aggregate size")
	}
}

func TestValidateHeaderNameValue(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		value   string
		wantErr string // substring; "" means no error
	}{
		{"both non-empty ok", "X-Custom", "foo", ""},
		{"empty header name rejected", "", "foo", "header name"},
		{"whitespace header name rejected", "  ", "foo", "header name"},
		{"CR in name rejected", "X\rInj", "foo", "carriage return"},
		{"LF in name rejected", "X\nInj", "foo", "line feed"},
		{"NUL in name rejected", "X\x00", "foo", "NUL"},
		{"colon in name rejected", "X:Inj", "foo", "colon"},
		{"space in name rejected (httpguts)", "X Bad", "foo", "not a valid HTTP header field name"},
		{"CR in value rejected", "X-OK", "foo\rInj", "carriage return"},
		{"LF in value rejected", "X-OK", "foo\nInj", "line feed"},
		{"NUL in value rejected", "X-OK", "foo\x00bar", "NUL"},
		{"value with spaces is fine", "X-OK", "bearer foo bar", ""},
		{"value with tabs is fine", "X-OK", "foo\tbar", ""},
		{"Authorization rejected", "Authorization", "Bearer foo", "reserved"},
		{"Host rejected", "Host", "evil.example.com", "reserved"},
		{"Content-Type rejected", "Content-Type", "text/plain", "reserved"},
		{"X-Client-Platform rejected", "X-Client-Platform", "forged", "reserved"},
		{"X-Forwarded-For rejected", "X-Forwarded-For", "1.2.3.4", "reserved"},
		{"Sec-Websocket-Key rejected", "Sec-Websocket-Key", "k", "reserved"},
		{"X-Forwarded-Something-New rejected", "X-Forwarded-Something-New", "x", "reserved"},
		{"Sec-Websocket-Something-New rejected", "Sec-Websocket-Something-New", "x", "reserved"},
		{"X-Client-Something-New rejected", "X-Client-Something-New", "x", "reserved"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateHeaderNameValue(tc.header, tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestResolveExtraHeaders(t *testing.T) {
	t.Setenv(ExtraHeadersEnv, "")

	cases := []struct {
		name     string
		override http.Header
		envSpec  string // "" means unset
		file     map[string]string
		want     http.Header
		wantErr  string // substring; "" means no error
	}{
		{
			name: "override wins",
			override: http.Header{
				"X-Flag": {"flag-val"},
			},
			envSpec: "X-Env: env-val",
			file:    map[string]string{"X-File": "file-val"},
			want: http.Header{
				"X-Flag": {"flag-val"},
			},
		},
		{
			name:     "env wins over file",
			override: nil,
			envSpec:  "X-Auth: bearer xyz\nX-Other: value",
			file:     map[string]string{"X-File": "file-val"},
			want: http.Header{
				"X-Auth":  {"bearer xyz"},
				"X-Other": {"value"},
			},
		},
		{
			name:     "file used when flag and env unset",
			override: nil,
			envSpec:  "",
			file:     map[string]string{"X-File": "file-val", "X-Other": "other-val"},
			want: http.Header{
				"X-File":  {"file-val"},
				"X-Other": {"other-val"},
			},
		},
		{
			// A non-nil but empty override is a distinct signal from
			// "flag not passed": the caller is explicitly saying
			// "zero headers, ignore env and file". resolveExtraHeaders
			// must surface that intent instead of falling through to
			// a populated env / file source.
			name:     "empty override beats populated env and file",
			override: http.Header{},
			envSpec:  "X-Env: env-val",
			file:     map[string]string{"X-File": "file-val"},
			want:     http.Header{},
		},
		{
			name:     "all unset returns nil",
			override: nil,
			envSpec:  "",
			file:     nil,
			want:     nil,
		},
		{
			name:     "empty file map returns nil",
			override: nil,
			envSpec:  "",
			file:     map[string]string{},
			want:     nil,
		},
		{
			name:     "whitespace-only env returns nil",
			override: nil,
			envSpec:  "   \n  \n",
			file:     nil,
			want:     nil,
		},
		{
			name:     "env with CR rejected",
			override: nil,
			envSpec:  "X-Bad: foo\rInj: 1",
			wantErr:  "carriage return",
		},
		{
			name:     "file with CR rejected",
			override: nil,
			file:     map[string]string{"X-Bad": "foo\rInj"},
			wantErr:  "carriage return",
		},
		{
			name:     "file with colon in name rejected",
			override: nil,
			file:     map[string]string{"X:Inj": "ok"},
			wantErr:  "colon",
		},
		{
			name:     "file with reserved name rejected",
			override: nil,
			file:     map[string]string{"Authorization": "Bearer foo"},
			wantErr:  "reserved",
		},
		{
			name:     "env with reserved name rejected",
			override: nil,
			envSpec:  "X-Forwarded-For: 1.2.3.4",
			wantErr:  "reserved",
		},
		{
			// When an override is present, the config-file map is
			// short-circuited (flag-wins-over-config). A reserved
			// header in the file must therefore NOT cause a startup
			// error here — the file is silently ignored. Reserve the
			// validation surface for the active source only.
			name:     "reserved header in file is ignored when override is set",
			override: http.Header{"X-OK": {"ok-val"}},
			envSpec:  "",
			file:     map[string]string{"Authorization": "Bearer foo"},
			want:     http.Header{"X-OK": {"ok-val"}},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(ExtraHeadersEnv, tc.envSpec)
			got, err := resolveExtraHeaders(tc.override, tc.file)
			if tc.wantErr != "" {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (headers=%v)", tc.wantErr, got)
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %q, want substring %q", err.Error(), tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(tc.want) == 0 && len(got) == 0 {
				return
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("headers = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestResolveExtraHeaders_OverrideCountOverLimit covers the same
// bounds from the override path: when --extra-header produced more
// than MaxExtraHeaders entries (the CLI flag loop already enforces
// this incrementally, but a future caller that hands resolveExtraHeaders
// a pre-built override must still trip the same check).
func TestResolveExtraHeaders_OverrideCountOverLimit(t *testing.T) {
	// Build MaxExtraHeaders+1 distinct entries so the len() check fires.
	big := make(http.Header, MaxExtraHeaders+1)
	for i := 0; i <= MaxExtraHeaders; i++ {
		// Distinct names: "X-H-0", "X-H-1", ... never collide.
		big[http.CanonicalHeaderKey("X-H-"+itoa(i))] = []string{"v"}
	}
	_, err := resolveExtraHeaders(big, nil)
	if err == nil {
		t.Fatalf("expected count-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "too many entries") {
		t.Fatalf("err = %q, want substring %q", err.Error(), "too many entries")
	}
}

// itoa is a tiny std-free integer formatter used only inside this
// test to build distinct header names without pulling in strconv for a
// single concatenation site. Avoids the aliasing bug from a %-based
// scheme that reuses the same 26 letters for 65 entries.
func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	n := len(buf)
	for i > 0 {
		n--
		buf[n] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[n:])
}

// TestResolveExtraHeaders_OverrideBytesOverLimit covers the same
// bounds from the override path for the byte ceiling.
func TestResolveExtraHeaders_OverrideBytesOverLimit(t *testing.T) {
	big := http.Header{"X-Big": {strings.Repeat("a", MaxExtraHeadersBytes+1)}}
	_, err := resolveExtraHeaders(big, nil)
	if err == nil {
		t.Fatalf("expected byte-limit error, got nil")
	}
	if !strings.Contains(err.Error(), "aggregate size") {
		t.Fatalf("err = %q, want substring %q", err.Error(), "aggregate size")
	}
}
