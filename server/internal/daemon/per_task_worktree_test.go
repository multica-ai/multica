package daemon

import (
	"errors"
	"fmt"
	"testing"
)

func TestErrorClassOf(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want string
	}{
		{"nil → unknown", nil, "unknown"},
		{"plain → unknown", errors.New("oops"), "unknown"},
		{"target_repo_not_allowed", fmt.Errorf("wrapped: %w", ErrTargetRepoNotAllowed), "target_repo_not_allowed"},
		{"bare_missing", fmt.Errorf("nope: %w", ErrBareMissing), "bare_missing"},
		{"disk_budget", fmt.Errorf("over: %w", ErrDiskBudgetExceeded), "disk_budget"},
		{"fetch_timeout", fmt.Errorf("timed out: %w", ErrFetchTimeout), "fetch_timeout"},
		{"fetch_auth", fmt.Errorf("creds: %w", ErrFetchAuth), "fetch_auth"},
		{"branch_collision", fmt.Errorf("dup: %w", ErrBranchCollision), "branch_collision"},
		{"path_collision_unexpected", fmt.Errorf("dup path: %w", ErrPathCollisionUnexpected), "path_collision_unexpected"},
		{"worktree_missing", fmt.Errorf("gone: %w", ErrWorktreeMissing), "worktree_missing"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			if got := errorClassOf(c.err); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestClassifyFetchError(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		input     error
		wantClass string // "timeout" | "auth" | "nil"
	}{
		{"nil passthrough", nil, "nil"},
		{"already-classified timeout", ErrFetchTimeout, "timeout"},
		{"already-classified auth", ErrFetchAuth, "auth"},
		{"auth via message lowercase", errors.New("fatal: authentication failed for 'https://github.com/org/repo'"), "auth"},
		{"auth via 401 code", errors.New("remote returned 401 Unauthorized"), "auth"},
		{"auth via 403 code", errors.New("HTTP 403 from origin"), "auth"},
		{"auth via could_not_read_username", errors.New("could not read username for 'https://...'"), "auth"},
		{"network timeout falls through to timeout", errors.New("dial tcp: i/o timeout"), "timeout"},
		{"generic git error falls through to timeout", errors.New("fatal: refusing to fetch into current branch"), "timeout"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got := classifyFetchError(c.input)
			switch c.wantClass {
			case "nil":
				if got != nil {
					t.Fatalf("expected nil, got %v", got)
				}
			case "timeout":
				if !errors.Is(got, ErrFetchTimeout) {
					t.Fatalf("expected ErrFetchTimeout, got %v", got)
				}
			case "auth":
				if !errors.Is(got, ErrFetchAuth) {
					t.Fatalf("expected ErrFetchAuth, got %v", got)
				}
			}
		})
	}
}
