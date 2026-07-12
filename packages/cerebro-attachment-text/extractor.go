package attachmenttext

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	pdftext "github.com/multica-ai/multica/packages/cerebro-pdf-text"
	"github.com/nkiri/xls"
)

var ErrNoExtractableText = pdftext.ErrNoExtractableText

var DefaultRunner CommandRunner = systemRunner{}

const (
	defaultMaxBytes = int64(2 << 20)
	commandTimeout  = 45 * time.Second
)

type CommandRunner interface {
	Run(ctx context.Context, dir, name string, args ...string) ([]byte, error)
}

type Options struct {
	MaxBytes int64
	Runner   CommandRunner
	PDFText  func([]byte, int64) ([]byte, error)
}

type PreviewError struct {
	StatusCode int
	Message    string
	Err        error
}

func (e *PreviewError) Error() string { return e.Message }

type systemRunner struct{}

func (systemRunner) Run(ctx context.Context, dir, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func Extract(ctx context.Context, body []byte, contentType, filename string, opts Options) ([]byte, error) {
	maxBytes := opts.MaxBytes
	if maxBytes <= 0 {
		maxBytes = defaultMaxBytes
	}
	if int64(len(body)) > maxBytes {
		return nil, fmt.Errorf("attachment exceeds %d byte extraction limit", maxBytes)
	}
	runner := opts.Runner
	if runner == nil {
		runner = DefaultRunner
	}
	pdfExtract := opts.PDFText
	if pdfExtract == nil {
		pdfExtract = pdftext.Extract
	}

	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		text, err := pdfExtract(body, maxBytes)
		if err == nil {
			return text, nil
		}
		if !errors.Is(err, pdftext.ErrNoExtractableText) {
			return nil, err
		}
		return extractScannedPDF(ctx, body, runner, maxBytes)
	case ".doc":
		return extractLegacyDoc(ctx, body, filename, runner, maxBytes)
	case ".xls":
		return extractLegacyXLS(body, maxBytes)
	case ".docx":
		return extractDocx(body, maxBytes)
	case ".xlsx":
		return extractXlsx(body, maxBytes)
	case ".pptx":
		return extractPptx(body, maxBytes)
	default:
		return nil, fmt.Errorf("unsupported attachment type %q", normalizedType(contentType, filename))
	}
}

func ExtractPreviewInto(ctx context.Context, body *[]byte, contentType, filename string, maxBytes int64) *PreviewError {
	if !IsPreviewable(contentType, filename) {
		return nil
	}
	text, err := Extract(ctx, *body, contentType, filename, Options{MaxBytes: maxBytes})
	if err == nil {
		*body = text
		return nil
	}
	switch {
	case errors.Is(err, pdftext.ErrTextTooLarge):
		return &PreviewError{StatusCode: http.StatusRequestEntityTooLarge, Message: "extracted attachment text too large for inline preview", Err: err}
	case errors.Is(err, ErrNoExtractableText):
		return &PreviewError{StatusCode: http.StatusUnsupportedMediaType, Message: "attachment contains no extractable text", Err: err}
	default:
		return &PreviewError{StatusCode: http.StatusUnsupportedMediaType, Message: "attachment could not be converted to text", Err: err}
	}
}

func extractDocx(body []byte, maxBytes int64) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("open docx: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == "word/document.xml" {
			return checkedText(xmlElements(readZip(f, maxBytes), "t"), maxBytes)
		}
	}
	return nil, fmt.Errorf("docx document.xml not found")
}

func extractXlsx(body []byte, maxBytes int64) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("open xlsx: %w", err)
	}
	var shared []string
	for _, f := range zr.File {
		if f.Name == "xl/sharedStrings.xml" {
			shared = strings.Split(xmlElements(readZip(f, maxBytes), "t"), "\n")
		}
	}
	var sheets []string
	for _, f := range zr.File {
		if !strings.HasPrefix(f.Name, "xl/worksheets/") || !strings.HasSuffix(f.Name, ".xml") {
			continue
		}
		if text := worksheetText(readZip(f, maxBytes), shared); text != "" {
			sheets = append(sheets, filepath.Base(f.Name)+": "+text)
		}
	}
	sort.Strings(sheets)
	return checkedText(strings.Join(sheets, "\n"), maxBytes)
}

func extractPptx(body []byte, maxBytes int64) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("open pptx: %w", err)
	}
	type slide struct {
		name string
		text string
	}
	var slides []slide
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			if text := xmlElements(readZip(f, maxBytes), "t"); text != "" {
				slides = append(slides, slide{name: filepath.Base(f.Name), text: text})
			}
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].name < slides[j].name })
	parts := make([]string, 0, len(slides))
	for i, s := range slides {
		parts = append(parts, fmt.Sprintf("Slide %d: %s", i+1, s.text))
	}
	return checkedText(strings.Join(parts, "\n"), maxBytes)
}

func readZip(f *zip.File, maxBytes int64) []byte {
	r, err := f.Open()
	if err != nil {
		return nil
	}
	defer r.Close()
	b, _ := io.ReadAll(io.LimitReader(r, maxBytes+1))
	return b
}

