package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

func newBriefDigestTestCmd(t *testing.T, args ...string) *cobra.Command {
	t.Helper()
	cmd := &cobra.Command{Use: "digest"}
	addBriefDigestFlags(cmd)
	if err := cmd.ParseFlags(args); err != nil {
		t.Fatalf("parse flags: %v", err)
	}
	return cmd
}

func TestParseBriefSince(t *testing.T) {
	now := time.Date(2026, 5, 10, 8, 0, 0, 0, time.UTC)
	cases := map[string]time.Time{
		"":                     now.Add(-24 * time.Hour),
		"24h":                  now.Add(-24 * time.Hour),
		"90m":                  now.Add(-90 * time.Minute),
		"7d":                   now.Add(-7 * 24 * time.Hour),
		"2026-05-01T00:00:00Z": time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	for in, want := range cases {
		got, err := parseBriefSince(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("parseBriefSince(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	for _, bad := range []string{"yesterday", "-5h", "0d", "xd"} {
		if _, err := parseBriefSince(bad, now); err == nil {
			t.Errorf("parseBriefSince(%q) should fail", bad)
		}
	}
}

func TestBriefDigestSendsSinceAndPrintsJSON(t *testing.T) {
	var gotSince string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/office/digest" {
			t.Errorf("request = %s %s, want GET /api/office/digest", r.Method, r.URL.Path)
		}
		gotSince = r.URL.Query().Get("since")
		_ = json.NewEncoder(w).Encode(map[string]any{"squads": []any{}, "omitted_squad_count": 0})
	}))
	defer srv.Close()
	setCLITestServerEnv(t, srv.URL)

	before := time.Now()
	out, err := captureStdout(t, func() error {
		return runBriefDigest(newBriefDigestTestCmd(t, "--since", "6h", "--json"), nil)
	})
	if err != nil {
		t.Fatalf("runBriefDigest: %v", err)
	}
	since, err := time.Parse(time.RFC3339, gotSince)
	if err != nil {
		t.Fatalf("since %q is not RFC 3339: %v", gotSince, err)
	}
	if d := before.Sub(since); d < 5*time.Hour || d > 7*time.Hour {
		t.Fatalf("since = %s, want about 6h before %s", gotSince, before)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(out), &decoded); err != nil {
		t.Fatalf("--json output is not JSON: %v\n%s", err, out)
	}
}

func TestBriefDigestRejectsBadSinceBeforeCallingTheServer(t *testing.T) {
	err := runBriefDigest(newBriefDigestTestCmd(t, "--since", "soon"), nil)
	if err == nil || !strings.Contains(err.Error(), "--since") {
		t.Fatalf("err = %v, want a --since error", err)
	}
}
