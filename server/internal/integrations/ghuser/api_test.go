package ghuser

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func apiService(t *testing.T, handler http.HandlerFunc) *Service {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	svc, _ := newTestService(t, nil, nil, nil)
	svc.SetEndpoints(srv.URL, srv.URL)
	return svc
}

func TestRESTSendsUserTokenAndDecodes(t *testing.T) {
	svc := apiService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ghu_secret" || r.Header.Get("X-GitHub-Api-Version") == "" {
			t.Errorf("headers = %v", r.Header)
		}
		if r.Method != http.MethodPatch || r.URL.Path != "/repos/o/r/pulls/1" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"state":"closed"}`))
	})
	var out struct {
		State string `json:"state"`
	}
	if err := svc.REST(context.Background(), "ghu_secret", http.MethodPatch, "/repos/o/r/pulls/1", map[string]string{"state": "closed"}, &out); err != nil || out.State != "closed" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestRESTErrors(t *testing.T) {
	cases := []struct {
		name        string
		status      int
		headers     map[string]string
		body        string
		wantStatus  int
		wantMessage string
		rateLimited bool
		retryAfter  time.Duration
	}{
		{"merge blocked", 405, nil, `{"message":"Pull Request is not mergeable"}`, 405, "Pull Request is not mergeable", false, 0},
		{"validation detail", 422, nil, `{"message":"Validation Failed","errors":[{"message":"Reviews may only be requested from collaborators."}]}`, 422, "Validation Failed: Reviews may only be requested from collaborators.", false, 0},
		{"primary rate limit", 403, map[string]string{"X-RateLimit-Remaining": "0"}, `{"message":"API rate limit exceeded"}`, 403, "API rate limit exceeded", true, time.Minute},
		{"secondary rate limit", 429, map[string]string{"Retry-After": "30"}, `{"message":"You have exceeded a secondary rate limit"}`, 429, "You have exceeded a secondary rate limit", true, 30 * time.Second},
		{"integration permission", 403, nil, `{"message":"Resource not accessible by integration"}`, 403, "Resource not accessible by integration", false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc := apiService(t, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tc.headers {
					w.Header().Set(k, v)
				}
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			})
			err := svc.REST(context.Background(), "ghu_secret", http.MethodGet, "/x", nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v", err)
			}
			if apiErr.Status != tc.wantStatus || apiErr.Message != tc.wantMessage || apiErr.RateLimited != tc.rateLimited {
				t.Fatalf("apiErr = %+v", apiErr)
			}
			if tc.rateLimited && apiErr.RetryAfter != tc.retryAfter {
				t.Fatalf("retry after = %v, want %v", apiErr.RetryAfter, tc.retryAfter)
			}
			if strings.Contains(err.Error(), "ghu_secret") {
				t.Fatal("error leaked the token")
			}
		})
	}
}

func TestGraphQLData(t *testing.T) {
	svc := apiService(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/graphql" || r.Method != http.MethodPost {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if !strings.Contains(r.Header.Get("Accept"), "merge-info-preview") {
			t.Errorf("accept = %q", r.Header.Get("Accept"))
		}
		_, _ = w.Write([]byte(`{"data":{"viewer":{"login":"octocat"}}}`))
	})
	var out struct {
		Viewer struct {
			Login string `json:"login"`
		} `json:"viewer"`
	}
	if err := svc.GraphQL(context.Background(), "ghu_secret", "query { viewer { login } }", nil, &out); err != nil || out.Viewer.Login != "octocat" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

func TestGraphQLErrors(t *testing.T) {
	cases := map[string]struct {
		body  string
		check func(*APIError) bool
	}{
		"forbidden": {
			`{"data":null,"errors":[{"type":"FORBIDDEN","message":"Resource not accessible by integration"}]}`,
			func(e *APIError) bool {
				return e.GraphQLType == "FORBIDDEN" && !e.RateLimited && e.Message == "Resource not accessible by integration"
			},
		},
		"rate limited": {
			`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`,
			func(e *APIError) bool { return e.GraphQLType == "RATE_LIMITED" && e.RateLimited },
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc := apiService(t, func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) })
			err := svc.GraphQL(context.Background(), "ghu_secret", "query { viewer { login } }", nil, nil)
			var apiErr *APIError
			if !errors.As(err, &apiErr) || !tc.check(apiErr) {
				t.Fatalf("err = %v", err)
			}
		})
	}
}
