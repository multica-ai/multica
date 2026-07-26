package lark

import (
	"bytes"
	"context"
	"testing"

	"github.com/multica-ai/multica/server/internal/storage"
)

type fakeMessageResourceDownloader struct {
	resource MessageResource
	called   bool
	msgID    string
	key      string
	kind     string
}

func (f *fakeMessageResourceDownloader) DownloadMessageResource(_ context.Context, _ InstallationCredentials, messageID, fileKey, resourceType string) (MessageResource, error) {
	f.called = true
	f.msgID = messageID
	f.key = fileKey
	f.kind = resourceType
	return f.resource, nil
}

type fakeInboundMediaCredentials struct{}

func (fakeInboundMediaCredentials) DecryptAppSecret(Installation) (string, error) {
	return "secret", nil
}

type fakeInboundMediaStorage struct {
	storage.Storage
	key         string
	data        []byte
	contentType string
	filename    string
}

func (f *fakeInboundMediaStorage) Upload(_ context.Context, key string, data []byte, contentType, filename string) (string, error) {
	f.key = key
	f.data = append([]byte(nil), data...)
	f.contentType = contentType
	f.filename = filename
	return "https://storage.example/" + key, nil
}

func TestInboundMediaService_IngestImage(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\nimage-body")
	downloader := &fakeMessageResourceDownloader{resource: MessageResource{
		Data:        png,
		ContentType: "application/octet-stream",
	}}
	objectStorage := &fakeInboundMediaStorage{}
	service, err := NewInboundMediaService(
		downloader,
		fakeInboundMediaCredentials{},
		objectStorage,
		InboundMediaServiceConfig{},
	)
	if err != nil {
		t.Fatalf("NewInboundMediaService: %v", err)
	}
	inst := Installation{ID: binderUUID(1), AppID: "cli_1", Region: "feishu"}
	ref, err := service.IngestImage(context.Background(), inst, binderUUID(2), "om_1", `{"image_key":"img_1"}`)
	if err != nil {
		t.Fatalf("IngestImage: %v", err)
	}
	if !downloader.called || downloader.msgID != "om_1" || downloader.key != "img_1" || downloader.kind != "image" {
		t.Errorf("download call = %+v", downloader)
	}
	if !bytes.Equal(objectStorage.data, png) {
		t.Errorf("uploaded data = %q", objectStorage.data)
	}
	if objectStorage.contentType != "image/png" || objectStorage.filename != "feishu-image.png" {
		t.Errorf("upload metadata = content_type %q filename %q", objectStorage.contentType, objectStorage.filename)
	}
	if ref.MimeType != "image/png" || ref.Filename != "feishu-image.png" ||
		ref.SizeBytes != int64(len(png)) || ref.URL == "" || ref.StorageKey != objectStorage.key {
		t.Errorf("media ref = %+v", ref)
	}
}

func TestInboundImageKeys_PostExtractsAndDeduplicatesImages(t *testing.T) {
	keys, err := inboundImageKeys("post", `{
		"title": "",
		"content": [
			[
				{"tag":"img","image_key":"img_1"},
				{"tag":"text","text":"图里有什么内容"}
			],
			[
				{"tag":"img","image_key":"img_2"},
				{"tag":"img","image_key":"img_1"}
			]
		]
	}`)
	if err != nil {
		t.Fatalf("inboundImageKeys: %v", err)
	}
	if len(keys) != 2 || keys[0] != "img_1" || keys[1] != "img_2" {
		t.Fatalf("keys = %v, want [img_1 img_2]", keys)
	}
}

func TestInboundMediaService_RejectsMissingImageKey(t *testing.T) {
	downloader := &fakeMessageResourceDownloader{}
	service, err := NewInboundMediaService(
		downloader,
		fakeInboundMediaCredentials{},
		&fakeInboundMediaStorage{},
		InboundMediaServiceConfig{},
	)
	if err != nil {
		t.Fatalf("NewInboundMediaService: %v", err)
	}
	if _, err := service.IngestImage(context.Background(), Installation{}, binderUUID(2), "om_1", `{}`); err == nil {
		t.Fatal("expected missing image_key error")
	}
	if downloader.called {
		t.Fatal("downloader must not run for malformed image content")
	}
}

func TestInboundMediaService_RejectsSpoofedImageContentType(t *testing.T) {
	downloader := &fakeMessageResourceDownloader{resource: MessageResource{
		Data:        []byte("<script>alert('xss')</script>"),
		ContentType: "image/png",
	}}
	objectStorage := &fakeInboundMediaStorage{}
	service, err := NewInboundMediaService(
		downloader,
		fakeInboundMediaCredentials{},
		objectStorage,
		InboundMediaServiceConfig{},
	)
	if err != nil {
		t.Fatalf("NewInboundMediaService: %v", err)
	}
	inst := Installation{ID: binderUUID(1), AppID: "cli_1", Region: "feishu"}
	_, err = service.IngestImage(context.Background(), inst, binderUUID(2), "om_1", `{"image_key":"img_1"}`)
	if err == nil {
		t.Fatal("expected spoofed image content type error")
	}
	if objectStorage.key != "" {
		t.Fatal("spoofed image must not be uploaded")
	}
}
