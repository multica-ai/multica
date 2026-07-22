package lark

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

type fakeResourceDownloader struct {
	results []DownloadedMessageResource
	errs    []error
	calls   int
}

func (f *fakeResourceDownloader) DownloadMessageResource(_ context.Context, _ InstallationCredentials, _, _ string, _ MessageResourceType) (DownloadedMessageResource, error) {
	i := f.calls
	f.calls++
	if i < len(f.errs) && f.errs[i] != nil {
		return DownloadedMessageResource{}, f.errs[i]
	}
	return f.results[i], nil
}

type fakeCredentialsResolver struct{}

func (fakeCredentialsResolver) DecryptAppSecret(Installation) (string, error) { return "secret", nil }

type fakeMediaStorage struct {
	keys    []string
	deleted []string
	bytes   [][]byte
}

func (s *fakeMediaStorage) Upload(ctx context.Context, key string, data []byte, contentType, filename string) (string, error) {
	return s.UploadFromReader(ctx, key, bytes.NewReader(data), int64(len(data)), contentType, filename)
}
func (s *fakeMediaStorage) UploadFromReader(_ context.Context, key string, reader io.Reader, _ int64, _, _ string) (string, error) {
	b, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}
	s.keys = append(s.keys, key)
	s.bytes = append(s.bytes, b)
	return "/uploads/" + key, nil
}
func (s *fakeMediaStorage) Delete(_ context.Context, key string) { s.deleted = append(s.deleted, key) }
func (s *fakeMediaStorage) DeleteKeys(ctx context.Context, keys []string) {
	for _, key := range keys {
		s.Delete(ctx, key)
	}
}
func (*fakeMediaStorage) KeyFromURL(raw string) string { return strings.TrimPrefix(raw, "/uploads/") }
func (*fakeMediaStorage) CdnDomain() string            { return "" }
func (*fakeMediaStorage) GetReader(context.Context, string) (io.ReadCloser, error) {
	return nil, errors.New("not implemented")
}

func mediaTestUUID(b byte) pgtype.UUID {
	var id pgtype.UUID
	id.Bytes[0] = b
	id.Valid = true
	return id
}

func TestSanitizeInboundFilename(t *testing.T) {
	t.Parallel()
	got := sanitizeInboundFilename("../../evil\x00\nreport.pdf", "file", "application/pdf")
	if got != "evilreport.pdf" {
		t.Fatalf("sanitized filename = %q", got)
	}
	if strings.Contains(got, "/") || strings.Contains(got, "\\") || len(got) > maxInboundFilenameBytes {
		t.Fatalf("unsafe sanitized filename = %q", got)
	}
}

func TestFeishuMediaResolverPreservesSuccessAndDegradesPermanentFailure(t *testing.T) {
	t.Parallel()
	png := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 600)...)
	downloader := &fakeResourceDownloader{
		results: []DownloadedMessageResource{{
			Body: io.NopCloser(bytes.NewReader(png)), ContentType: "image/png", Filename: "../../evidence.png", ContentLength: int64(len(png)),
		}},
		errs: []error{nil, &messageResourceError{category: "upstream HTTP 403"}},
	}
	store := &fakeMediaStorage{}
	resolver := NewFeishuMediaResolver(downloader, fakeCredentialsResolver{}, store, nil)
	lm := InboundMessage{MessageID: "om_message", Body: "Please inspect", Resources: []MessageResourceRef{
		{Type: "image", Key: "img_secret"},
		{Type: "file", Key: "file_secret", Filename: "private.pdf"},
	}}
	raw, _ := json.Marshal(lm)
	msg := channel.InboundMessage{MessageID: lm.MessageID, Text: lm.Body, Raw: raw}
	resolved, cleanup, err := resolver.Resolve(context.Background(), engine.ResolvedInstallation{
		WorkspaceID: mediaTestUUID(2), Platform: Installation{AppID: "cli_app"},
	}, engine.ResolvedIdentity{UserID: mediaTestUUID(7)}, msg)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(resolved.MediaRefs) != 1 || resolved.MediaRefs[0].Filename != "evidence.png" || resolved.MediaRefs[0].MimeType != "image/png" {
		t.Fatalf("MediaRefs = %#v", resolved.MediaRefs)
	}
	if !strings.HasPrefix(resolved.MediaRefs[0].StorageKey, "workspaces/") || !strings.Contains(resolved.MediaRefs[0].StorageKey, "/channel-inbound/") {
		t.Fatalf("storage key = %q", resolved.MediaRefs[0].StorageKey)
	}
	if !strings.Contains(resolved.Text, "1 attachment could not be downloaded") {
		t.Fatalf("safe warning missing from text: %q", resolved.Text)
	}
	if strings.Contains(resolved.Text, "file_secret") {
		t.Fatalf("resource key leaked into text: %q", resolved.Text)
	}
	cleanup(context.Background())
	if len(store.deleted) != 1 || store.deleted[0] != resolved.MediaRefs[0].StorageKey {
		t.Fatalf("cleanup deleted = %#v", store.deleted)
	}
}

func TestFeishuMediaResolverRejectsSpoofedImage(t *testing.T) {
	t.Parallel()
	downloader := &fakeResourceDownloader{results: []DownloadedMessageResource{{
		Body: io.NopCloser(strings.NewReader("plain text pretending to be png")), ContentType: "image/png", Filename: "fake.png", ContentLength: 31,
	}}}
	store := &fakeMediaStorage{}
	resolver := NewFeishuMediaResolver(downloader, fakeCredentialsResolver{}, store, nil)
	lm := InboundMessage{MessageID: "om", Resources: []MessageResourceRef{{Type: "image", Key: "img"}}}
	raw, _ := json.Marshal(lm)
	resolved, _, err := resolver.Resolve(context.Background(), engine.ResolvedInstallation{
		WorkspaceID: mediaTestUUID(2), Platform: Installation{AppID: "cli"},
	}, engine.ResolvedIdentity{}, channel.InboundMessage{MessageID: "om", Text: "[Image attachment]", Raw: raw})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.MediaRefs) != 0 || len(store.keys) != 0 || !strings.Contains(resolved.Text, "1 attachment could not be downloaded") {
		t.Fatalf("spoof result=%#v stored=%#v", resolved, store.keys)
	}
}
