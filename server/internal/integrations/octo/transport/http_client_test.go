package transport

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGetGroupMembers_EscapesGroupNo is the regression guard for the path
// injection fix: a groupNo carrying URL-significant characters must be escaped
// so it stays inside its path segment and cannot inject a query string or
// traverse toward other bot endpoints.
func TestGetGroupMembers_EscapesGroupNo(t *testing.T) {
	cases := []struct {
		name    string
		groupNo string
		// wantContains asserts the (escaped) path segment is intact; wantNotRaw
		// asserts the dangerous raw form never reaches the server.
		wantContains string
		wantNotRaw   string
	}{
		{"plain numeric", "12345", "/v1/bot/groups/12345/members", ""},
		{"query injection", "x?force_refresh=true", "/v1/bot/groups/x%3Fforce_refresh=true/members", "?force_refresh=true/members"},
		{"path traversal", "../../v1/bot/admin", "/members", "/v1/bot/groups/../../v1/bot/admin/members"},
		{"slash in id", "a/b", "%2F", "/groups/a/b/"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotRequestURI string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotRequestURI = r.RequestURI
				_, _ = w.Write([]byte(`{"members":[]}`))
			}))
			defer srv.Close()

			c2 := NewHTTPClient(srv.URL, "bf_test")
			if _, err := c2.GetGroupMembers(context.Background(), c.groupNo); err != nil {
				t.Fatalf("GetGroupMembers: %v", err)
			}
			if !strings.Contains(gotRequestURI, c.wantContains) {
				t.Errorf("request URI %q does not contain %q", gotRequestURI, c.wantContains)
			}
			if c.wantNotRaw != "" && strings.Contains(gotRequestURI, c.wantNotRaw) {
				t.Errorf("request URI %q leaked unescaped sequence %q", gotRequestURI, c.wantNotRaw)
			}
		})
	}
}
