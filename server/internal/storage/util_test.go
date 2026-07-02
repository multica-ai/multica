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

func TestContentDisposition(t *testing.T) {
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

func TestContentDispositionUTF8Filename(t *testing.T) {
	// RFC 6266: `filename=` is ASCII-only. A non-ASCII name (common for this
	// product's users) must ride in `filename*=UTF-8''…` with an ASCII fallback,
	// otherwise stricter clients/proxies garble the download name.
	got := ContentDisposition("application/zip", "测试.zip")
	want := `attachment; filename="__.zip"; filename*=UTF-8''%E6%B5%8B%E8%AF%95.zip`
	if got != want {
		t.Fatalf("ContentDisposition utf8 = %q, want %q", got, want)
	}

	got2 := AttachmentContentDisposition("résumé.pdf")
	want2 := `attachment; filename="r_sum_.pdf"; filename*=UTF-8''r%C3%A9sum%C3%A9.pdf`
	if got2 != want2 {
		t.Fatalf("AttachmentContentDisposition utf8 = %q, want %q", got2, want2)
	}
}
