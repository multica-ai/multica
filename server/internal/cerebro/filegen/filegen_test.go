package filegen

import (
	"strings"
	"testing"
)

func TestBuild_TextFormats(t *testing.T) {
	cases := []struct {
		format  string
		content string
		wantCT  string
		wantExt string
	}{
		{"md", "# Title\n\nbody", "text/markdown; charset=utf-8", "md"},
		{"txt", "plain text", "text/plain; charset=utf-8", "txt"},
		{"html", "<h1>hi</h1>", "text/html; charset=utf-8", "html"},
		{"svg", "<svg></svg>", "image/svg+xml; charset=utf-8", "svg"},
		{"xml", "<root/>", "application/xml; charset=utf-8", "xml"},
		{"MD", "uppercase format key", "text/markdown; charset=utf-8", "md"}, // case-insensitive
	}
	for _, tc := range cases {
		res, err := Build(Request{Format: tc.format, Content: tc.content})
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", tc.format, err)
		}
		if res.ContentType != tc.wantCT {
			t.Errorf("%s: content-type = %q, want %q", tc.format, res.ContentType, tc.wantCT)
		}
		if res.Ext != tc.wantExt {
			t.Errorf("%s: ext = %q, want %q", tc.format, res.Ext, tc.wantExt)
		}
		if string(res.Bytes) != tc.content {
			t.Errorf("%s: bytes did not preserve content verbatim: %q", tc.format, res.Bytes)
		}
	}
}

func TestBuild_EmptyContentRejected(t *testing.T) {
	for _, f := range []string{"md", "txt", "html", "svg", "xml"} {
		if _, err := Build(Request{Format: f, Content: "   "}); err == nil {
			t.Errorf("%s: expected error for empty content", f)
		}
	}
}

func TestBuild_UnsupportedFormat(t *testing.T) {
	// xlsx is a known future format but not yet generatable — must error clearly.
	for _, f := range []string{"docx", "pdf", "xlsx", "exe"} {
		if _, err := Build(Request{Format: f, Content: "x"}); err == nil {
			t.Errorf("expected error for unsupported format %q", f)
		}
	}
}

func TestBuild_JSONValidation(t *testing.T) {
	if _, err := Build(Request{Format: "json", Content: `{"a":1}`}); err != nil {
		t.Fatalf("valid json rejected: %v", err)
	}
	if _, err := Build(Request{Format: "json", Content: `{not json}`}); err == nil {
		t.Fatal("expected error for invalid json")
	}
}

func TestBuild_CSVFromRows(t *testing.T) {
	res, err := Build(Request{Format: "csv", Rows: [][]string{
		{"navn", "antal"},
		{"æbler, røde", "3"}, // comma forces quoting
	}})
	if err != nil {
		t.Fatalf("csv: %v", err)
	}
	if res.ContentType != "text/csv; charset=utf-8" || res.Ext != "csv" {
		t.Errorf("csv: ct=%q ext=%q", res.ContentType, res.Ext)
	}
	got := string(res.Bytes)
	if !strings.Contains(got, "navn,antal") {
		t.Errorf("csv missing header row: %q", got)
	}
	if !strings.Contains(got, `"æbler, røde"`) {
		t.Errorf("csv did not quote comma cell: %q", got)
	}
}

func TestBuild_CSVFromContentFallback(t *testing.T) {
	res, err := Build(Request{Format: "csv", Content: "a,b\n1,2\n"})
	if err != nil {
		t.Fatalf("csv fallback: %v", err)
	}
	if string(res.Bytes) != "a,b\n1,2\n" {
		t.Errorf("csv fallback altered content: %q", res.Bytes)
	}
}

func TestBuild_CSVRequiresInput(t *testing.T) {
	if _, err := Build(Request{Format: "csv"}); err == nil {
		t.Fatal("expected error: csv with neither rows nor content")
	}
}

func TestBuild_MaxOutputEnforced(t *testing.T) {
	big := strings.Repeat("a", MaxOutputBytes+1)
	if _, err := Build(Request{Format: "txt", Content: big}); err == nil {
		t.Fatal("expected error: output exceeds MaxOutputBytes")
	}
}

func TestSupportedFormats(t *testing.T) {
	got := SupportedFormats()
	want := []string{"csv", "html", "json", "md", "svg", "txt", "xml"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("SupportedFormats = %v, want %v", got, want)
	}
}
