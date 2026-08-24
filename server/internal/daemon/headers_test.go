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
			name:     "basic name=value",
			token:    "Authorization=Bearer abc",
			wantName: "Authorization",
			wantVal:  "Bearer abc",
		},
		{
			name:    "empty token errors",
			token:   "",
			wantErr: "empty",
		},
		{
			name:    "missing separator errors",
			token:   "Authorization",
			wantErr: "expected 'Name: Value' or 'Name=value'",
		},
		{
			name:    "empty name errors",
			token:   "=Bearer",
			wantErr: "non-empty",
		},
		{
			name:     "empty value is allowed",
			token:    "X-Empty=",
			wantName: "X-Empty",
			wantVal:  "",
		},
		{
			name:    "CR in value rejected",
			token:   "X-Bad=foo\rInjected: 1",
			wantErr: "carriage return",
		},
		{
			name:    "LF in value rejected",
			token:   "X-Bad=foo\nInjected: 1",
			wantErr: "line feed",
		},
		{
			name:    "NUL in value rejected",
			token:   "X-Bad=foo\x00bar",
			wantErr: "NUL",
		},
		// Colon-as-separator (TIM-142 PR 2 wiring): "X:Injected=foo" used
		// to be rejected because the colon-in-name rule fired. With the
		// colon now treated as a valid separator (matches the documented
		// "Name: Value" form), the colon-at-position-1 wins and the name
		// is just "X". The "Injected=foo" tail becomes a perfectly
		// legitimate value — the injection vector is closed at the parser
		// level rather than at the validator, which is stronger.
		{
			name:     "colon terminates name before value",
			token:    "X:Injected=foo",
			wantName: "X",
			wantVal:  "Injected=foo",
		},
		{
			name:     "colon form (HTTP header style) accepted",
			token:    "Cf-Access-Client-Id: abc.def",
			wantName: "Cf-Access-Client-Id",
			wantVal:  "abc.def",
		},
		{
			name:     "equals form (cobra style) still accepted",
			token:    "Cf-Access-Client-Id=abc.def",
			wantName: "Cf-Access-Client-Id",
			wantVal:  "abc.def",
		},
		{
			name:     "leftmost separator wins",
			token:    "X=val:more",
			wantName: "X",
			wantVal:  "val:more",
		},
		{
			name:    "CR in name rejected",
			token:   "X\rInjected=foo",
			wantErr: "carriage return",
		},
		{
			name:     "tabs and spaces in value allowed",
			token:    "X-Tab=foo\tbar baz",
			wantName: "X-Tab",
			wantVal:  "foo\tbar baz",
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
		{"CR in value rejected", "X-OK", "foo\rInj", "carriage return"},
		{"LF in value rejected", "X-OK", "foo\nInj", "line feed"},
		{"NUL in value rejected", "X-OK", "foo\x00bar", "NUL"},
		{"value with spaces is fine", "X-OK", "bearer foo bar", ""},
		{"value with tabs is fine", "X-OK", "foo\tbar", ""},
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
