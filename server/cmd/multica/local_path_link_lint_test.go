package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withAgentContext makes inAgentExecutionContext() report true for one test.
func withAgentContext(t *testing.T) {
	t.Helper()
	t.Setenv("MULTICA_TASK_ID", "task-1")
}

// withWorkdir chdirs into a fresh temp dir so workdir-prefix classification has
// a real, isolated root to resolve against.
func withWorkdir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	// EvalSymlinks: macOS temp dirs live under a symlinked /var, and the
	// classifier canonicalizes both sides. Return the resolved path so tests
	// build targets that match what it compares against.
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve temp dir: %v", err)
	}
	return resolved
}

func targets(findings []localPathLinkFinding) []string {
	out := make([]string, 0, len(findings))
	for _, f := range findings {
		out = append(out, f.Target)
	}
	return out
}

// The three high-confidence signals, each proven by a positive case.
func TestFindLocalPathLinksHighConfidenceSignals(t *testing.T) {
	workdir := withWorkdir(t)

	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	outside := filepath.Join(t.TempDir(), "outside.png")
	if err := os.WriteFile(outside, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	resolvedOutside, err := filepath.EvalSymlinks(outside)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}

	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "file:// URL in a link",
			body: "see [the chart](file:///tmp/chart.png)",
			want: []string{"file:///tmp/chart.png"},
		},
		{
			name: "file:// URL in an autolink renders clickable too",
			body: "see <file:///tmp/chart.png>",
			want: []string{"file:///tmp/chart.png"},
		},
		{
			name: "absolute path inside the task workdir",
			body: "![screenshot](" + shot + ")",
			want: []string{shot},
		},
		{
			name: "absolute path to a file that exists on this machine",
			body: "[log](" + resolvedOutside + ")",
			want: []string{resolvedOutside},
		},
		{
			name: "the reported repro: image link to a workdir file",
			body: "Done. Screenshot: [点我](" + shot + ")",
			want: []string{shot},
		},
		{
			name: "one path linked repeatedly reports once",
			body: "[a](" + shot + ") and [b](" + shot + ")",
			want: []string{shot},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := targets(findLocalPathLinks(tc.body))
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("got[%d]=%q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// Everything the lint must let through. Each of these is a legitimate
// deliverable that a full-text scan or a looser rule would break — the reason
// the lint parses markdown and limits itself to three positive signals.
func TestFindLocalPathLinksAllowsLegitimateContent(t *testing.T) {
	workdir := withWorkdir(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	cases := []struct {
		name string
		body string
	}{
		{
			// The canonical in-app link. Absolute-looking, but a valid
			// origin-relative URI reference (RFC 3986 §4.2) — blocking it would
			// break every issue link an agent writes.
			name: "origin-relative in-app link",
			body: "see [MUL-1](/acme/issues/MUL-1) and [inbox](/issues/123)",
		},
		{
			name: "external http link",
			body: "the PR is at [#42](https://github.com/multica-ai/multica/pull/42)",
		},
		{
			name: "mention link",
			body: "[@Nevi](mention://member/abc-123)",
		},
		{
			// The whole point of parsing an AST: an agent explaining THIS bug
			// quotes local paths constantly. None of these are link nodes.
			name: "path inside a code span",
			body: "the repro is `[x](" + shot + ")` — note the local path",
		},
		{
			name: "path inside a fenced code block",
			body: "```md\n[screenshot](" + shot + ")\n![x](file:///tmp/x.png)\n```",
		},
		{
			name: "path inside an indented code block",
			body: "    [screenshot](" + shot + ")\n",
		},
		{
			name: "path as plain prose is not a link",
			body: "I wrote the screenshot to " + shot + " on my machine.",
		},
		{
			name: "relative link is left alone",
			body: "[readme](docs/readme.md)",
		},
		{
			// Absolute, does not exist, not in the workdir → indistinguishable
			// from an in-app route. Deliberately allowed; the prompt contract
			// covers this residue, not the lint.
			name: "absolute path that does not exist",
			body: "[x](/Users/someone-else/never-existed.png)",
		},
		{
			name: "link text mentioning a path is not a destination",
			body: "[" + shot + "](/acme/issues/1)",
		},
		{
			name: "empty body",
			body: "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := targets(findLocalPathLinks(tc.body)); len(got) != 0 {
				t.Errorf("expected no findings, got %v\n--- body ---\n%s", got, tc.body)
			}
		})
	}
}

// A directory is not a deliverable file; stat-ing one must not trip the lint.
func TestFindLocalPathLinksIgnoresDirectories(t *testing.T) {
	dir := t.TempDir()
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	t.Chdir(t.TempDir())
	if got := targets(findLocalPathLinks("[dir](" + resolved + ")")); len(got) != 0 {
		t.Errorf("a directory target should not be reported, got %v", got)
	}
}

func TestGuardLocalPathLinksOnlyFiresInAgentContext(t *testing.T) {
	workdir := withWorkdir(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	body := "[screenshot](" + shot + ")"

	t.Run("human PAT context is never linted", func(t *testing.T) {
		// No MULTICA_AGENT_ID / MULTICA_TASK_ID: a person running the CLI. Their
		// links are their own business — they may well have a path the reader
		// really can open.
		t.Setenv("MULTICA_AGENT_ID", "")
		t.Setenv("MULTICA_TASK_ID", "")
		if err := guardLocalPathLinks(body, "comment body", "hint"); err != nil {
			t.Errorf("expected no error outside agent context, got: %v", err)
		}
	})

	t.Run("agent task context hard-fails", func(t *testing.T) {
		withAgentContext(t)
		err := guardLocalPathLinks(body, "comment body", "hint")
		if err == nil {
			t.Fatal("expected a hard failure inside agent context")
		}
		if !strings.Contains(err.Error(), shot) {
			t.Errorf("error should name the offending target, got: %v", err)
		}
	})

	t.Run("agent context with a clean body passes", func(t *testing.T) {
		withAgentContext(t)
		if err := guardLocalPathLinks("all good — see [MUL-1](/acme/issues/MUL-1)", "comment body", "hint"); err != nil {
			t.Errorf("expected no error for a clean body, got: %v", err)
		}
	})
}

// The per-command fix hint. `issue update` is the trap: it has no --attachment
// flag, so a shared "pass --attachment" message would point the agent at an
// argument the command rejects and turn one failure into two.
func TestGuardLocalPathLinksHintIsPerCommand(t *testing.T) {
	workdir := withWorkdir(t)
	shot := filepath.Join(workdir, "shot.png")
	if err := os.WriteFile(shot, []byte("x"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	withAgentContext(t)

	err := guardLocalPathLinks(
		"[screenshot]("+shot+")",
		"issue description",
		"`multica issue update` cannot carry files — deliver the file with `multica issue comment add <issue-id> --attachment <path>` instead, and drop the link.",
	)
	if err == nil {
		t.Fatal("expected a hard failure")
	}
	msg := err.Error()
	if !strings.Contains(msg, "`multica issue update` cannot carry files") {
		t.Errorf("update hint missing, got: %v", msg)
	}
	if !strings.Contains(msg, "multica issue comment add <issue-id> --attachment <path>") {
		t.Errorf("update hint must redirect to comment add, got: %v", msg)
	}
	if strings.Contains(msg, "multica issue update --attachment") {
		t.Errorf("update hint must never name a flag `issue update` does not have, got: %v", msg)
	}
}
