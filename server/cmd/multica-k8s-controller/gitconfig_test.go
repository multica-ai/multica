package main

import (
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/daemon"
)

func TestGitconfigForTask_GitHubHTTPS(t *testing.T) {
	got := gitconfigForTask(
		"ws-123",
		"/repos",
		[]daemon.RepoData{{URL: "https://github.com/chrissnell/graywolf.git"}},
	)
	wantSubstrings := []string{
		`[url "file:///repos/ws-123/github.com+chrissnell+graywolf.git"]`,
		"insteadOf = https://github.com/chrissnell/graywolf",
		"insteadOf = https://github.com/chrissnell/graywolf.git",
		"insteadOf = git@github.com:chrissnell/graywolf",
		"insteadOf = git@github.com:chrissnell/graywolf.git",
		// pushInsteadOf block: pushes route to SSH origin, not to the
		// read-only cache PVC.
		`[url "git@github.com:chrissnell/graywolf.git"]`,
		"pushInsteadOf = https://github.com/chrissnell/graywolf",
		"pushInsteadOf = https://github.com/chrissnell/graywolf.git",
		"pushInsteadOf = git@github.com:chrissnell/graywolf",
	}
	for _, want := range wantSubstrings {
		if !strings.Contains(got, want) {
			t.Errorf("missing: %q\ngot:\n%s", want, got)
		}
	}
}

func TestGitconfigForTask_PushDoesNotRouteToCache(t *testing.T) {
	got := gitconfigForTask(
		"ws",
		"/repos",
		[]daemon.RepoData{{URL: "https://github.com/org/repo.git"}},
	)
	// There must be a pushInsteadOf entry — without it, push would route
	// through the file:// insteadOf rewrite to the RO PVC and fail.
	if !strings.Contains(got, "pushInsteadOf =") {
		t.Errorf("missing pushInsteadOf entries:\n%s", got)
	}
	// The push target MUST be the git@host:owner/repo.git form (SSH),
	// not the file:// cache.
	if !strings.Contains(got, `[url "git@github.com:org/repo.git"]`) {
		t.Errorf("missing SSH push target block:\n%s", got)
	}
}

func TestGitconfigForTask_SCPStyle(t *testing.T) {
	got := gitconfigForTask(
		"ws-XYZ",
		"/repos",
		[]daemon.RepoData{{URL: "git@github.com:org/repo.git"}},
	)
	for _, want := range []string{
		`[url "file:///repos/ws-XYZ/github.com+org+repo.git"]`,
		"insteadOf = https://github.com/org/repo",
		"insteadOf = git@github.com:org/repo.git",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing: %q\ngot:\n%s", want, got)
		}
	}
}

func TestGitconfigForTask_MultipleRepos(t *testing.T) {
	got := gitconfigForTask(
		"ws-A",
		"/repos",
		[]daemon.RepoData{
			{URL: "https://github.com/a/one.git"},
			{URL: "https://github.com/b/two.git"},
		},
	)
	if !strings.Contains(got, "/ws-A/github.com+a+one.git") {
		t.Errorf("missing first repo block:\n%s", got)
	}
	if !strings.Contains(got, "/ws-A/github.com+b+two.git") {
		t.Errorf("missing second repo block:\n%s", got)
	}
}

func TestGitconfigForTask_NoRepos(t *testing.T) {
	got := gitconfigForTask("ws", "/repos", nil)
	if got != "" {
		t.Errorf("expected empty, got: %q", got)
	}
}

func TestGitconfigForTask_SkipsMalformedURLs(t *testing.T) {
	got := gitconfigForTask("ws", "/repos", []daemon.RepoData{
		{URL: ""},
		{URL: "not-a-url"},
		{URL: "https://github.com/ok/repo.git"},
	})
	if !strings.Contains(got, "ok+repo.git") {
		t.Errorf("missing valid repo:\n%s", got)
	}
	// One fetch block + one push block per valid repo.
	if strings.Contains(got, "insteadOf =\n") || strings.Count(got, "[url ") != 2 {
		t.Errorf("expected exactly two url blocks (fetch + push) for one valid repo, got:\n%s", got)
	}
}

func TestGitconfigForTask_DefaultMountPath(t *testing.T) {
	got := gitconfigForTask("ws", "", []daemon.RepoData{{URL: "https://github.com/a/b.git"}})
	if !strings.Contains(got, "file:///repos/ws/") {
		t.Errorf("expected default /repos mount, got:\n%s", got)
	}
}

func TestGitconfigForTask_TrimsTrailingSlashMountPath(t *testing.T) {
	got := gitconfigForTask("ws", "/repos/", []daemon.RepoData{{URL: "https://github.com/a/b.git"}})
	if strings.Contains(got, "file:///repos//ws/") {
		t.Errorf("double slash in path:\n%s", got)
	}
}

func TestSplitGitURL_Cases(t *testing.T) {
	cases := []struct {
		in            string
		wantHost      string
		wantOwnerRepo string
	}{
		{"https://github.com/owner/repo.git", "github.com", "owner/repo"},
		{"https://github.com/owner/repo", "github.com", "owner/repo"},
		{"git@github.com:owner/repo.git", "github.com", "owner/repo"},
		{"git@github.com:owner/repo", "github.com", "owner/repo"},
		{"ssh://git@gitlab.example.com:22/g/s/r.git", "gitlab.example.com:22", "g/s/r"},
		{"http://example.com/a/b", "example.com", "a/b"},
		{"not-a-url", "", ""},
		{"", "", ""},
	}
	for _, tc := range cases {
		gotH, gotR := splitGitURL(tc.in)
		if gotH != tc.wantHost || gotR != tc.wantOwnerRepo {
			t.Errorf("splitGitURL(%q) = (%q, %q), want (%q, %q)", tc.in, gotH, gotR, tc.wantHost, tc.wantOwnerRepo)
		}
	}
}
