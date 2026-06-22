package storage

import "testing"

func TestContentDisposition(t *testing.T) {
	cases := []struct {
		name        string
		disposition string
		filename    string
		want        string
	}{
		{
			name:        "ascii filename preserves legacy shape",
			disposition: "attachment",
			filename:    "report.pdf",
			want:        `attachment; filename="report.pdf"`,
		},
		{
			name:        "header injection characters are sanitized",
			disposition: "attachment",
			filename:    `weird";name.txt`,
			want:        `attachment; filename="weird__name.txt"`,
		},
		{
			name:        "non ascii filename gets ascii fallback and utf8 filename star",
			disposition: "inline",
			filename:    "脉率.pdf",
			want:        `inline; filename="__.pdf"; filename*=UTF-8''%E8%84%89%E7%8E%87.pdf`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := contentDisposition(tc.disposition, tc.filename); got != tc.want {
				t.Fatalf("contentDisposition(%q, %q) = %q, want %q", tc.disposition, tc.filename, got, tc.want)
			}
		})
	}
}

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
		{"IMAGE/SVG+XML", false},
		{"Image/Svg+Xml", false},
		{"image/svg+xml; charset=utf-8", false},
		{"image/svg+xml;charset=utf-8", false},
		{"  image/svg+xml  ", false},
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

func TestExportedContentDispositionHelpers(t *testing.T) {
	if got := ContentDisposition("image/png", `nice"file;.png`); got != `inline; filename="nice_file_.png"` {
		t.Fatalf("ContentDisposition image = %q", got)
	}
	if got := ContentDisposition("text/plain", "notes.txt"); got != `attachment; filename="notes.txt"` {
		t.Fatalf("ContentDisposition text = %q", got)
	}
	if got := ContentDisposition("image/svg+xml", "logo.svg"); got != `attachment; filename="logo.svg"` {
		t.Fatalf("ContentDisposition svg = %q", got)
	}
}
