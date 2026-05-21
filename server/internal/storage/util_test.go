package storage

import "testing"

func TestIsInlineContentType(t *testing.T) {
	cases := []struct {
		ct   string
		want bool
	}{
		{"image/png", true},
		{"image/jpeg", true},
		{"image/gif", true},
		{"image/webp", true},
		{"video/mp4", true},
		{"audio/mpeg", true},
		{"application/pdf", true},

		// SVG must NOT render inline — it can carry executable script.
		{"image/svg+xml", false},

		{"text/html", false},
		{"application/octet-stream", false},
		{"text/plain", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isInlineContentType(tc.ct); got != tc.want {
			t.Errorf("isInlineContentType(%q) = %v, want %v", tc.ct, got, tc.want)
		}
	}
}
