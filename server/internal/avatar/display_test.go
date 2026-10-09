package avatar

import "testing"

func TestDisplayPrecedence(t *testing.T) {
	tests := []struct {
		name   string
		stored string
		seed   string
		want   string
	}{
		{
			name:   "explicit image wins over generated seed",
			stored: "https://cdn.example.com/a.png",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "https://cdn.example.com/a.png",
		},
		{
			name:   "data URI image wins over generated seed",
			stored: "data:image/svg+xml,%3Csvg%3E%3C/svg%3E",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "data:image/svg+xml,%3Csvg%3E%3C/svg%3E",
		},
		{
			name:   "existing emoji wins over generated seed",
			stored: "emoji:\U0001F454",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "emoji:\U0001F454",
		},
		{
			name:   "emoji with surrounding whitespace is preserved",
			stored: "  emoji:\U0001F981  ",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "emoji:\U0001F981",
		},
		{
			name:   "bare emoji marker with no glyph is not a choice, generated wins",
			stored: "emoji:",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "gen:11111111-1111-1111-1111-111111111111",
		},
		{
			name:   "emoji marker with only whitespace payload is not a choice",
			stored: "emoji:   ",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "gen:11111111-1111-1111-1111-111111111111",
		},
		{
			name:   "generated is the default when nothing is stored",
			stored: "",
			seed:   "11111111-1111-1111-1111-111111111111",
			want:   "gen:11111111-1111-1111-1111-111111111111",
		},
		{
			name:   "emoji still renders when no seed exists",
			stored: "emoji:\U0001F454",
			seed:   "",
			want:   "emoji:\U0001F454",
		},
		{
			name:   "empty when neither exists",
			stored: "",
			seed:   "",
			want:   "",
		},
		{
			name:   "bare emoji marker and no seed projects nothing renderable",
			stored: "emoji:",
			seed:   "",
			want:   "",
		},
		{
			name:   "whitespace-only stored value behaves as empty",
			stored: "   ",
			seed:   "  seeded  ",
			want:   "gen:seeded",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Display(tt.stored, tt.seed); got != tt.want {
				t.Errorf("Display(%q, %q) = %q, want %q", tt.stored, tt.seed, got, tt.want)
			}
		})
	}
}

// Display must depend only on (stored, seed): identity inputs like name,
// provider, or runtime are not parameters at all, and the same pair must
// always project the same value.
func TestDisplayDeterministic(t *testing.T) {
	seed := "22222222-2222-2222-2222-222222222222"
	for _, tc := range []struct{ stored, want string }{
		{"", "gen:" + seed},
		{"emoji:\U0001F9E1", "emoji:\U0001F9E1"},
		{"https://cdn.example.com/a.png", "https://cdn.example.com/a.png"},
	} {
		first := Display(tc.stored, seed)
		for i := 0; i < 10; i++ {
			if got := Display(tc.stored, seed); got != first {
				t.Fatalf("Display(%q) is not deterministic: run %d = %q, first = %q", tc.stored, i, got, first)
			}
		}
		if first != tc.want {
			t.Fatalf("Display(%q) = %q, want %q", tc.stored, first, tc.want)
		}
	}
}

func TestMarkerHelpers(t *testing.T) {
	if !IsEmoji("emoji:\U0001F454") || IsEmoji("gen:abc") || IsEmoji("https://x/y.png") {
		t.Error("IsEmoji classification wrong")
	}
	if !IsGenerated("gen:abc") || IsGenerated("emoji:\U0001F454") || IsGenerated("") {
		t.Error("IsGenerated classification wrong")
	}
}
