package storage

import (
	"strings"
	"testing"
)

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

func TestContentDispositionNonASCII(t *testing.T) {
	// Chinese filename — should include filename* and an ASCII-only fallback.
	got := ContentDisposition("image/webp", "微信图片_2026-04-09_162004_785.webp")
	if !strings.Contains(got, "filename*=UTF-8''") {
		t.Fatalf("ContentDisposition should include filename* for non-ASCII: %q", got)
	}
	const legacyPrefix = `filename="`
	start := strings.Index(got, legacyPrefix)
	if start == -1 {
		t.Fatalf("ContentDisposition should keep ASCII fallback: %q", got)
	}
	// Legacy filename parameter must be ASCII only.
	start += len(legacyPrefix)
	end := strings.IndexByte(got[start:], '"')
	if end == -1 {
		t.Fatalf("ContentDisposition fallback is not terminated: %q", got)
	}
	fallback := got[start : start+end]
	for _, r := range fallback {
		if r > 0x7f {
			t.Fatalf("legacy filename parameter must be ASCII only, got: %q", fallback)
		}
	}
	// Modern filename* must contain the percent-encoded original name.
	if !strings.Contains(got, "%E5%BE%AE%E4%BF%A1%E5%9B%BE%E7%89%87") {
		t.Fatalf("filename* should encode the original Chinese name: %q", got)
	}

	// ASCII-only filename — no filename*
	got2 := ContentDisposition("text/plain", "notes.txt")
	if strings.Contains(got2, "filename*=") {
		t.Fatalf("ContentDisposition should NOT include filename* for ASCII: %q", got2)
	}

	// Mixed ASCII + special chars (but no non-ASCII) — no filename*
	got3 := ContentDisposition("image/png", `nice"file;.png`)
	if strings.Contains(got3, "filename*=") {
		t.Fatalf("ContentDisposition should NOT include filename* for pure ASCII with special chars: %q", got3)
	}
}

func TestAttachmentContentDispositionNonASCII(t *testing.T) {
	got := AttachmentContentDisposition("微信图片_2026-04-09_162004_785.webp")
	wantPrefix := `attachment; filename="_____2026-04-09_162004_785.webp"; filename*=UTF-8''`
	if !strings.HasPrefix(got, wantPrefix) {
		t.Fatalf("AttachmentContentDisposition = %q, want prefix %q", got, wantPrefix)
	}
	if !strings.Contains(got, "%E5%BE%AE%E4%BF%A1%E5%9B%BE%E7%89%87") {
		t.Fatalf("AttachmentContentDisposition should encode the original Chinese name: %q", got)
	}
}

func TestRFC5987Encode(t *testing.T) {
	got := rfc5987Encode("微信图片")
	want := "%E5%BE%AE%E4%BF%A1%E5%9B%BE%E7%89%87"
	if got != want {
		t.Fatalf("rfc5987Encode(%q) = %q, want %q", "微信图片", got, want)
	}
	// ASCII pass-through
	if got2 := rfc5987Encode("hello.txt"); got2 != "hello.txt" {
		t.Fatalf("rfc5987Encode(ASCII) = %q, want %q", got2, "hello.txt")
	}
	// Space and special chars are encoded
	if got3 := rfc5987Encode("a b"); got3 != "a%20b" {
		t.Fatalf("rfc5987Encode(space) = %q, want %q", got3, "a%20b")
	}
}

func TestNeedsRFC5987Encoding(t *testing.T) {
	if needsRFC5987Encoding("hello.txt") {
		t.Fatal("ASCII filename should not need RFC 5987 encoding")
	}
	if !needsRFC5987Encoding("微信图片.webp") {
		t.Fatal("Chinese filename should need RFC 5987 encoding")
	}
}
