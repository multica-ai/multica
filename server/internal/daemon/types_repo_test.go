package daemon

import "testing"

// TestRepoData_TypeHelpers pins the behavior of the IsLocal/IsGitHub helpers
// used all over the daemon to branch on repo kind. Empty type must map to
// github for v1 backwards compatibility; everything else is clear-cut.
func TestRepoData_TypeHelpers(t *testing.T) {
	t.Parallel()
	tests := []struct {
		in         RepoData
		wantLocal  bool
		wantGithub bool
		wantSource string
	}{
		{RepoData{Type: "github", URL: "u"}, false, true, "u"},
		{RepoData{Type: "", URL: "u"}, false, true, "u"},      // v1 row
		{RepoData{Type: "local", LocalPath: "/p"}, true, false, "/p"},
	}
	for i, tt := range tests {
		if got := tt.in.IsLocal(); got != tt.wantLocal {
			t.Errorf("case %d IsLocal: got %v", i, got)
		}
		if got := tt.in.IsGitHub(); got != tt.wantGithub {
			t.Errorf("case %d IsGitHub: got %v", i, got)
		}
		if got := tt.in.Source(); got != tt.wantSource {
			t.Errorf("case %d Source: got %q want %q", i, got, tt.wantSource)
		}
	}
}
