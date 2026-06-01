package main

import (
	"strings"
	"testing"
)

func TestEndpointExtractor(t *testing.T) {
	cases := []struct {
		name    string
		hits    []StringHit
		want    string
		wantErr string
	}{
		{
			name: "happy path — exact URL embedded",
			hits: []StringHit{
				{Offset: 100, Value: "var ep=\"https://platform.claude.com/v1/oauth/token\";"},
			},
			want: "https://platform.claude.com/v1/oauth/token",
		},
		{
			name:    "missing endpoint URL",
			hits:    []StringHit{{Offset: 100, Value: "/v1/oauth/token"}},
			wantErr: "not present",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := endpointExtractor().Run(tc.hits)
			checkResult(t, got, err, tc.want, tc.wantErr)
		})
	}
}

func TestVersionHeaderExtractor(t *testing.T) {
	cases := []struct {
		name    string
		hits    []StringHit
		want    string
		wantErr string
	}{
		{
			name: "happy path",
			hits: []StringHit{
				{Offset: 100, Value: "oauth-2025-04-20"},
				{Offset: 200, Value: "some other string"},
			},
			want: "oauth-2025-04-20",
		},
		{
			name:    "no header",
			hits:    []StringHit{{Offset: 100, Value: "other"}},
			wantErr: "no oauth-YYYY-MM-DD",
		},
		{
			name: "ambiguous",
			hits: []StringHit{
				{Offset: 100, Value: "oauth-2025-04-20"},
				{Offset: 200, Value: "oauth-2026-01-01"},
			},
			wantErr: "multiple candidates",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := versionHeaderExtractor().Run(tc.hits)
			checkResult(t, got, err, tc.want, tc.wantErr)
		})
	}
}

func TestClientIDExtractor(t *testing.T) {
	const anchor = "platform.claude.com/oauth/code/callback"
	const goodUUID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	const otherUUID = "00000000-1111-2222-3333-444444444444"

	cases := []struct {
		name    string
		hits    []StringHit
		want    string
		wantErr string
	}{
		{
			name: "happy path",
			hits: []StringHit{
				{Offset: 1000, Value: "...some other string..."},
				{Offset: 1500, Value: anchor},
				{Offset: 1700, Value: goodUUID},
			},
			want: goodUUID,
		},
		{
			name: "uuid too far from anchor",
			hits: []StringHit{
				{Offset: 1500, Value: anchor},
				{Offset: 100000, Value: goodUUID},
			},
			wantErr: "no UUID found within",
		},
		{
			name:    "anchor missing",
			hits:    []StringHit{{Offset: 1700, Value: goodUUID}},
			wantErr: "anchor",
		},
		{
			name: "ambiguous",
			hits: []StringHit{
				{Offset: 1500, Value: anchor},
				{Offset: 1700, Value: goodUUID},
				{Offset: 1900, Value: otherUUID},
			},
			wantErr: "ambiguous",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := clientIDExtractor().Run(tc.hits)
			checkResult(t, got, err, tc.want, tc.wantErr)
		})
	}
}

func TestScopesExtractor(t *testing.T) {
	all := []StringHit{
		{Offset: 100, Value: "user:profile user:inference user:sessions:claude_code user:mcp_servers"},
	}
	got, err := scopesExtractor().Run(all)
	if err != nil {
		t.Fatalf("happy path: %v", err)
	}
	if got != "user:profile user:inference user:sessions:claude_code user:mcp_servers" {
		t.Errorf("got %q", got)
	}

	missing := []StringHit{
		{Offset: 100, Value: "user:profile user:inference user:sessions:claude_code"},
	}
	_, err = scopesExtractor().Run(missing)
	if err == nil || !strings.Contains(err.Error(), "user:mcp_servers") {
		t.Errorf("missing-scope error = %v", err)
	}
}

func TestRun_MultiFailureReporting(t *testing.T) {
	// Hits missing both /v1/oauth/token and the version header.
	hits := []StringHit{
		{Offset: 100, Value: "api.anthropic.com"},
		{Offset: 200, Value: "user:profile user:inference user:sessions:claude_code user:mcp_servers"},
		{Offset: 1500, Value: "platform.claude.com/oauth/code/callback"},
		{Offset: 1700, Value: "9d1c250a-e61b-44d9-88ed-5944d1962f5e"},
	}
	_, errs := Run(hits)
	if len(errs) < 2 {
		t.Fatalf("expected >= 2 errors (endpoint + version_header), got %d: %v", len(errs), errs)
	}
	var sawEndpoint, sawVersion bool
	for _, e := range errs {
		s := e.Error()
		if strings.HasPrefix(s, "endpoint:") {
			sawEndpoint = true
		}
		if strings.HasPrefix(s, "version_header:") {
			sawVersion = true
		}
	}
	if !sawEndpoint || !sawVersion {
		t.Errorf("expected both endpoint + version_header failures: %v", errs)
	}
}

func checkResult(t *testing.T, got string, err error, want, wantErr string) {
	t.Helper()
	if wantErr != "" {
		if err == nil || !strings.Contains(err.Error(), wantErr) {
			t.Errorf("err = %v, want substring %q", err, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
