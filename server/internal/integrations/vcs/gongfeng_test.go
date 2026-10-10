package vcs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

// The fixture models the documented MR hook, not a captured live delivery:
// https://code.tencent.com/help/webhooks
func gongfengFixture(t *testing.T) []byte {
	t.Helper()
	body, err := os.ReadFile("testdata/gongfeng-merge-request.json")
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func gongfengAdapter(t *testing.T) Provider {
	t.Helper()
	p, ok := For("gongfeng")
	if !ok || !Kind("gongfeng").Valid() {
		t.Fatal("gongfeng provider is not registered")
	}
	return p
}

func TestGongfengWebhookAuthentication(t *testing.T) {
	p := gongfengAdapter(t)
	for _, tc := range []struct {
		name, secret, token string
		want                bool
	}{
		{"matching", "secret", "secret", true},
		{"wrong", "secret", "wrong", false},
		{"missing", "secret", "", false},
		{"empty secret", "", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			h := http.Header{}
			h.Set("X-Token", tc.token)
			if got := p.VerifySignature(tc.secret, h, nil); got != tc.want {
				t.Fatalf("VerifySignature = %v, want %v", got, tc.want)
			}
		})
	}
	h := http.Header{"X-Gitlab-Token": []string{"secret"}}
	if p.VerifySignature("secret", h, nil) {
		t.Fatal("a GitLab header must not authenticate a Gongfeng delivery")
	}
	for event, want := range map[string]EventKind{
		"Merge Request Hook": EventPullRequest,
		"Pipeline Hook":      EventOther,
		"Push Hook":          EventOther,
		"":                   EventOther,
	} {
		h.Set("X-Event", event)
		if got := p.EventKind(h); got != want {
			t.Fatalf("EventKind(%q) = %v, want %v", event, got, want)
		}
	}
}

