package handler

import (
	"bytes"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type uploadZeroReader struct{}

func (uploadZeroReader) Read(p []byte) (int, error) {
	clear(p)
	return len(p), nil
}

func TestParseUploadFormSizeLimits(t *testing.T) {
	for _, tc := range []struct {
		name         string
		size         int64
		metadataSize int
		wantLimit    int64
	}{
		{name: "above Next proxy default", size: 11 << 20},
		{name: "below file limit", size: maxUploadSize - 1},
		{name: "exact file limit with metadata", size: maxUploadSize, metadataSize: 1024},
		{name: "one byte above file limit", size: maxUploadSize + 1, wantLimit: maxUploadSize},
		{name: "request body limit", size: maxUploadSize, metadataSize: 2 << 20, wantLimit: maxUploadBodySize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var envelope bytes.Buffer
			writer := multipart.NewWriter(&envelope)
			if _, err := writer.CreateFormFile("file", "attachment.bin"); err != nil {
				t.Fatal(err)
			}
			prefixLen := envelope.Len()
			if err := writer.WriteField("description", strings.Repeat("m", tc.metadataSize)); err != nil {
				t.Fatal(err)
			}
			if err := writer.Close(); err != nil {
				t.Fatal(err)
			}
			body := io.MultiReader(bytes.NewReader(envelope.Bytes()[:prefixLen]), io.LimitReader(uploadZeroReader{}, tc.size), bytes.NewReader(envelope.Bytes()[prefixLen:]))
			req := httptest.NewRequest(http.MethodPost, "/api/upload-file", body)
			req.Header.Set("Content-Type", writer.FormDataContentType())
			err := parseUploadForm(httptest.NewRecorder(), req)
			if req.MultipartForm != nil {
				defer req.MultipartForm.RemoveAll()
			}
			if tc.wantLimit != 0 {
				var limitErr *http.MaxBytesError
				if !errors.As(err, &limitErr) || limitErr.Limit != tc.wantLimit {
					t.Fatalf("error = %v, want MaxBytesError with limit %d", err, tc.wantLimit)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			file, header, err := req.FormFile("file")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if header.Size != tc.size {
				t.Fatalf("received %d bytes, want %d", header.Size, tc.size)
			}
			if got := req.FormValue("description"); len(got) != tc.metadataSize {
				t.Fatalf("received %d metadata bytes, want %d", len(got), tc.metadataSize)
			}
		})
	}
}

func TestParseUploadFormMalformed(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/api/upload-file", strings.NewReader("broken multipart"))
	req.Header.Set("Content-Type", "multipart/form-data; boundary=test")
	err := parseUploadForm(httptest.NewRecorder(), req)
	var limitErr *http.MaxBytesError
	if err == nil || errors.As(err, &limitErr) {
		t.Fatalf("expected a malformed multipart error, got %v", err)
	}
}
