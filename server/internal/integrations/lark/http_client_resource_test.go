package lark

import (
	"bytes"
	"context"
	"net/http"
	"testing"
)

func TestHTTPAPIClient_DownloadMessageResource(t *testing.T) {
	f := newLarkFake(t)
	f.stubToken("token-resource", 3600)
	wantData := []byte("\x89PNG\r\n\x1a\nimage-body")
	f.mux.HandleFunc("/open-apis/im/v1/messages/om_1/resources/img_1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("method = %s, want GET", r.Method)
		}
		if got := r.URL.Query().Get("type"); got != "image" {
			t.Errorf("type query = %q, want image", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer token-resource" {
			t.Errorf("authorization = %q", got)
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Content-Disposition", `attachment; filename="screen.png"`)
		_, _ = w.Write(wantData)
	})

	client := NewHTTPAPIClient(HTTPClientConfig{BaseURL: f.URL()})
	downloader, ok := client.(MessageResourceDownloader)
	if !ok {
		t.Fatalf("client %T does not implement MessageResourceDownloader", client)
	}
	got, err := downloader.DownloadMessageResource(context.Background(), testCreds(), "om_1", "img_1", "image")
	if err != nil {
		t.Fatalf("DownloadMessageResource: %v", err)
	}
	if !bytes.Equal(got.Data, wantData) {
		t.Errorf("data = %q, want %q", got.Data, wantData)
	}
	if got.ContentType != "image/png" || got.Filename != "screen.png" {
		t.Errorf("metadata = %+v", got)
	}
}

func TestHTTPAPIClient_DownloadMessageResourceRejectsUnsupportedType(t *testing.T) {
	client := NewHTTPAPIClient(HTTPClientConfig{})
	downloader := client.(MessageResourceDownloader)
	if _, err := downloader.DownloadMessageResource(context.Background(), testCreds(), "om_1", "img_1", "sticker"); err == nil {
		t.Fatal("expected unsupported resource type error")
	}
}
