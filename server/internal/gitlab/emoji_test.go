package gitlab

import "testing"

func TestEmojiUnicodeToShortcode(t *testing.T) {
	cases := []struct {
		unicode string
		want    string
		ok      bool
	}{
		{"👍", "thumbsup", true},
		{"👎", "thumbsdown", true},
		{"❤️", "heart", true},
		{"🎉", "tada", true},
		{"🚀", "rocket", true},
		{"🔥", "fire", true},
		{"💯", "100", true},
		{"✅", "white_check_mark", true},
		{"🦀", "", false}, // crab — unmapped
		{"", "", false},  // empty
	}
	for _, tc := range cases {
		got, ok := EmojiUnicodeToShortcode(tc.unicode)
		if ok != tc.ok {
			t.Errorf("EmojiUnicodeToShortcode(%q) ok = %v, want %v", tc.unicode, ok, tc.ok)
		}
		if got != tc.want {
			t.Errorf("EmojiUnicodeToShortcode(%q) = %q, want %q", tc.unicode, got, tc.want)
		}
	}
}

func TestEmojiShortcodeToUnicode(t *testing.T) {
	cases := []struct {
		shortcode string
		want      string
		ok        bool
	}{
		{"thumbsup", "👍", true},
		{"heart", "❤️", true},
		{"rocket", "🚀", true},
		{"facepalm", "", false}, // not in our default set
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := EmojiShortcodeToUnicode(tc.shortcode)
		if ok != tc.ok {
			t.Errorf("EmojiShortcodeToUnicode(%q) ok = %v, want %v", tc.shortcode, ok, tc.ok)
		}
		if got != tc.want {
			t.Errorf("EmojiShortcodeToUnicode(%q) = %q, want %q", tc.shortcode, got, tc.want)
		}
	}
}

// TestEmojiRoundTrip verifies every supported unicode round-trips through
// shortcode and back. Catches duplicate-shortcode entries in the map.
func TestEmojiRoundTrip(t *testing.T) {
	for unicode, shortcode := range emojiUnicodeToShortcode {
		back, ok := EmojiShortcodeToUnicode(shortcode)
		if !ok {
			t.Errorf("shortcode %q has no reverse mapping for unicode %q", shortcode, unicode)
			continue
		}
		if back != unicode {
			t.Errorf("round-trip for %q: got %q via shortcode %q", unicode, back, shortcode)
		}
	}
}