func TestGongfengParsesMRIdentityAndTime(t *testing.T) {
	p := gongfengAdapter(t)
	pr, err := p.ParsePullRequest(gongfengFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	if pr.RepoOwner != "acme/team" || pr.RepoName != "widget" || pr.Number != 7 {
		t.Fatalf("MR must use the target repository path and iid: %+v", pr)
	}
	if pr.State != "open" || pr.Branch != "mul-123-fix-login" || pr.HeadSHA != "deadbeef" || pr.Body != "Closes MUL-123" {
		t.Fatalf("unexpected MR: %+v", pr)
	}
	if pr.CreatedAt != "2026-10-09T08:00:00Z" || pr.UpdatedAt != "2026-10-09T08:00:00.123456Z" {
		t.Fatalf("timestamp precision lost: %+v", pr)
	}
	if pr.AuthorLogin != "alice" || pr.Terminal() {
		t.Fatalf("unexpected author or terminal action: %+v", pr)
	}
}

func TestGongfengMRTransitions(t *testing.T) {
	p := gongfengAdapter(t)
	for _, tc := range []struct {
		action, state, title, want string
		terminal                   bool
	}{
		{"open", "opened", "WIP: MUL-123", "draft", false},
		{"update", "opened", "Draft: MUL-123", "draft", false},
		{"update", "opened", "MUL-123", "open", false},
		{"close", "closed", "MUL-123", "closed", true},
		{"reopen", "opened", "MUL-123", "open", false},
		{"reopen", "reopened", "MUL-123", "open", false},
		{"update", "reopened", "Draft: MUL-123", "draft", false},
		{"merge", "merged", "WIP: MUL-123", "merged", true},
	} {
		t.Run(tc.action+"/"+tc.state+"/"+tc.title, func(t *testing.T) {
			var payload map[string]any
			if err := json.Unmarshal(gongfengFixture(t), &payload); err != nil {
				t.Fatal(err)
			}
			attrs := payload["object_attributes"].(map[string]any)
			attrs["action"], attrs["state"], attrs["title"] = tc.action, tc.state, tc.title
			attrs["updated_at"] = "2026-10-09T09:00:00+0800"
			body, _ := json.Marshal(payload)
			pr, err := p.ParsePullRequest(body)
			if err != nil {
				t.Fatal(err)
			}
			if pr.State != tc.want || pr.Terminal() != tc.terminal {
				t.Fatalf("unexpected transition: %+v", pr)
			}
			if tc.action != "open" && pr.AuthorLogin != "" {
				t.Fatal("event operator must not replace the MR author")
			}
			if tc.state == "merged" && pr.MergedAt != pr.UpdatedAt {
				t.Fatal("merge timestamp missing")
			}
			if tc.state == "closed" && pr.ClosedAt != pr.UpdatedAt {
				t.Fatal("close timestamp missing")
			}
		})
	}
}

func TestGongfengRejectsMalformedMR(t *testing.T) {
	p := gongfengAdapter(t)
	for _, field := range []string{"iid", "state", "url", "target", "created_at", "updated_at"} {
		t.Run(field, func(t *testing.T) {
			var payload map[string]any
			_ = json.Unmarshal(gongfengFixture(t), &payload)
			delete(payload["object_attributes"].(map[string]any), field)
			body, _ := json.Marshal(payload)
			if _, err := p.ParsePullRequest(body); err == nil {
				t.Fatalf("missing %s must be rejected", field)
			}
		})
	}
	if _, err := p.ParsePullRequest([]byte(`{"object_kind":"note"}`)); err == nil {
		t.Fatal("wrong payload kind accepted")
	}
	if _, err := p.ParseCIStatus([]byte(`{}`)); err == nil {
		t.Fatal("unsupported CI webhooks must be rejected")
	}
}

func TestGongfengRejectsInvalidExternalFields(t *testing.T) {
	p := gongfengAdapter(t)
	for _, tc := range []struct {
		field string
		value any
	}{
		{"state", "unknown"}, {"iid", -1},
		{"url", "javascript:alert(1)"}, {"url", "https://token@git.code.tencent.com/acme/widget"},
		{"target", map[string]any{"web_url": "https://git.code.tencent.com/widget"}},
		{"created_at", "invalid"}, {"updated_at", "2026-13-01T00:00:00Z"},
	} {
		t.Run(tc.field+fmt.Sprint(tc.value), func(t *testing.T) {
			var payload map[string]any
			_ = json.Unmarshal(gongfengFixture(t), &payload)
			payload["object_attributes"].(map[string]any)[tc.field] = tc.value
			body, _ := json.Marshal(payload)
			if _, err := p.ParsePullRequest(body); err == nil {
				t.Fatal("invalid field accepted")
			}
		})
	}
	if got, err := parseGongfengTime("2026-10-09T09:00:00.123456+0800"); err != nil || got != "2026-10-09T01:00:00.123456Z" {
		t.Fatalf("offset/precision normalization: %q, %v", got, err)
	}
}

func TestGongfengTokenDoesNotFollowRedirects(t *testing.T) {
	called := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))
	defer destination.Close()
	instance := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusFound)
	}))
	defer instance.Close()
	if _, err := gongfengAdapter(t).ValidateToken(context.Background(), instance.URL, "private-token"); err == nil {
		t.Fatal("redirect must fail token validation")
	}
	if called {
		t.Fatal("token validation followed an untrusted redirect")
	}
}

func TestGongfengValidateToken(t *testing.T) {
	p := gongfengAdapter(t)
	for _, tc := range []struct {
		name, response string
		status         int
		wantLogin      string
		unauthorized   bool
	}{
		{"valid", `{"username":"alice"}`, 200, "alice", false},
		{"unauthorized", `{}`, 401, "", true},
		{"forbidden", `{}`, 403, "", true},
		{"missing user", `{}`, 200, "", false},
		{"invalid JSON", `<html>`, 200, "", false},
		{"unavailable", `{}`, 503, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/v3/user" || r.Header.Get("PRIVATE-TOKEN") != "test-token" || r.URL.RawQuery != "" {
					t.Errorf("unexpected request: %s %s", r.Method, r.URL)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.response))
			}))
			defer srv.Close()
			account, err := p.ValidateToken(context.Background(), srv.URL, "test-token")
			if tc.wantLogin != "" {
				if err != nil || account.Login != tc.wantLogin {
					t.Fatalf("account=%+v err=%v", account, err)
				}
			} else if err == nil || errors.Is(err, ErrUnauthorized) != tc.unauthorized {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
