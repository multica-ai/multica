package note

import "testing"

// TestRelocateAnchor covers the comment/suggestion anchoring robustness claim
// (Wave 3 / G1): a comment must re-find its span even after the note is edited
// above, around, or near it. Pure function — always runs (no DB).
func TestRelocateAnchor(t *testing.T) {
	const quote = "quick brown fox"
	tests := []struct {
		name           string
		body           string
		prefix, suffix string
		want           int
	}{
		{
			name:   "exact full context",
			body:   "the quick brown fox jumps",
			prefix: "the ",
			suffix: " jumps",
			want:   4,
		},
		{
			name:   "text inserted ABOVE shifts offset but context still matches",
			body:   "a new first paragraph.\n\nthe quick brown fox jumps",
			prefix: "the ",
			suffix: " jumps",
			want:   28,
		},
		{
			name:   "suffix changed — falls back to prefix+quote",
			body:   "the quick brown fox leaps high",
			prefix: "the ",
			suffix: " jumps",
			want:   4,
		},
		{
			name:   "prefix changed — falls back to quote+suffix",
			body:   "a quick brown fox jumps",
			prefix: "the ",
			suffix: " jumps",
			want:   2,
		},
		{
			name:   "both context sides changed — bare quote still found",
			body:   "WHO let the quick brown fox OUT",
			prefix: "the ",
			suffix: " jumps",
			want:   12,
		},
		{
			name:   "quote removed entirely — not found",
			body:   "the lazy dog sleeps",
			prefix: "the ",
			suffix: " jumps",
			want:   -1,
		},
		{
			name:   "disambiguated by full context when quote appears twice",
			body:   "quick brown fox here, and the quick brown fox jumps there",
			prefix: "the ",
			suffix: " jumps",
			want:   30,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := relocateAnchor(tc.body, tc.prefix, quote, tc.suffix)
			if got != tc.want {
				t.Errorf("relocateAnchor = %d, want %d (body=%q)", got, tc.want, tc.body)
			}
			if got >= 0 && tc.body[got:got+len(quote)] != quote {
				t.Errorf("relocateAnchor pointed at %q, not the quote", tc.body[got:got+len(quote)])
			}
		})
	}
}

// TestRelocateAnchorEmptyQuote: an empty quote is unanchorable.
func TestRelocateAnchorEmptyQuote(t *testing.T) {
	if got := relocateAnchor("any body", "p", "", "s"); got != -1 {
		t.Errorf("empty quote should return -1, got %d", got)
	}
}