func xmlElements(data []byte, element string) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var parts []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		start, ok := tok.(xml.StartElement)
		if !ok || start.Name.Local != element {
			continue
		}
		var value string
		if dec.DecodeElement(&value, &start) == nil && strings.TrimSpace(value) != "" {
			parts = append(parts, strings.TrimSpace(value))
		}
	}
	return strings.Join(parts, "\n")
}

func worksheetText(data []byte, shared []string) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var parts []string
	var cellType string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "c":
				cellType = ""
				for _, attr := range t.Attr {
					if attr.Name.Local == "t" {
						cellType = attr.Value
					}
				}
			case "v", "t":
				var value string
				if dec.DecodeElement(&value, &t) != nil {
					continue
				}
				value = strings.TrimSpace(value)
				if t.Name.Local == "v" && cellType == "s" {
					if index, err := strconv.Atoi(value); err == nil && index >= 0 && index < len(shared) {
						value = shared[index]
					}
				}
				if value != "" {
					parts = append(parts, value)
				}
			}
		}
	}
	return strings.Join(parts, "\n")
}

func checkedText(text string, maxBytes int64) ([]byte, error) {
	body := []byte(strings.TrimSpace(text))
	if len(body) == 0 {
		return nil, ErrNoExtractableText
	}
	if int64(len(body)) > maxBytes {
		return nil, pdftext.ErrTextTooLarge
	}
	return body, nil
}

func IsPreviewable(contentType, filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".pdf", ".doc", ".xls", ".docx", ".xlsx", ".pptx":
		return true
	}
	ct := normalizedType(contentType, filename)
	return ct == "application/pdf" || strings.Contains(ct, "msword") || strings.Contains(ct, "ms-excel") || strings.Contains(ct, "officedocument")
}

func extractScannedPDF(ctx context.Context, body []byte, runner CommandRunner, maxBytes int64) ([]byte, error) {
	dir, err := os.MkdirTemp("", "multica-attachment-ocr-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	input := filepath.Join(dir, "input.pdf")
	if err := os.WriteFile(input, body, 0o600); err != nil {
		return nil, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	if _, err := runner.Run(commandCtx, dir, "pdftoppm", "-png", "-r", "200", "-f", "1", "-l", "20", input, filepath.Join(dir, "page")); err != nil {
		return nil, fmt.Errorf("render scanned PDF: %w", err)
	}
	pages, err := filepath.Glob(filepath.Join(dir, "page-*.png"))
	if err != nil {
		return nil, err
	}
	sort.Strings(pages)
	if len(pages) == 0 {
		return nil, ErrNoExtractableText
	}
	var parts []string
	for _, page := range pages {
		out, err := runner.Run(commandCtx, dir, "tesseract", page, "stdout", "-l", "eng+dan")
		if err != nil {
			return nil, fmt.Errorf("OCR scanned PDF: %w", err)
		}
		if text := strings.TrimSpace(string(out)); text != "" {
			parts = append(parts, text)
		}
	}
	text := []byte(strings.Join(parts, "\n\n"))
	if len(text) == 0 {
		return nil, ErrNoExtractableText
	}
	if int64(len(text)) > maxBytes {
		return nil, pdftext.ErrTextTooLarge
	}
	return text, nil
}

func extractLegacyDoc(ctx context.Context, body []byte, filename string, runner CommandRunner, maxBytes int64) ([]byte, error) {
	dir, err := os.MkdirTemp("", "multica-attachment-office-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	base := strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename))
	input := filepath.Join(dir, base+strings.ToLower(filepath.Ext(filename)))
	if err := os.WriteFile(input, body, 0o600); err != nil {
		return nil, err
	}
	commandCtx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	text, err := runner.Run(commandCtx, dir, "antiword", input)
	if err == nil {
		return checkedText(string(text), maxBytes)
	}
	profile := "-env:UserInstallation=file://" + filepath.ToSlash(filepath.Join(dir, "profile"))
	if _, fallbackErr := runner.Run(commandCtx, dir, "lowriter", profile, "--headless", "--nologo", "--nodefault", "--nolockcheck", "--nofirststartwizard", "--convert-to", "txt:Text", "--outdir", dir, input); fallbackErr != nil {
		return nil, fmt.Errorf("convert legacy Word attachment: antiword: %v; lowriter: %w", err, fallbackErr)
	}
	converted, readErr := os.ReadFile(filepath.Join(dir, base+".txt"))
	if readErr != nil {
		return nil, fmt.Errorf("read converted legacy Word attachment: %w", readErr)
	}
	return checkedText(string(converted), maxBytes)
}

func extractLegacyXLS(body []byte, maxBytes int64) (text []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			text = nil
			err = fmt.Errorf("parse legacy Excel attachment: %v", recovered)
		}
	}()
	workbook, err := xls.Read(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("parse legacy Excel attachment: %w", err)
	}
	var parts []string
	for i := 0; i < workbook.SheetCount(); i++ {
		sheet := workbook.Sheet(i)
		if sheet == nil {
			continue
		}
		parts = append(parts, "Sheet: "+sheet.Name())
		for _, row := range sheet.Strings() {
			parts = append(parts, strings.Join(row, "\t"))
		}
	}
	return checkedText(strings.Join(parts, "\n"), maxBytes)
}

func normalizedType(contentType, filename string) string {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	if ct == "" || ct == "application/octet-stream" {
		return strings.ToLower(filepath.Ext(filename))
	}
	return ct
}
