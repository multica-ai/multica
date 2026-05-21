package pdftext

import (
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

func TestExtractText(t *testing.T) {
	text, err := Extract(tinyTextPDF("Hello PDF text"), 2<<20)
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(string(text), "Hello PDF text") {
		t.Fatalf("text = %q, want extracted PDF text", string(text))
	}
}

func TestExtractNoText(t *testing.T) {
	_, err := Extract(tinyBlankPDF(), 2<<20)
	if err != ErrNoExtractableText {
		t.Fatalf("err = %v, want ErrNoExtractableText", err)
	}
}

func TestExtractParserPanic(t *testing.T) {
	origNewReader := NewReader
	NewReader = func(io.ReaderAt, int64) (*pdf.Reader, error) {
		panic("malformed xref")
	}
	defer func() { NewReader = origNewReader }()

	_, err := Extract([]byte("%PDF-broken"), 2<<20)
	if err == nil || !strings.Contains(err.Error(), "PDF parser panic: malformed xref") {
		t.Fatalf("err = %v, want parser panic error", err)
	}
}

func TestIsPreviewable(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		filename    string
		want        bool
	}{
		{name: "content type", contentType: "application/pdf", filename: "manual.bin", want: true},
		{name: "content type params", contentType: "application/pdf; charset=binary", filename: "manual.bin", want: true},
		{name: "extension", contentType: "application/octet-stream", filename: "manual.pdf", want: true},
		{name: "not pdf", contentType: "text/plain", filename: "manual.txt", want: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsPreviewable(tc.contentType, tc.filename); got != tc.want {
				t.Fatalf("IsPreviewable() = %v, want %v", got, tc.want)
			}
		})
	}
}

func tinyTextPDF(text string) []byte {
	return buildTinyPDF(fmt.Sprintf("BT /F1 24 Tf 100 700 Td (%s) Tj ET", text))
}

func tinyBlankPDF() []byte {
	return buildTinyPDF("")
}

func buildTinyPDF(stream string) []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>",
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream)+1, stream),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, 0, len(objects)+1)
	offsets = append(offsets, 0)
	for i, obj := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, obj)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, off := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(b.String())
}
