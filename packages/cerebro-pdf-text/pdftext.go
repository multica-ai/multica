package pdftext

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"

	"github.com/ledongthuc/pdf"
)

var (
	ErrNoExtractableText = errors.New("PDF contains no extractable text")
	ErrTextTooLarge      = errors.New("extracted PDF text too large")
	NewReader            = pdf.NewReader
)

type Preview struct {
	Handled    bool
	Body       []byte
	StatusCode int
	Message    string
	Err        error
}

type PreviewError struct {
	StatusCode int
	Message    string
	Err        error
}

func IsPreviewable(contentType, filename string) bool {
	ct := strings.ToLower(strings.TrimSpace(contentType))
	if idx := strings.Index(ct, ";"); idx >= 0 {
		ct = strings.TrimSpace(ct[:idx])
	}
	return ct == "application/pdf" || strings.ToLower(path.Ext(filename)) == ".pdf"
}

func ExtractPreviewInto(body *[]byte, contentType, filename string, maxBytes int64) *PreviewError {
	preview := ExtractPreview(*body, contentType, filename, maxBytes)
	if !preview.Handled {
		return nil
	}
	*body = preview.Body
	if preview.Err == nil {
		return nil
	}
	return &PreviewError{StatusCode: preview.StatusCode, Message: preview.Message, Err: preview.Err}
}

func ExtractPreview(body []byte, contentType, filename string, maxBytes int64) Preview {
	if !IsPreviewable(contentType, filename) {
		return Preview{Body: body}
	}
	text, err := Extract(body, maxBytes)
	if err == nil {
		return Preview{Handled: true, Body: text}
	}
	switch {
	case errors.Is(err, ErrNoExtractableText):
		return Preview{Handled: true, StatusCode: http.StatusUnsupportedMediaType, Message: "PDF contains no extractable text; OCR is not supported in this version", Err: err}
	case errors.Is(err, ErrTextTooLarge):
		return Preview{Handled: true, StatusCode: http.StatusRequestEntityTooLarge, Message: "extracted PDF text too large for inline preview", Err: err}
	default:
		return Preview{Handled: true, StatusCode: http.StatusUnsupportedMediaType, Message: "PDF could not be parsed", Err: err}
	}
}

func Extract(body []byte, maxBytes int64) (text []byte, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("PDF parser panic: %v", recovered)
			text = nil
		}
	}()

	reader, err := NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	textReader, err := reader.GetPlainText()
	if err != nil {
		return nil, err
	}
	text, err = io.ReadAll(io.LimitReader(textReader, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(text) > int(maxBytes) {
		return nil, ErrTextTooLarge
	}
	if strings.TrimSpace(string(text)) == "" {
		return nil, ErrNoExtractableText
	}
	return text, nil
}
