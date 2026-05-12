package github

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func TestDismissPriorCRChangesRequested_Idempotent(t *testing.T) {
	var dismissed atomic.Bool
	var graphqlCalls atomic.Int32
	rt := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch {
		case req.URL.Path == "/app/installations/123/access_tokens":
			return jsonResponse(http.StatusCreated, map[string]any{
				"token":      "installation-token",
				"expires_at": time.Now().Add(time.Hour).Format(time.RFC3339),
			}), nil
		case req.Method == http.MethodGet && req.URL.Path == "/repos/acme/repo/pulls/7/reviews":
			reviews := []map[string]any{
				{"id": 1, "node_id": "old-node", "state": "CHANGES_REQUESTED", "user": map[string]string{"login": "coderabbitai[bot]"}},
				{"id": 2, "node_id": "human-node", "state": "CHANGES_REQUESTED", "user": map[string]string{"login": "alice"}},
			}
			if !dismissed.Load() {
				reviews = append(reviews, map[string]any{"id": 3, "node_id": "latest-node", "state": "CHANGES_REQUESTED", "user": map[string]string{"login": "coderabbitai[bot]"}})
			} else {
				reviews = append(reviews, map[string]any{"id": 3, "node_id": "latest-node", "state": "DISMISSED", "user": map[string]string{"login": "coderabbitai[bot]"}})
			}
			return jsonResponse(http.StatusOK, reviews), nil
		case req.Method == http.MethodPost && req.URL.Path == "/graphql":
			body, _ := io.ReadAll(req.Body)
			if !strings.Contains(string(body), "latest-node") {
				t.Fatalf("dismiss mutation did not target latest CR review: %s", string(body))
			}
			graphqlCalls.Add(1)
			dismissed.Store(true)
			return jsonResponse(http.StatusOK, map[string]any{"data": map[string]any{"dismissPullRequestReview": map[string]any{"pullRequestReview": map[string]string{"id": "latest-node", "state": "DISMISSED"}}}}), nil
		default:
			t.Fatalf("unexpected request: %s %s", req.Method, req.URL.String())
			return nil, nil
		}
	})
	auth := &AppAuth{
		AppID:      1,
		httpClient: &http.Client{Transport: rt},
		tokens: map[int64]cachedToken{
			123: {value: "installation-token", expiresAt: time.Now().Add(time.Hour)},
		},
	}
	actions := &ReviewActions{Auth: auth}
	binding := db.WorkspaceRepoBinding{RepoFullName: "acme/repo", InstallationID: 123, CrBotUsername: "coderabbitai[bot]"}
	issue := db.Issue{PrNumber: pgtype.Int4{Int32: 7, Valid: true}}

	first, err := actions.DismissPriorCRChangesRequested(context.Background(), binding, issue)
	if err != nil {
		t.Fatalf("first dismiss: %v", err)
	}
	if !first.Dismissed || first.ReviewID != "latest-node" {
		t.Fatalf("first dismiss result = %+v, want latest-node dismissed", first)
	}
	second, err := actions.DismissPriorCRChangesRequested(context.Background(), binding, issue)
	if err != nil {
		t.Fatalf("second dismiss: %v", err)
	}
	if second.Dismissed {
		t.Fatalf("second dismiss should be idempotent no-op; got %+v", second)
	}
	if graphqlCalls.Load() != 1 {
		t.Fatalf("graphql dismiss calls = %d, want 1", graphqlCalls.Load())
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func jsonResponse(status int, v any) *http.Response {
	b, _ := json.Marshal(v)
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(string(b))),
	}
}
