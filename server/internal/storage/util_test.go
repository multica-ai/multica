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
		// MIME types are case-insensitive (RFC 2045 §5.1) and may carry
		// parameters. The SVG carve-out is a security boundary, so any
		// variant that resolves to image/svg+xml must also be blocked.
		{"IMAGE/SVG+XML", false},
		{"Image/Svg+Xml", false},
		{"image/svg+xml; charset=utf-8", false},
		{"image/svg+xml;charset=utf-8", false},
		{"  image/svg+xml  ", false},
		// Normalization must not break the positive cases either.
		{"IMAGE/PNG", true},
		{"image/png; foo=bar", true},
		{"  application/pdf", true},

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
