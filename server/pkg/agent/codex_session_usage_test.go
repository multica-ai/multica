package agent

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
	"time"
)

func TestParseCodexSessionFileContinuesAfterLargeRecords(t *testing.T) {
	t.Parallel()
	first := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2}}}}`
	last := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20}}}}`
	model := `{"type":"turn_context","payload":{"model":"test-model"}}`
	large := `{"type":"response_item","payload":{"type":"message","content":[{"type":"input_image","image_url":"data:image/png;base64,` + strings.Repeat("A", 2<<20) + `"}]}}`
	for _, tc := range []struct {
		name    string
		content string
	}{
		{"between usage snapshots", first + "\n" + large + "\n" + model + "\n" + last + "\n"},
		{"before first snapshot", large + "\n" + model + "\n" + last + "\n"},
		{"consecutive large records and CRLF", first + "\r\n\r\n" + large + "\r\n" + large + "\r\n" + model + "\r\n" + last + "\r\n"},
		{"final snapshot without newline", first + "\n" + large + "\n" + model + "\n" + last},
		{"large final record without newline", model + "\n" + last + "\n" + large},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got := parseCodexSessionFile(path)
			want := TokenUsage{InputTokens: 100, OutputTokens: 20}
			if got == nil || got.usage != want || got.model != "test-model" {
				t.Fatalf("usage = %+v, want %+v with model test-model", got, want)
			}
		})
	}
}

func TestParseCodexSessionFileSinceLargeRecordsPreserveResumeAccounting(t *testing.T) {
	t.Parallel()
	startTime := time.Date(2026, time.July, 13, 0, 0, 10, 0, time.UTC)
	large := `{"type":"response_item","payload":{"type":"function_call_output","output":"` + strings.Repeat("x", 2<<20) + `"}}`
	baseline := `{"timestamp":"2026-07-13T00:00:05Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"cached_input_tokens":60,"output_tokens":10}}}}`
	for _, tc := range []struct {
		name  string
		lines []string
		want  TokenUsage
	}{
		{
			name: "baseline after large record",
			lines: []string{large, baseline, large,
				`{"timestamp":"2026-07-13T00:00:11Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":80,"output_tokens":20}}}}`},
			want: TokenUsage{InputTokens: 30, CacheReadTokens: 20, OutputTokens: 10},
		},
		{
			name: "reset across large records",
			lines: []string{baseline,
				`{"timestamp":"2026-07-13T00:00:11Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":80,"output_tokens":20}}}}`,
				large,
				`{"timestamp":"2026-07-13T00:00:12Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"cached_input_tokens":5,"output_tokens":2}}}}`,
				large,
				`{"timestamp":"2026-07-13T00:00:13Z","type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":30,"cached_input_tokens":10,"output_tokens":6}}}}`},
			want: TokenUsage{InputTokens: 50, CacheReadTokens: 30, OutputTokens: 16},
		},
		{
			name: "timestamp-less cache-only delta after turn context",
			lines: []string{baseline, large,
				`{"timestamp":"2026-07-13T00:00:11Z","type":"turn_context","payload":{"model":"test-model"}}`,
				large,
				`{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":150,"cached_input_tokens":110,"output_tokens":10}}}}`},
			want: TokenUsage{CacheReadTokens: 50},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "session.jsonl")
			if err := os.WriteFile(path, []byte(strings.Join(tc.lines, "\n")), 0o600); err != nil {
				t.Fatal(err)
			}
			got := parseCodexSessionFileSince(path, startTime, true)
			if got == nil || got.usage != tc.want {
				t.Fatalf("usage = %+v, want resumed delta %+v", got, tc.want)
			}
		})
	}
}

func TestParseCodexSessionFileRejectsOversizedUsageCandidates(t *testing.T) {
	t.Parallel()
	first := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2}}}}`
	for _, marker := range []string{"token_count", "turn_context"} {
		// Include markers in later chunks and across chunk boundaries. Missing
		// one could discard a resume baseline or a counter reset and overbill.
		for _, offset := range []int{0, (1 << 20) - 5, (2 << 20) + 10} {
			t.Run(fmt.Sprintf("%s at %d", marker, offset), func(t *testing.T) {
				prefix := `{"type":"event_msg","payload":{"type":"`
				suffix := `","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20}},"padding":"`
				if marker == "turn_context" {
					prefix = `{"type":"`
					suffix = `","payload":{"model":"test-model","padding":"`
				}
				large := strings.Repeat(" ", max(0, offset-len(prefix))) + prefix + marker + suffix + strings.Repeat("x", 2<<20) + `"}}`
				path := filepath.Join(t.TempDir(), "session.jsonl")
				if err := os.WriteFile(path, []byte(first+"\n"+large+"\n"+first+"\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if got := parseCodexSessionFile(path); got != nil {
					t.Fatalf("incomplete usage = %+v, want nil after an oversized usage candidate", got)
				}
			})
		}
	}
}

func TestParseCodexSessionUsageRejectsReadErrors(t *testing.T) {
	t.Parallel()
	snapshot := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":10,"output_tokens":2}}}}`
	readErr := errors.New("session read failed")
	for _, tc := range []struct {
		name string
		data string
	}{
		{"after complete snapshot", snapshot + "\n"},
		{"after unterminated snapshot", snapshot},
		{"while draining large record", snapshot + "\n" + strings.Repeat("x", 2<<20)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := io.MultiReader(strings.NewReader(tc.data), iotest.ErrReader(readErr))
			got, err := parseCodexSessionUsage(reader, time.Time{}, false)
			if !errors.Is(err, readErr) || got != nil {
				t.Fatalf("usage = %+v, error = %v; want nil usage and read error", got, err)
			}
		})
	}
}

func TestParseCodexSessionUsageAcceptsRecordsWithinBuffer(t *testing.T) {
	t.Parallel()
	snapshot := `{"type":"event_msg","payload":{"type":"token_count","info":{"total_token_usage":{"input_tokens":100,"output_tokens":20}}}}`
	for _, ending := range []string{"\n", "\r\n", ""} {
		t.Run(fmt.Sprintf("ending %q", ending), func(t *testing.T) {
			// The entire physical line, including CRLF, fits in the 1 MiB buffer.
			padding := strings.Repeat(" ", (1<<20)-1-len(snapshot)-len(ending))
			got, err := parseCodexSessionUsage(strings.NewReader(padding+snapshot+ending), time.Time{}, false)
			want := TokenUsage{InputTokens: 100, OutputTokens: 20}
			if err != nil || got == nil || got.usage != want {
				t.Fatalf("usage = %+v, error = %v; want %+v", got, err, want)
			}
		})
	}
}
