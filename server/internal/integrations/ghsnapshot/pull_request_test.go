package ghsnapshot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestFetchPullRequestUsesInstallationAndRejectsRedirect(t *testing.T) {
	for _, redirect := range []bool{false, true} {
		var contacted bool
		target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { contacted = true }))
		defer target.Close()
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("Authorization") != "Bearer installation-token" || r.URL.Path != "/repos/owner/repo/pulls/12" {
				t.Error("wrong auth or route")
			}
			if redirect {
				http.Redirect(w, r, target.URL, 302)
				return
			}
			w.Write([]byte(`{"number":12,"body":"plain POL-1"}`))
		}))
		defer server.Close()
		c := newTestClient(t, server.URL)
		c.tokens[42] = cachedToken{token: "installation-token", expiry: time.Now().Add(time.Hour)}
		body, err := c.FetchPullRequest(context.Background(), 42, "owner", "repo", 12)
		if redirect {
			if err == nil || contacted {
				t.Fatal("redirect exposed credential")
			}
		} else if err != nil || len(body) == 0 {
			t.Fatal(err)
		}
	}
}
