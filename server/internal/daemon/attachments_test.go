package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestLoadClaudeImageAttachmentsDownloadsOnlyImages(t *testing.T) {
	t.Parallel()

	var sawAttachmentAuth bool
	var sawDownloadAuth bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/attachments/img-1":
			sawAttachmentAuth = r.Header.Get("Authorization") == "Bearer task-token"
			_ = json.NewEncoder(w).Encode(AttachmentInfo{
				ID:          "img-1",
				Filename:    "screen.png",
				DownloadURL: "/files/screen.png",
				ContentType: "image/png",
				SizeBytes:   4,
			})
		case "/files/screen.png":
			sawDownloadAuth = r.Header.Get("Authorization") == "Bearer task-token"
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write([]byte{0x89, 0x50, 0x4e, 0x47})
		default:
			t.Fatalf("unexpected request path %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	client.SetToken("task-token")
	d := &Daemon{client: client}

	images := d.loadClaudeImageAttachments(context.Background(), Task{
		ChatMessageAttachments: []ChatAttachmentMeta{
			{ID: "img-1", Filename: "screen.png", ContentType: "image/png; charset=binary", SizeBytes: 4},
			{ID: "doc-1", Filename: "notes.txt", ContentType: "text/plain", SizeBytes: 12},
		},
	}, slog.Default())

	if len(images) != 1 {
		t.Fatalf("expected one image attachment, got %d", len(images))
	}
	if images[0].ID != "img-1" || images[0].Filename != "screen.png" {
		t.Fatalf("unexpected image metadata: %+v", images[0])
	}
	if images[0].ContentType != "image/png" {
		t.Fatalf("expected normalized image/png content type, got %q", images[0].ContentType)
	}
	if string(images[0].Data) != string([]byte{0x89, 0x50, 0x4e, 0x47}) {
		t.Fatalf("unexpected image bytes: %v", images[0].Data)
	}
	if !sawAttachmentAuth || !sawDownloadAuth {
		t.Fatalf("expected auth on metadata and relative download requests, metadata=%v download=%v", sawAttachmentAuth, sawDownloadAuth)
	}
}

func TestLoadClaudeImageAttachmentsSkipsOversizedBeforeDownload(t *testing.T) {
	t.Parallel()

	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		t.Fatalf("oversized attachment should not be requested")
	}))
	defer srv.Close()

	client := NewClient(srv.URL)
	d := &Daemon{client: client}

	images := d.loadClaudeImageAttachments(context.Background(), Task{
		ChatMessageAttachments: []ChatAttachmentMeta{
			{ID: "large", Filename: "large.jpg", ContentType: "image/jpeg", SizeBytes: maxClaudeImageAttachmentBytes + 1},
		},
	}, slog.Default())

	if len(images) != 0 {
		t.Fatalf("expected no images, got %d", len(images))
	}
	if requests != 0 {
		t.Fatalf("expected no HTTP requests, got %d", requests)
	}
}
