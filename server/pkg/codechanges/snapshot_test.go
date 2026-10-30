package codechanges

import (
	"bytes"
	"compress/gzip"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func fixtureGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.invalid", "GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.invalid")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
func TestCaptureFixedCommitsAndSpecialPaths(t *testing.T) {
	root := t.TempDir()
	fixtureGit(t, root, "init")
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("removed.txt", "remove\n")
	write("file.txt", "before\n")
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "base")
	base, err := Head(context.Background(), root, "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(root, "removed.txt")); err != nil {
		t.Fatal(err)
	}
	write("file.txt", "after\n")
	write("tab\tline\n.txt", "special\n")
	write("binary.dat", "\x00\xff\x00")
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "change")
	head := fixtureGit(t, root, "rev-parse", "HEAD")
	write("file.txt", "uncommitted\n")
	s := Capture(context.Background(), root, base, head, true)
	if s.Reason != "" || !s.WorkingTreeDirty || len(s.Files) != 4 {
		t.Fatalf("snapshot=%+v", s)
	}
	files := map[string]File{}
	for _, f := range s.Files {
		files[f.Path] = f
	}
	if files["removed.txt"].Status != "D" || files["file.txt"].Status != "M" || !files["binary.dat"].Binary || files["tab\tline\n.txt"].Status != "A" {
		t.Fatalf("files=%+v", files)
	}
	if !strings.Contains(files["file.txt"].Diff, "+after") || strings.Contains(files["file.txt"].Diff, "uncommitted") {
		t.Fatal("diff did not stay on fixed commits")
	}
	encoded, err := Encode(s)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := Decode(encoded)
	if err != nil || decoded.HeadRef != head {
		t.Fatalf("roundtrip %v", err)
	}
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "later")
	again := Capture(context.Background(), root, base, head, false)
	if again.Files[0].Diff != s.Files[0].Diff {
		t.Fatal("later commit changed saved range")
	}
	empty := Capture(context.Background(), root, head, head, false)
	if len(empty.Files) != 0 || empty.Reason != "" {
		t.Fatalf("empty=%+v", empty)
	}
}
func TestCaptureLimits(t *testing.T) {
	root := t.TempDir()
	fixtureGit(t, root, "init")
	fixtureGit(t, root, "commit", "--allow-empty", "-m", "base")
	base := fixtureGit(t, root, "rev-parse", "HEAD")
	if err := os.WriteFile(filepath.Join(root, "large.txt"), []byte(strings.Repeat("line\n", MaxFileBytes)), 0600); err != nil {
		t.Fatal(err)
	}
	fixtureGit(t, root, "add", ".")
	fixtureGit(t, root, "commit", "-m", "large")
	head := fixtureGit(t, root, "rev-parse", "HEAD")
	s := Capture(context.Background(), root, base, head, false)
	if s.Reason != "" || !s.Truncated || len(s.Files) != 1 || !s.Files[0].Truncated || len(s.Files[0].Diff) > MaxFileBytes {
		t.Fatalf("bounded snapshot=%+v", s)
	}
}
func TestSnapshotRejectsOversizeAndInvalidData(t *testing.T) {
	s := &Snapshot{Version: 1, BaseRef: strings.Repeat("a", 40), HeadRef: strings.Repeat("b", 40), Files: []File{{Path: "x", Status: "M", Diff: strings.Repeat("x", MaxFileBytes+1)}}}
	if _, err := Encode(s); err == nil {
		t.Fatal("oversize accepted")
	}
	var bomb bytes.Buffer
	w := gzip.NewWriter(&bomb)
	_, _ = w.Write(bytes.Repeat([]byte(" "), MaxJSONBytes+1))
	_ = w.Close()
	if _, err := Decode(bomb.Bytes()); err == nil {
		t.Fatal("decompression bomb accepted")
	}
	if Enabled(nil) || Enabled([]byte(`{"code_changes_enabled":"true"}`)) || !Enabled([]byte(`{"code_changes_enabled":true}`)) {
		t.Fatal("settings must be explicit boolean true")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if s := Capture(ctx, t.TempDir(), strings.Repeat("a", 40), strings.Repeat("b", 40), false); s.Reason != "capture_failed" {
		t.Fatal("cancelled capture was complete")
	}
}
