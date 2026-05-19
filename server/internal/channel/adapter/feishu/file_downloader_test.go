package feishu_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/channel/adapter/feishu"
)

// fakeFileDownloader is a test double for the FileDownloader interface.
type fakeFileDownloader struct {
	imageData          []byte
	imageErr           error
	fileData           []byte
	fileName           string
	fileErr            error
	downloadImageCalls []struct{ MessageID, FileKey string }
	downloadFileCalls  []struct{ MessageID, FileKey string }
}

func (f *fakeFileDownloader) DownloadImage(ctx context.Context, messageID, fileKey string) ([]byte, error) {
	f.downloadImageCalls = append(f.downloadImageCalls, struct{ MessageID, FileKey string }{messageID, fileKey})
	return f.imageData, f.imageErr
}

func (f *fakeFileDownloader) DownloadFile(ctx context.Context, messageID, fileKey string) ([]byte, string, error) {
	f.downloadFileCalls = append(f.downloadFileCalls, struct{ MessageID, FileKey string }{messageID, fileKey})
	return f.fileData, f.fileName, f.fileErr
}

// Compile-time assertion that fakeFileDownloader satisfies the interface.
var _ feishu.FileDownloader = (*fakeFileDownloader)(nil)

// ---------------------------------------------------------------------------
// TC-downloader-1: DownloadImage happy path
// ---------------------------------------------------------------------------

func TestFileDownloader_DownloadImage_HappyPath(t *testing.T) {
	t.Parallel()

	fake := &fakeFileDownloader{imageData: []byte("fake-image-bytes")}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, err := fake.DownloadImage(ctx, "msg_1", "img_abc123")
	if err != nil {
		t.Fatalf("DownloadImage: %v", err)
	}
	if string(data) != "fake-image-bytes" {
		t.Errorf("data = %q, want %q", string(data), "fake-image-bytes")
	}
	if len(fake.downloadImageCalls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.downloadImageCalls))
	}
	if fake.downloadImageCalls[0].MessageID != "msg_1" {
		t.Errorf("messageID = %q, want msg_1", fake.downloadImageCalls[0].MessageID)
	}
	if fake.downloadImageCalls[0].FileKey != "img_abc123" {
		t.Errorf("fileKey = %q, want img_abc123", fake.downloadImageCalls[0].FileKey)
	}
}

// ---------------------------------------------------------------------------
// TC-downloader-2: DownloadFile happy path
// ---------------------------------------------------------------------------

func TestFileDownloader_DownloadFile_HappyPath(t *testing.T) {
	t.Parallel()

	fake := &fakeFileDownloader{fileData: []byte("fake-pdf-bytes"), fileName: "design.pdf"}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	data, name, err := fake.DownloadFile(ctx, "msg_1", "file_xyz789")
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(data) != "fake-pdf-bytes" {
		t.Errorf("data = %q, want %q", string(data), "fake-pdf-bytes")
	}
	if name != "design.pdf" {
		t.Errorf("name = %q, want %q", name, "design.pdf")
	}
	if len(fake.downloadFileCalls) != 1 {
		t.Fatalf("calls = %d, want 1", len(fake.downloadFileCalls))
	}
}

// ---------------------------------------------------------------------------
// TC-downloader-3: DownloadImage error propagation
// ---------------------------------------------------------------------------

func TestFileDownloader_DownloadImage_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("network timeout")
	fake := &fakeFileDownloader{imageErr: wantErr}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, err := fake.DownloadImage(ctx, "msg_1", "img_abc123")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}

// ---------------------------------------------------------------------------
// TC-downloader-4: DownloadFile error propagation
// ---------------------------------------------------------------------------

func TestFileDownloader_DownloadFile_Error(t *testing.T) {
	t.Parallel()

	wantErr := errors.New("not found")
	fake := &fakeFileDownloader{fileErr: wantErr}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	_, _, err := fake.DownloadFile(ctx, "msg_1", "file_xyz789")
	if !errors.Is(err, wantErr) {
		t.Fatalf("err = %v, want %v", err, wantErr)
	}
}
