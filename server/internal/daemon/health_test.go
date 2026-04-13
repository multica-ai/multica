package daemon

import "testing"

// TestIsLikelyLocalPath covers the heuristics the daemon uses to auto-detect
// whether a /repo/checkout request is for a local path or a remote URL.
// False positives here would route the request to CreateWorktreeFromLocal
// and fail with a confusing "not a git repo" error instead of a clean
// "repo not in cache". False negatives would silently treat a local path as
// a URL and try to clone it, which is the legacy (broken) behavior.
func TestIsLikelyLocalPath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"", false},
		{"https://github.com/org/repo", false},
		{"git@github.com:org/repo.git", false},
		{"ssh://git@host/repo.git", false},
		{"/Users/me/project", true},
		{"/var/tmp", true},
		{"~/code/multica", true},
		{"~", true},
		{"file:///tmp/repo", true},
		{"C:\\Users\\x\\repo", true},
		{"repo", false}, // bare name — treat as URL-ish, not path
	}
	for _, tt := range cases {
		if got := isLikelyLocalPath(tt.in); got != tt.want {
			t.Errorf("isLikelyLocalPath(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
