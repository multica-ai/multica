package sessions

import "testing"

func TestModelContextWindow_CrossModel(t *testing.T) {
	cases := []struct {
		model string
		want  int64
	}{
		{"claude-opus-4-8", 200_000},
		{"claude-sonnet-4-6", 200_000},
		{"claude-haiku-4-5", 200_000},
		{"gpt-5.5", 272_000},
		{"codex-mini", 272_000},
		{"gemini-2.5-pro", 1_000_000},
		{"", 200_000}, // unknown/empty falls back to the standard window
		{"some-future", 200_000},
	}
	for _, c := range cases {
		if got := modelContextWindow(c.model); got != c.want {
			t.Errorf("modelContextWindow(%q) = %d, want %d", c.model, got, c.want)
		}
	}
}

func TestClampPercent(t *testing.T) {
	cases := []struct{ in, want int }{
		{-5, 0}, {0, 0}, {50, 50}, {100, 100}, {137, 100},
	}
	for _, c := range cases {
		if got := clampPercent(c.in); got != c.want {
			t.Errorf("clampPercent(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}
