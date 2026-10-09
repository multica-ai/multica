package agent

import (
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestBuildOmpArgsOwnsSessionSelection(t *testing.T) {
	for _, resume := range []string{"", "/saved.jsonl"} {
		dir := ""
		want := []string{"-p", "--mode", "json", "--tools", "read"}
		if resume == "" {
			dir = "/fresh"
			want = append(want, "--session-dir", dir)
		} else {
			want = []string{"-p", "--mode", "json", "--session", resume, "--tools", "read"}
		}
		args := buildOmpArgs(resume, dir, ExecOptions{CustomArgs: []string{
			"--session-dir", "/other", "--session-dir=/other", "--no-session",
			"--session", "--continue", "-c", "--resume=other", "-r", "other",
			"--fork", "other", "--session-id", "other", "--tools", "read",
		}}, slog.Default())
		if !slices.Equal(args, want) {
			t.Fatalf("resume %q: args = %v, want %v", resume, args, want)
		}
	}
}

func TestFindOmpSessionFile(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"missing", nil, ""},
		{"empty", map[string]string{"empty.jsonl": ""}, ""},
		{"persisted", map[string]string{"chosen.jsonl": "{}\n", "metadata.json": "{}", "empty.jsonl": ""}, "chosen.jsonl"},
		{"ambiguous", map[string]string{"a.jsonl": "{}\n", "b.jsonl": "{}\n"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, data := range tc.files {
				if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			path, err := findOmpSessionFile(dir)
			if tc.want == "" {
				if err == nil || path != "" {
					t.Fatalf("expected no resumable path, got %q, %v", path, err)
				}
			} else if err != nil || path != filepath.Join(dir, tc.want) {
				t.Fatalf("got %q, %v", path, err)
			}
		})
	}
}
