package service

import (
	"strings"
	"testing"
)

// TestQuickCreateFailureDetail pins the reason-extraction that turns the opaque
// "agent finished without creating an issue" inbox into the concrete CLI error
// the quick-create agent actually emitted (GH #5885).
func TestQuickCreateFailureDetail(t *testing.T) {
	t.Parallel()

	t.Run("real CLI error passes through", func(t *testing.T) {
		t.Parallel()
		out := "Error: an active issue already exists: JKY-30 (blocked)."
		result := []byte(`{"output":"` + out + `"}`)
		if got := quickCreateFailureDetail(result); got != out {
			t.Fatalf("expected passthrough of the CLI error\n got: %q\nwant: %q", got, out)
		}
	})

	t.Run("empty output yields empty so caller uses its generic default", func(t *testing.T) {
		t.Parallel()
		if got := quickCreateFailureDetail([]byte(`{"output":""}`)); got != "" {
			t.Fatalf("expected empty detail for empty output, got %q", got)
		}
	})

	t.Run("whitespace-only output yields empty", func(t *testing.T) {
		t.Parallel()
		if got := quickCreateFailureDetail([]byte(`{"output":"   \n  "}`)); got != "" {
			t.Fatalf("expected empty detail for whitespace output, got %q", got)
		}
	})

	t.Run("missing output field yields empty", func(t *testing.T) {
		t.Parallel()
		if got := quickCreateFailureDetail([]byte(`{"task_id":"abc"}`)); got != "" {
			t.Fatalf("expected empty detail when output absent, got %q", got)
		}
	})

	t.Run("malformed json yields empty", func(t *testing.T) {
		t.Parallel()
		if got := quickCreateFailureDetail([]byte(`not json`)); got != "" {
			t.Fatalf("expected empty detail for malformed json, got %q", got)
		}
	})

	t.Run("escaped paragraph breaks are decoded to real newlines", func(t *testing.T) {
		t.Parallel()
		// The daemon may deliver an Output that is one escaped blob with 2-char
		// `\n\n` paragraph breaks; they must render as real line breaks,
		// matching the comment-fallback path.
		result := []byte(`{"output":"Error: duplicate issue.\\n\\nSee JKY-30."}`)
		got := quickCreateFailureDetail(result)
		if got != "Error: duplicate issue.\n\nSee JKY-30." {
			t.Fatalf("expected decoded newlines, got %q", got)
		}
	})

	t.Run("single-line path is not decoded", func(t *testing.T) {
		t.Parallel()
		out := `Error: cannot read C:\\tools\\node_modules\\repo`
		want := `Error: cannot read C:\tools\node_modules\repo`
		if got := quickCreateFailureDetail([]byte(`{"output":"` + out + `"}`)); got != want {
			t.Fatalf("expected verbatim path\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("backslashes in multi-line output pass through", func(t *testing.T) {
		t.Parallel()
		// GH #8845: once the output has real line breaks, `\t` and `\r` are
		// content (here a Windows path), not escapes to decode.
		result := []byte(`{"output":"Error: cannot read\nC:\\tools\\repo\\task.md"}`)
		want := "Error: cannot read\nC:\\tools\\repo\\task.md"
		if got := quickCreateFailureDetail(result); got != want {
			t.Fatalf("expected verbatim output\n got: %q\nwant: %q", got, want)
		}
	})

	t.Run("oversized output is dropped to a safe generic notice", func(t *testing.T) {
		t.Parallel()
		// A runaway raw-stream dump must not have its tool-trace head surfaced
		// as the failure reason (same concern as GH #5455).
		huge := strings.Repeat("x", maxQuickCreateFailureDetailRunes+1)
		result := []byte(`{"output":"` + huge + `"}`)
		if got := quickCreateFailureDetail(result); got != quickCreateOversizedFailureDetail {
			t.Fatalf("expected oversized notice, got %d-rune body", len(got))
		}
	})

	t.Run("output exactly at the cap passes through", func(t *testing.T) {
		t.Parallel()
		body := strings.Repeat("x", maxQuickCreateFailureDetailRunes)
		result := []byte(`{"output":"` + body + `"}`)
		if got := quickCreateFailureDetail(result); got != body {
			t.Fatalf("expected body at cap to pass through, got %d runes", len(got))
		}
	})
}
