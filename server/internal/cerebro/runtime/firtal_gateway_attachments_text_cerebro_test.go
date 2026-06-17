package runtime

import (
	"archive/zip"
	"bytes"
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

func TestGatewayAttachmentBlocksReadsXlsxTextCells(t *testing.T) {
	cases := []struct {
		name string
		body []byte
		want string
	}{
		{
			name: "inline string",
			body: testXlsx(t, "", `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="inlineStr"><is><t>EXCEL_INLINE_OK</t></is></c></row></sheetData></worksheet>`),
			want: "EXCEL_INLINE_OK",
		},
		{
			name: "shared string",
			body: testXlsx(t, `<sst xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><si><t>EXCEL_SHARED_OK</t></si></sst>`, `<worksheet xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main"><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c></row></sheetData></worksheet>`),
			want: "EXCEL_SHARED_OK",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			att := db.Attachment{
				Filename:    "report.xlsx",
				ContentType: "application/zip",
				Url:         "uploads/x/report.xlsx",
				SizeBytes:   int64(len(tc.body)),
			}
			blocks, err := gatewayAttachmentBlocks(context.Background(), stubAttachmentStorage{body: tc.body}, att)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(blocks) == 0 || blocks[0].Type != "text" || !strings.Contains(blocks[0].Text, tc.want) {
				t.Fatalf("expected text block containing %q, got %+v", tc.want, blocks)
			}
		})
	}
}

func testXlsx(t *testing.T, sharedStrings, sheet string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	files := map[string]string{
		"[Content_Types].xml":        `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/xl/workbook.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet.main+xml"/><Override PartName="/xl/worksheets/sheet1.xml" ContentType="application/vnd.openxmlformats-officedocument.spreadsheetml.worksheet+xml"/></Types>`,
		"_rels/.rels":                `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="xl/workbook.xml"/></Relationships>`,
		"xl/workbook.xml":            `<?xml version="1.0" encoding="UTF-8"?><workbook xmlns="http://schemas.openxmlformats.org/spreadsheetml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
		"xl/worksheets/sheet1.xml":   sheet,
	}
	if sharedStrings != "" {
		files["xl/sharedStrings.xml"] = sharedStrings
	}
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
func (s stubAttachmentStorage) Delete(context.Context, string)       {}
func (s stubAttachmentStorage) DeleteKeys(context.Context, []string) {}
func (s stubAttachmentStorage) KeyFromURL(raw string) string         { return raw }
func (s stubAttachmentStorage) CdnDomain() string                    { return "" }
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
