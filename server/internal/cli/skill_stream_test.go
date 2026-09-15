package cli

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type skillStreamTransport func(*http.Request) (*http.Response, error)

func (f skillStreamTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type skillCountingReader struct {
	reads  int
	source io.Reader
}

func (r *skillCountingReader) Read(p []byte) (int, error) { r.reads++; return r.source.Read(p) }

func TestImportSkillFileStreamsIntoTransport(t *testing.T) {
	source := &skillCountingReader{source: strings.NewReader("archive content")}
	client := NewAPIClient("https://example.test", "", "token")
	client.HTTPClient = &http.Client{Transport: skillStreamTransport(func(r *http.Request) (*http.Response, error) {
		if source.reads != 0 {
			t.Fatal("archive was buffered before HTTP transport")
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		if r.MultipartForm != nil {
			defer r.MultipartForm.RemoveAll()
		}
		data, err := io.ReadAll(file)
		if err != nil || string(data) != "archive content" || header.Filename != "test.skill" || r.FormValue("on_conflict") != "rename" {
			t.Fatalf("invalid multipart payload: %q %v", data, err)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(`{"status":"created"}`)), Header: make(http.Header)}, nil
	})}
	var result map[string]any
	if err := client.ImportSkillFile(context.Background(), source, "test.skill", "rename", &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "created" {
		t.Fatal(result)
	}
}
