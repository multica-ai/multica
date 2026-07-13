package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	attachmenttext "github.com/multica-ai/multica/packages/cerebro-attachment-text"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type gatewayAttachmentRunner struct{}

func (gatewayAttachmentRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	switch name {
	case "pdftoppm":
		return nil, os.WriteFile(filepath.Join(dir, "page-1.png"), []byte("png"), 0o600)
	case "tesseract":
		return []byte("Secret code: ORANGE-7319"), nil
	case "antiword":
		return []byte("Secret code: COBALT-4826"), nil
	}
	return nil, nil
}

func TestGatewayAttachmentTextUsesPortableExtractor(t *testing.T) {
	originalRunner := attachmenttext.DefaultRunner
	attachmenttext.DefaultRunner = gatewayAttachmentRunner{}
	defer func() { attachmenttext.DefaultRunner = originalRunner }()

	cases := []struct {
		filename string
		body     []byte
		want     string
	}{
		{filename: "scanned-ocr-test.pdf", body: tinyBlankPDF(), want: "ORANGE-7319"},
		{filename: "legacy-source.doc", body: []byte("legacy-doc"), want: "COBALT-4826"},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			att := db.Attachment{Filename: tc.filename, ContentType: "application/octet-stream", Url: "uploads/" + tc.filename, SizeBytes: int64(len(tc.body))}
			text, err := gatewayAttachmentText(context.Background(), stubAttachmentStorage{body: tc.body}, att)
			if err != nil {
				t.Fatalf("gatewayAttachmentText returned error: %v", err)
			}
			if !strings.Contains(text, tc.want) {
				t.Fatalf("text = %q, want %s", text, tc.want)
			}
		})
	}
}

func tinyBlankPDF() []byte {
	objects := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Contents 4 0 R >>",
		"<< /Length 1 >>\nstream\n\nendstream",
	}
	var b strings.Builder
	b.WriteString("%PDF-1.4\n")
	offsets := []int{0}
	for i, object := range objects {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, object)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets[1:] {
		fmt.Fprintf(&b, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&b, "trailer\n<< /Root 1 0 R /Size %d >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return []byte(b.String())
}
