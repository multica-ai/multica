package repocache

import (
	"encoding/json"
	"testing"
)

func TestParseGithubRepoRef(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		ref       string
		wantOwner string
		wantName  string
		wantErr   bool
	}{
		{"https with .git", `{"url":"https://github.com/rabbeet/multica.git"}`, "rabbeet", "multica", false},
		{"https no .git", `{"url":"https://github.com/rabbeet/multica"}`, "rabbeet", "multica", false},
		{"https trailing slash", `{"url":"https://github.com/rabbeet/multica/"}`, "rabbeet", "multica", false},
		{"ssh short form", `{"url":"git@github.com:rabbeet/multica.git"}`, "rabbeet", "multica", false},
		{"ssh no .git", `{"url":"git@github.com:rabbeet/multica"}`, "rabbeet", "multica", false},
		{"ssh long form", `{"url":"ssh://git@github.com/rabbeet/multica.git"}`, "rabbeet", "multica", false},
		{"case preserved", `{"url":"https://github.com/Rabbeet/Pulse"}`, "Rabbeet", "Pulse", false},
		{"non-github host", `{"url":"https://gitlab.com/rabbeet/multica.git"}`, "", "", true},
		{"empty url", `{"url":""}`, "", "", true},
		{"missing url field", `{"label":"hi"}`, "", "", true},
		{"three-segment path", `{"url":"https://github.com/org/sub/repo"}`, "", "", true},
		{"one-segment path", `{"url":"https://github.com/org"}`, "", "", true},
		{"malformed json", `not json`, "", "", true},
		{"empty json", ``, "", "", true},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseGithubRepoRef(json.RawMessage(c.ref))
			if c.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.Owner != c.wantOwner || got.Name != c.wantName {
				t.Fatalf("got owner=%q name=%q, want %q/%q", got.Owner, got.Name, c.wantOwner, c.wantName)
			}
		})
	}
}

func TestGithubRepoRef_String(t *testing.T) {
	t.Parallel()
	r := GithubRepoRef{Owner: "rabbeet", Name: "multica"}
	if got := r.String(); got != "rabbeet/multica" {
		t.Fatalf("got %q, want rabbeet/multica", got)
	}
}

func TestResolveBareFromGithubRef(t *testing.T) {
	t.Parallel()
	m := map[string]string{
		"rabbeet/Pulse":         "/srv/pulse-bare.git",
		"rabbeet/multica":       "/srv/multica-bare.git",
		"rabbeet/agent-context": "/srv/agent-context-bare.git",
	}

	t.Run("exact match", func(t *testing.T) {
		t.Parallel()
		p, ok := ResolveBareFromGithubRef(m, GithubRepoRef{Owner: "rabbeet", Name: "Pulse"})
		if !ok || p != "/srv/pulse-bare.git" {
			t.Fatalf("got %q ok=%v, want /srv/pulse-bare.git true", p, ok)
		}
	})

	t.Run("case-insensitive owner", func(t *testing.T) {
		t.Parallel()
		p, ok := ResolveBareFromGithubRef(m, GithubRepoRef{Owner: "Rabbeet", Name: "pulse"})
		if !ok || p != "/srv/pulse-bare.git" {
			t.Fatalf("got %q ok=%v, want /srv/pulse-bare.git true", p, ok)
		}
	})

	t.Run("miss returns empty", func(t *testing.T) {
		t.Parallel()
		p, ok := ResolveBareFromGithubRef(m, GithubRepoRef{Owner: "rabbeet", Name: "unknown"})
		if ok || p != "" {
			t.Fatalf("got %q ok=%v, want empty miss", p, ok)
		}
	})

	t.Run("empty map", func(t *testing.T) {
		t.Parallel()
		p, ok := ResolveBareFromGithubRef(nil, GithubRepoRef{Owner: "any", Name: "thing"})
		if ok || p != "" {
			t.Fatalf("got %q ok=%v, want empty miss on nil map", p, ok)
		}
	})
}

func TestPerTaskWorktreePath(t *testing.T) {
	t.Parallel()
	cases := []struct {
		root, agent, task, want string
	}{
		{"/srv/agent-worktrees", "agent-1", "a1b2c3d4-5e6f-7890-abcd-ef1234567890", "/srv/agent-worktrees/agent-1-a1b2c3d4"},
		{"/srv/agent-worktrees", "GPT_Boy", "1234abcd-...", "/srv/agent-worktrees/gpt-boy-1234abcd"},
	}
	for _, c := range cases {
		c := c
		t.Run(c.agent, func(t *testing.T) {
			t.Parallel()
			got := PerTaskWorktreePath(c.root, c.agent, c.task)
			if got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
		})
	}
}
