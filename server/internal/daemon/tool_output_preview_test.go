package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func TestToolOutputPreview(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, input, want string
		wantTruncated     bool
	}{
		{"empty", "", "", false},
		{"short unicode", "执行完成 😀\n", "执行完成 😀\n", false},
		{"under budget", strings.Repeat("a", 8191), strings.Repeat("a", 8191), false},
		{"exact budget", strings.Repeat("a", 8192), strings.Repeat("a", 8192), false},
		{"over budget", strings.Repeat("a", 8193), strings.Repeat("a", 8192), true},
		{"chinese", strings.Repeat("中", 5000), strings.Repeat("中", 2730), true},
		{"emoji", strings.Repeat("😀", 3000), strings.Repeat("😀", 2048), true},
		// Server-side redaction may expand this input. That is not source truncation.
		{"redaction growth", strings.Repeat("TOKEN=x ", 1024), strings.Repeat("TOKEN=x ", 1024), false},
		{"nul", "before\x00after", "beforeafter", false},
		{"invalid utf8", "a\xffb", "a\uFFFDb", false},
		// Sanitizing this input pushes it past the budget, so the record really
		// does lose bytes — the flag follows the stored preview, not the raw input.
		{"normalization growth", strings.Repeat("a", 8191) + "\xff", strings.Repeat("a", 8191), true},
		{"normalization shrink", strings.Repeat("a", 8191) + "\x00b", strings.Repeat("a", 8191) + "b", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, truncated := toolOutputPreview(tt.input)
			if got != tt.want {
				t.Fatalf("preview differs from expected text: got %d bytes, want %d", len(got), len(tt.want))
			}
			if truncated != tt.wantTruncated {
				t.Fatalf("truncated=%v, want %v: the flag must describe whether the stored preview dropped bytes", truncated, tt.wantTruncated)
			}
		})
	}
}

func TestToolOutputPreviewRuneBoundaries(t *testing.T) {
	t.Parallel()
	// Include every cut inside 2-, 3-, and 4-byte runes, plus both exact
	// boundaries. A literal replacement character is valid text to preserve.
	for _, char := range []string{"é", "中", "😀", "\uFFFD"} {
		for kept := 0; kept <= len(char); kept++ {
			t.Run(fmt.Sprintf("%s/%d", char, kept), func(t *testing.T) {
				prefix := strings.Repeat("a", 8192-kept)
				want := prefix
				if kept == len(char) {
					want += char
				}
				got, truncated := toolOutputPreview(prefix + char + "tail")
				if got != want {
					t.Fatalf("cut must retain exactly the complete runes: got %d bytes, want %d", len(got), len(want))
				}
				if !truncated {
					t.Fatal("every cut in this loop drops the tail, so truncated must be true")
				}
			})
		}
	}
}

// Exercise the reporting path: JSON encoding must not repair a split rune by
// inserting replacement characters into an otherwise valid tool result.
func TestExecuteAndDrain_ToolOutputUTF8Boundary(t *testing.T) {
	t.Parallel()
	d, rec := newTranscriptRecorder(t)
	messages := make(chan agent.Message, 1)
	messages <- agent.Message{
		Type: agent.MessageToolResult, Tool: "bash", Output: strings.Repeat("中", 2731),
	}
	close(messages)
	results := make(chan agent.Result, 1)
	results <- agent.Result{Status: "completed"}
	close(results)
	backend := sessionBackend{session: &agent.Session{Messages: messages, Result: results}}

	if _, _, err := d.executeAndDrain(context.Background(), backend, "p", agent.ExecOptions{},
		slog.Default(), "task-utf8-preview", "", new(atomic.Int32)); err != nil {
		t.Fatalf("executeAndDrain: %v", err)
	}
	reported := rec.snapshot()
	if len(reported) != 1 || reported[0].Type != "tool_result" || reported[0].Tool != "bash" {
		t.Fatalf("expected one bash tool_result, got %+v", reported)
	}
	want := strings.Repeat("中", 2730)
	if got := reported[0].Output; got != want {
		t.Fatalf("reported output differs from the complete-rune prefix: got %d bytes, want %d", len(got), len(want))
	}
}

func BenchmarkToolOutputPreview(b *testing.B) {
	for _, tt := range []struct{ name, input string }{
		{"8KiB", strings.Repeat("ordinary log line\n", 512)[:8192]},
		{"300KiBChinese", strings.Repeat("中", 100*1024)},
	} {
		b.Run(tt.name, func(b *testing.B) {
			b.SetBytes(int64(len(tt.input)))
			b.ReportAllocs()
			for b.Loop() {
				toolOutputPreview(tt.input)
			}
		})
	}
}
