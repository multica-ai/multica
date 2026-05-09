package ship

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	gh "github.com/multica-ai/multica/server/pkg/github"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// fakeGithub implements GithubClient for the table-driven mapping tests
// below. It returns whatever Calls were preconfigured, in FIFO order.
type fakeGithub struct {
	responses []ghResponse
	idx       int
	requested []ghRequest
}

type ghResponse struct {
	prs []gh.PullRequest
	err error
}

type ghRequest struct {
	owner string
	repo  string
	state string
}

func (f *fakeGithub) ListPullRequests(_ context.Context, owner, repo string, opts gh.ListOptions) ([]gh.PullRequest, error) {
	f.requested = append(f.requested, ghRequest{owner, repo, opts.State})
	if f.idx >= len(f.responses) {
		return nil, nil
	}
	r := f.responses[f.idx]
	f.idx++
	return r.prs, r.err
}

func TestMapPRState(t *testing.T) {
	merged := time.Now()
	tests := []struct {
		name string
		pr   gh.PullRequest
		want db.PullRequestState
	}{
		{"open", gh.PullRequest{State: "open"}, db.PullRequestStateOpen},
		{"closed", gh.PullRequest{State: "closed"}, db.PullRequestStateClosed},
		{"merged-promoted", gh.PullRequest{State: "closed", MergedAt: &merged}, db.PullRequestStateMerged},
		// Defensive: GitHub occasionally returns merged_at on a state="open"
		// row during a race; promote to merged regardless of state.
		{"merged-while-open", gh.PullRequest{State: "open", MergedAt: &merged}, db.PullRequestStateMerged},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := mapPRState(tt.pr); got != tt.want {
				t.Errorf("mapPRState: got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestMapMergeable(t *testing.T) {
	tru, fls := true, false
	tests := []struct {
		name string
		in   *bool
		want string
	}{
		{"nil", nil, "UNKNOWN"},
		{"true", &tru, "MERGEABLE"},
		{"false", &fls, "CONFLICTING"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapMergeable(tt.in)
			if !got.Valid || got.String != tt.want {
				t.Errorf("mapMergeable(%v): got %+v, want %s", tt.in, got, tt.want)
			}
		})
	}
}

func TestRepoURLFromResource(t *testing.T) {
	good := []byte(`{"url":"https://github.com/owner/repo"}`)
	url, err := repoURLFromResource(good)
	if err != nil || url != "https://github.com/owner/repo" {
		t.Errorf("good ref: got %q err=%v", url, err)
	}

	missing := []byte(`{}`)
	if _, err := repoURLFromResource(missing); err == nil {
		t.Errorf("missing url: expected error")
	}

	bad := []byte(`{"url`)
	if _, err := repoURLFromResource(bad); err == nil {
		t.Errorf("bad json: expected error")
	}
}

// TestSyncProject_NoGithubClient verifies the service refuses to sync
// when no GitHub client is wired (e.g. workspace has no token configured).
// This is the only way to cover the SyncProject error path without a DB.
func TestSyncProject_NoGithubClient(t *testing.T) {
	s := &Service{}
	if _, err := s.SyncProject(context.Background(), pgtype.UUID{}, pgtype.UUID{}); err == nil {
		t.Errorf("expected error for unconfigured github client")
	}
}

// TestFakeGithub_Wired keeps the fake usable for richer integration tests
// later; for now it just verifies the construction site so unused symbols
// don't accumulate. (mapPRState already covers the data-side path.)
func TestFakeGithub_Wired(t *testing.T) {
	f := &fakeGithub{responses: []ghResponse{{prs: []gh.PullRequest{{Number: 1}}}}}
	prs, err := f.ListPullRequests(context.Background(), "o", "r", gh.ListOptions{State: "open"})
	if err != nil || len(prs) != 1 || prs[0].Number != 1 {
		t.Fatalf("fake list: %v %+v", err, prs)
	}
	if len(f.requested) != 1 || f.requested[0].state != "open" {
		t.Fatalf("expected one request recorded, got %+v", f.requested)
	}
	// time import is used by the mapPRState test.
	_ = time.Now()
}
