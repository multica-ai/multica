package execenv

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteContextFilesMaterializesReadOnlyFallbackTranscript(t *testing.T) {
	dir := t.TempDir()
	ctx := TaskContextForEnv{
		IssueID: "issue-1",
		FallbackTranscript: &FallbackTranscriptForEnv{
			SourceTaskID: "parent-task-1",
			Messages: []json.RawMessage{
				json.RawMessage(`{"seq":1,"type":"text","content":"investigated auth"}`),
				json.RawMessage(`{"seq":2,"type":"tool_use","tool":"shell","input":{"cmd":"go test ./..."}}`),
			},
		},
	}

	if err := writeContextFiles(dir, "codex", ctx, nil); err != nil {
		t.Fatalf("writeContextFiles: %v", err)
	}
	relPath := fallbackTranscriptRelPath(ctx.FallbackTranscript)
	path := filepath.Join(dir, relPath)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read transcript: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("transcript lines = %d, want 2: %s", len(lines), data)
	}
	for _, line := range lines {
		if !json.Valid([]byte(line)) {
			t.Fatalf("invalid JSONL line: %s", line)
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat transcript: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o444 {
		t.Fatalf("transcript mode = %o, want 444", got)
	}

	brief := buildMetaSkillContent("codex", ctx)
	for _, want := range []string{relPath, "source task `parent-task-1`", "contents are not injected into your prompt"} {
		if !strings.Contains(brief, want) {
			t.Errorf("runtime brief missing %q:\n%s", want, brief)
		}
	}
	if strings.Contains(brief, "investigated auth") || strings.Contains(brief, "go test ./...") {
		t.Fatalf("runtime brief injected transcript contents:\n%s", brief)
	}
}

func TestWriteContextFilesRejectsInvalidFallbackTranscriptMessage(t *testing.T) {
	err := writeContextFiles(t.TempDir(), "codex", TaskContextForEnv{
		FallbackTranscript: &FallbackTranscriptForEnv{
			Messages: []json.RawMessage{json.RawMessage(`not-json`)},
		},
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "invalid fallback transcript message") {
		t.Fatalf("writeContextFiles error = %v, want invalid transcript error", err)
	}
}

func TestWriteFallbackTranscriptDoesNotChmodPreExistingIdenticalFile(t *testing.T) {
	dir := t.TempDir()
	transcript := &FallbackTranscriptForEnv{
		SourceTaskID: "parent-task-1",
		Messages:     []json.RawMessage{json.RawMessage(`{"seq":1,"type":"text","content":"same"}`)},
	}
	path := filepath.Join(dir, fallbackTranscriptRelPath(transcript))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	data := append([]byte(transcript.Messages[0]), '\n')
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeFallbackTranscript(dir, transcript, &sidecarManifest{}); err != nil {
		t.Fatalf("writeFallbackTranscript: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("pre-existing file mode changed to %o, want 644", got)
	}
}
