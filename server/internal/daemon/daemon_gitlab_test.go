package daemon

import (
	"testing"
)

func TestInjectGitLabCreds(t *testing.T) {
	tests := []struct {
		name   string
		rawURL string
		token  string
		want   string
	}{
		{
			name:   "injects creds",
			rawURL: "https://gitlab.company.com/group/repo.git",
			token:  "tok123",
			want:   "https://oauth2:tok123@gitlab.company.com/group/repo.git",
		},
		{
			name:   "empty token is no-op",
			rawURL: "https://gitlab.company.com/group/repo.git",
			token:  "",
			want:   "https://gitlab.company.com/group/repo.git",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := injectGitLabCreds(tc.rawURL, tc.token)
			if got != tc.want {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
