package runtime

import (
	"context"
	"io"
	"strings"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// stubAttachmentStorage returns a fixed body for any key so gatewayAttachmentBlocks
// can be exercised without a real S3/local backend.
type stubAttachmentStorage struct{ body []byte }

func (s stubAttachmentStorage) Upload(context.Context, string, []byte, string, string) (string, error) {
	return "", nil
}
func (s stubAttachmentStorage) Delete(context.Context, string)        {}
func (s stubAttachmentStorage) DeleteKeys(context.Context, []string)  {}
func (s stubAttachmentStorage) KeyFromURL(raw string) string          { return raw }
func (s stubAttachmentStorage) CdnDomain() string                     { return "" }
func (s stubAttachmentStorage) GetReader(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader(string(s.body))), nil
}

// TestGatewayAttachmentBlocksReadsTextLikeFiles verifies that attachments whose
// bytes are readable UTF-8 text — including types the explicit cases do not
// cover, like application/json — are injected as text the agent can read
// (TECH-3657), and that binary blobs are still rejected.
func TestGatewayAttachmentBlocksReadsTextLikeFiles(t *testing.T) {
	cases := []struct {
		name        string
		filename    string
		contentType string
		body        []byte
		wantText    string // substring expected in the injected text block; "" = expect error
	}{
		{"json", "data.json", "application/json", []byte(`{"marker":"JSON_READ_OK"}`), "JSON_READ_OK"},
		{"csv as octet-stream", "rows.csv", "application/octet-stream", []byte("a,b\n1,2\n"), "a,b"},
		{"plain text", "note.txt", "text/plain", []byte("hello world"), "hello world"},
		{"yaml", "conf.yaml", "application/x-yaml", []byte("key: value\n"), "key: value"},
		{"binary rejected", "blob.bin", "application/octet-stream", []byte{0x00, 0x01, 0x02, 0xff}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			att := db.Attachment{
				Filename:    tc.filename,
				ContentType: tc.contentType,
				Url:         "uploads/x/" + tc.filename,
				SizeBytes:   int64(len(tc.body)),
			}
			blocks, err := gatewayAttachmentBlocks(context.Background(), stubAttachmentStorage{body: tc.body}, att)
			if tc.wantText == "" {
				if err == nil {
					t.Fatalf("expected error for binary attachment, got blocks %+v", blocks)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(blocks) == 0 || blocks[0].Type != "text" || !strings.Contains(blocks[0].Text, tc.wantText) {
				t.Fatalf("expected text block containing %q, got %+v", tc.wantText, blocks)
			}
		})
	}
}
