package handler

import "testing"

func TestParseGitHubPullRequestURL(t *testing.T) {
	tests := []struct {
		name      string
		url       string
		wantOwner string
		wantRepo  string
		wantNum   int32
		wantErr   bool
	}{
		{
			name:      "basic",
			url:       "https://github.com/kylerjensen/multica/pull/123",
			wantOwner: "kylerjensen",
			wantRepo:  "multica",
			wantNum:   123,
		},
		{
			name:      "trailing slash",
			url:       "https://github.com/kylerjensen/multica/pull/123/",
			wantOwner: "kylerjensen",
			wantRepo:  "multica",
			wantNum:   123,
		},
		{
			name:      "trailing path segment",
			url:       "https://github.com/kylerjensen/multica/pull/123/files",
			wantOwner: "kylerjensen",
			wantRepo:  "multica",
			wantNum:   123,
		},
		{
			name:      "query and fragment",
			url:       "https://github.com/kylerjensen/multica/pull/123?diff=split#discussion",
			wantOwner: "kylerjensen",
			wantRepo:  "multica",
			wantNum:   123,
		},
		{
			name:      "repo with dot git suffix",
			url:       "https://github.com/kylerjensen/multica.git/pull/123",
			wantOwner: "kylerjensen",
			wantRepo:  "multica",
			wantNum:   123,
		},
		{
			name:      "leading/trailing whitespace",
			url:       "  https://github.com/kylerjensen/multica/pull/123  ",
			wantOwner: "kylerjensen",
			wantRepo:  "multica",
			wantNum:   123,
		},
		{name: "not a url", url: "not a url", wantErr: true},
		{name: "gitlab url", url: "https://gitlab.com/kylerjensen/multica/-/merge_requests/123", wantErr: true},
		{name: "issue url not pull", url: "https://github.com/kylerjensen/multica/issues/123", wantErr: true},
		{name: "http not https", url: "http://github.com/kylerjensen/multica/pull/123", wantErr: true},
		{name: "non-numeric pr number", url: "https://github.com/kylerjensen/multica/pull/abc", wantErr: true},
		{name: "empty", url: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			owner, repo, number, err := parseGitHubPullRequestURL(tc.url)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got owner=%q repo=%q number=%d", owner, repo, number)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if owner != tc.wantOwner || repo != tc.wantRepo || number != tc.wantNum {
				t.Fatalf("got (%q, %q, %d), want (%q, %q, %d)", owner, repo, number, tc.wantOwner, tc.wantRepo, tc.wantNum)
			}
		})
	}
}
