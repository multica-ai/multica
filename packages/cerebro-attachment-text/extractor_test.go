package attachmenttext

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls []string
}

type wordFallbackRunner struct{}

func (wordFallbackRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	if name == "antiword" {
		return nil, errors.New("text stream too small")
	}
	if name == "lowriter" {
		input := args[len(args)-1]
		base := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
		if err := os.WriteFile(filepath.Join(dir, base+".txt"), []byte("Secret code: COBALT-4826"), 0o600); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return nil, errors.New("unexpected command")
}

func (r *fakeRunner) Run(_ context.Context, dir, name string, args ...string) ([]byte, error) {
	r.calls = append(r.calls, name+" "+strings.Join(args, " "))
	switch name {
	case "pdftoppm":
		if err := os.WriteFile(filepath.Join(dir, "page-1.png"), []byte("png"), 0o600); err != nil {
			return nil, err
		}
	case "tesseract":
		return []byte("SCANNED DOCUMENT TEST\nSecret code: ORANGE-7319\n"), nil
	case "antiword":
		return []byte("Legacy Word test\nSecret code: COBALT-4826\n"), nil
	}
	return nil, nil
}

func TestExtractScannedPDFUsesServerOCR(t *testing.T) {
	runner := &fakeRunner{}
	text, err := Extract(context.Background(), []byte("%PDF-blank"), "application/pdf", "scanned-ocr-test.pdf", Options{
		MaxBytes: 2 << 20,
		Runner:   runner,
		PDFText: func([]byte, int64) ([]byte, error) {
			return nil, ErrNoExtractableText
		},
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(string(text), "ORANGE-7319") {
		t.Fatalf("text = %q, want OCR control code", text)
	}
	if got := strings.Join(runner.calls, "\n"); !strings.Contains(got, "pdftoppm") || !strings.Contains(got, "tesseract") {
		t.Fatalf("calls = %q, want pdftoppm and tesseract", got)
	}
}

func TestExtractLegacyWordUsesServerConverter(t *testing.T) {
	cases := []struct {
		filename string
		want     string
	}{
		{filename: "legacy-source.doc", want: "COBALT-4826"},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			runner := &fakeRunner{}
			text, err := Extract(context.Background(), []byte("legacy-binary"), "application/octet-stream", tc.filename, Options{
				MaxBytes: 2 << 20,
				Runner:   runner,
			})
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if !strings.Contains(string(text), tc.want) {
				t.Fatalf("text = %q, want %s", text, tc.want)
			}
			if got := strings.Join(runner.calls, "\n"); !strings.Contains(got, "antiword") {
				t.Fatalf("calls = %q, want antiword", got)
			}
		})
	}
}

func TestExtractLegacyWordFallsBackWhenAntiwordRejectsSmallStream(t *testing.T) {
	text, err := Extract(context.Background(), []byte("legacy-binary"), "application/octet-stream", "legacy-source.doc", Options{
		MaxBytes: 2 << 20,
		Runner:   wordFallbackRunner{},
	})
	if err != nil {
		t.Fatalf("Extract returned error: %v", err)
	}
	if !strings.Contains(string(text), "COBALT-4826") {
		t.Fatalf("text = %q, want fallback control code", text)
	}
}

func TestIsPreviewableIncludesPortableDocumentFormats(t *testing.T) {
	for _, filename := range []string{"scan.pdf", "legacy.doc", "legacy.xls", "modern.docx", "modern.xlsx", "slides.pptx"} {
		if !IsPreviewable("application/octet-stream", filename) {
			t.Errorf("IsPreviewable(%q) = false, want true", filename)
		}
	}
}

func TestExtractModernOfficeWithoutRuntimePrograms(t *testing.T) {
	cases := []struct {
		filename string
		files    map[string]string
		want     string
	}{
		{filename: "modern.docx", files: map[string]string{"word/document.xml": `<w:document xmlns:w="w"><w:p><w:r><w:t>DOCX-CODE-101</w:t></w:r></w:p></w:document>`}, want: "DOCX-CODE-101"},
		{filename: "modern.xlsx", files: map[string]string{"xl/sharedStrings.xml": `<sst><si><t>XLSX-CODE-202</t></si></sst>`, "xl/worksheets/sheet1.xml": `<worksheet><sheetData><row><c t="s"><v>0</v></c></row></sheetData></worksheet>`}, want: "XLSX-CODE-202"},
		{filename: "slides.pptx", files: map[string]string{"ppt/slides/slide1.xml": `<p:sld xmlns:p="p" xmlns:a="a"><a:t>PPTX-CODE-303</a:t></p:sld>`}, want: "PPTX-CODE-303"},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			text, err := Extract(context.Background(), officeZip(t, tc.files), "application/octet-stream", tc.filename, Options{MaxBytes: 2 << 20})
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if !strings.Contains(string(text), tc.want) {
				t.Fatalf("text = %q, want %s", text, tc.want)
			}
		})
	}
}

func TestExtractPreviewIntoUsesSharedExtractor(t *testing.T) {
	original := DefaultRunner
	DefaultRunner = &fakeRunner{}
	defer func() { DefaultRunner = original }()

	body := []byte("legacy-binary")
	if previewErr := ExtractPreviewInto(context.Background(), &body, "application/octet-stream", "legacy-source.doc", 2<<20); previewErr != nil {
		t.Fatalf("ExtractPreviewInto returned error: %v", previewErr)
	}
	if !strings.Contains(string(body), "COBALT-4826") {
		t.Fatalf("body = %q, want converted control code", body)
	}
}

func officeZip(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
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

func TestRealServerProgramsReadControlledFixtures(t *testing.T) {
	fixtureDir := os.Getenv("MULTICA_ATTACHMENT_FIXTURE_DIR")
	if fixtureDir == "" {
		t.Skip("set MULTICA_ATTACHMENT_FIXTURE_DIR to run server-program integration")
	}
	cases := []struct {
		filename string
		want     string
	}{
		{filename: "scanned-ocr-test.pdf", want: "ORANGE-7319"},
		{filename: "legacy-source.doc", want: "COBALT-4826"},
		{filename: "legacy-source.xls", want: "VIOLET-2058"},
	}
	for _, tc := range cases {
		t.Run(tc.filename, func(t *testing.T) {
			body, err := os.ReadFile(filepath.Join(fixtureDir, tc.filename))
			if err != nil {
				t.Fatal(err)
			}
			text, err := Extract(context.Background(), body, "application/octet-stream", tc.filename, Options{MaxBytes: 2 << 20})
			if err != nil {
				t.Fatalf("Extract returned error: %v", err)
			}
			if !strings.Contains(string(text), tc.want) {
				t.Fatalf("text = %q, want %s", text, tc.want)
			}
		})
	}
}
