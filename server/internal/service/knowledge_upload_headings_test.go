package service

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/internal/knowledge"
)

// The fixtures in testdata/upload are public samples, unchanged unless noted:
//   - headings-sample.docx: Apache POI test-data heading123.docx (Apache-2.0),
//     Heading 1 to 3 paragraphs.
//   - no-headings-sample.docx: Apache POI test-data SampleDoc.docx (Apache-2.0),
//     no heading styles.
//   - headings-sample.pdf: Apache PDFBox test resource eu-001.pdf, an E-PRTR
//     thresholds table from an EU/EEA document with section headings.
//   - no-headings-sample.pdf: Apache PDFBox test resource opencompany-test-document.pdf,
//     text "Test" only.
//   - corrupt-truncated.docx and corrupt-truncated.pdf: the first 20000 and 15000
//     bytes of the two samples above, so the zip directory and the PDF trailer are cut off.
func uploadFixture(t *testing.T, name string) []byte {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("testdata", "upload", name))
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestDocxHeadingsAreTheHeadingStyledParagraphs(t *testing.T) {
	headings, err := docxHeadings(uploadFixture(t, "headings-sample.docx"))
	if err != nil {
		t.Fatalf("headings-sample.docx: %v", err)
	}
	const want = "# First paragraph\n## Second paragraph\n### Third paragraph"
	if headings != want {
		t.Fatalf("headings = %q, want %q", headings, want)
	}

	headings, err = docxHeadings(uploadFixture(t, "no-headings-sample.docx"))
	if err != nil || headings != "" {
		t.Fatalf("a document without heading styles: headings %q, err %v", headings, err)
	}
}

func TestPDFHeadingsAreTheLinesOfItsText(t *testing.T) {
	headings, err := pdfHeadings(uploadFixture(t, "headings-sample.pdf"))
	if err != nil {
		t.Fatalf("headings-sample.pdf: %v", err)
	}
	for _, line := range []string{"E-PRTR pollutants and their thresholds", "Greenhouse gases", "Heavy metals", "Pesticides"} {
		if !strings.Contains(headings, "## "+line+"\n") {
			t.Errorf("heading line %q missing from %q", line, headings)
		}
	}

	headings, err = pdfHeadings(uploadFixture(t, "no-headings-sample.pdf"))
	if err != nil || strings.Contains(headings, "Greenhouse") {
		t.Fatalf("a PDF without the sample headings: headings %q, err %v", headings, err)
	}
}

// A file that cannot be parsed is refused with a clear error, never a panic, and the
// refusal is the same for every malformed input.
func TestUnreadableWordAndPDFFilesAreRefusedClearly(t *testing.T) {
	cases := []struct {
		name  string
		read  func([]byte) (string, error)
		input []byte
	}{
		{"truncated docx", docxHeadings, uploadFixture(t, "corrupt-truncated.docx")},
		{"truncated pdf", pdfHeadings, uploadFixture(t, "corrupt-truncated.pdf")},
		{"plain text named docx", docxHeadings, []byte("# Summary\n")},
		{"plain text named pdf", pdfHeadings, []byte("# Summary\n")},
		{"pdf header without a body", pdfHeadings, []byte("%PDF-1.4\n%%EOF\n")},
		{"pdf with a broken trailer", pdfHeadings, []byte("%PDF-1.4\nstartxref\n0\n%%EOF")},
		{"empty docx", docxHeadings, nil},
		{"empty pdf", pdfHeadings, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			headings, err := tc.read(tc.input)
			var invalid *KnowledgeInvalidError
			if err == nil {
				// A malformed file that parses to no text has no headings; that is
				// reported as missing sections by the caller, not here.
				if headings != "" {
					t.Fatalf("a malformed file produced headings %q", headings)
				}
				return
			}
			if !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "cannot be read") {
				t.Fatalf("err = %v, want a KnowledgeInvalidError saying the file cannot be read", err)
			}
		})
	}
}

// S3.3: a Word or PDF upload is checked for its kind's headings before it is stored.
func TestUploadChecksWordAndPDFHeadingsBeforeStorage(t *testing.T) {
	e := newKnowledgeEnv(t).enabled()
	ctx := context.Background()
	upload := func(name string) (KnowledgeDocumentView, bool, error) {
		body := uploadFixture(t, name)
		return e.svc.UploadDocument(ctx, e.ws, memberActor(e), KnowledgeUploadInput{
			KB: "market", Kind: "market_report", Name: name, Size: int64(len(body)), Content: bytes.NewReader(body),
		})
	}
	// A kind's template must carry its required headings, so both are set together.
	setRequired := func(sections ...string) {
		t.Helper()
		template := defaultTemplate("Sizing", sections)
		if _, err := e.svc.UpdateKnowledgeKind(ctx, e.ws, "market_report", KnowledgeKindPatch{Template: &template, RequiredSections: &sections}); err != nil {
			t.Fatal(err)
		}
	}
	missingOf := func(err error) string {
		t.Helper()
		var missing *KnowledgeMissingSectionsError
		if !errors.As(err, &missing) {
			t.Fatalf("err = %v, want the missing list", err)
		}
		return strings.Join(missing.Missing, ",")
	}

	setRequired("First paragraph", "Second paragraph", "Fourth paragraph")
	if _, _, err := upload("headings-sample.docx"); missingOf(err) != "Fourth paragraph" {
		t.Fatalf("Word file missing one heading: missing %q", missingOf(err))
	}
	if _, _, err := upload("no-headings-sample.docx"); missingOf(err) != "First paragraph,Second paragraph,Fourth paragraph" {
		t.Fatalf("Word file without headings: missing %q", missingOf(err))
	}
	if _, _, err := upload("no-headings-sample.pdf"); missingOf(err) != "First paragraph,Second paragraph,Fourth paragraph" {
		t.Fatalf("PDF without headings: missing %q", missingOf(err))
	}
	for _, name := range []string{"corrupt-truncated.docx", "corrupt-truncated.pdf"} {
		var invalid *KnowledgeInvalidError
		if _, _, err := upload(name); !errors.As(err, &invalid) || !strings.Contains(invalid.Message, "cannot be read") {
			t.Fatalf("%s: err = %v, want a clear unreadable-file error", name, err)
		}
	}
	if e.fake.Calls(knowledge.OpCreateFile) != 0 || e.linkCount() != 0 {
		t.Fatal("a refused Word or PDF upload reached WeKnora or the registry")
	}

	setRequired("First paragraph", "Second paragraph", "Third paragraph")
	view, created, err := upload("headings-sample.docx")
	if err != nil || !created || view.State != "published" {
		t.Fatalf("complete Word file: view %+v, created %v, err %v", view, created, err)
	}

	setRequired("E-PRTR pollutants and their thresholds", "Greenhouse gases", "Heavy metals")
	if _, created, err := upload("headings-sample.pdf"); err != nil || !created {
		t.Fatalf("complete PDF: created %v, err %v", created, err)
	}
}
