package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

type fakeMediaStorage struct {
	mu      sync.Mutex
	uploads map[string][]byte
	names   map[string]string
	types   map[string]string
}

func newFakeMediaStorage() *fakeMediaStorage {
	return &fakeMediaStorage{uploads: map[string][]byte{}, names: map[string]string{}, types: map[string]string{}}
}

func (f *fakeMediaStorage) Upload(_ context.Context, key string, data []byte, contentType string, filename string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[key] = append([]byte(nil), data...)
	f.names[key] = filename
	f.types[key] = contentType
	return f.ObjectURL(key), nil
}

func (f *fakeMediaStorage) ObjectURL(key string) string { return "https://cdn.example/" + key }

type fakeMediaLedger struct {
	mu      sync.Mutex
	records []engine.RecordPendingMediaObjectParams
	refuse  bool
}

func (f *fakeMediaLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, p)
	return !f.refuse, nil
}

type slackMediaTestEnv struct {
	resolver *slackMediaResolver
	store    *fakeMediaStorage
	ledger   *fakeMediaLedger
	host     *httptest.Server
	// authHeader records the Authorization header of the last download.
	authHeader string
}

// newSlackMediaTestEnv serves files at /files/<id>. The resolver's host
// allowlist is widened to the httptest host — production keeps isSlackFileHost.
func newSlackMediaTestEnv(t *testing.T, files map[string][]byte, contentType string) *slackMediaTestEnv {
	t.Helper()
	env := &slackMediaTestEnv{store: newFakeMediaStorage(), ledger: &fakeMediaLedger{}}
	env.host = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.authHeader = r.Header.Get("Authorization")
		data, ok := files[strings.TrimPrefix(r.URL.Path, "/files/")]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		_, _ = w.Write(data)
	}))
	t.Cleanup(env.host.Close)
	env.resolver = NewMediaResolver(nil, env.store, env.ledger, nil).(*slackMediaResolver)
	env.resolver.fetch.Transport = env.host.Client().Transport
	env.resolver.allowedHost = func(string) bool { return true }
	return env
}

func slackMediaFixture(t *testing.T, files ...slackRawFile) (engine.ResolvedInstallation, pgtype.UUID, channel.InboundMessage) {
	t.Helper()
	cfg, _ := json.Marshal(installConfig{
		AppID:  "A1",
		TeamID: "T1",
		// nil Decrypter treats the base64-decoded bytes as the plaintext token.
		BotTokenEncrypted: base64.StdEncoding.EncodeToString([]byte("xoxb-test-token")),
	})
	var ws, instID, chatMessageID pgtype.UUID
	ws.Bytes[0], instID.Bytes[0], chatMessageID.Bytes[0] = 0xAB, 0xCD, 0xEF
	ws.Valid, instID.Valid, chatMessageID.Valid = true, true, true
	raw, _ := json.Marshal(slackRawEvent{TeamID: "T1", APIAppID: "A1", EventType: "message", Files: files})
	inst := engine.ResolvedInstallation{
		ID: instID, WorkspaceID: ws,
		Platform: db.ChannelInstallation{Config: cfg},
	}
	return inst, chatMessageID, channel.InboundMessage{MessageID: "1700000000.000100", Type: channel.MsgTypeText, Text: "see attached", Raw: raw}
}

func (env *slackMediaTestEnv) fileURL(id string) string { return env.host.URL + "/files/" + id }

func TestSlackMediaResolver_HasMedia(t *testing.T) {
	env := newSlackMediaTestEnv(t, nil, "")
	_, _, withFiles := slackMediaFixture(t, slackRawFile{ID: "F1", DownloadURL: "https://files.slack.com/f"})
	if !env.resolver.HasMedia(withFiles) {
		t.Error("HasMedia = false for a message with files")
	}
	_, _, without := slackMediaFixture(t)
	if env.resolver.HasMedia(without) {
		t.Error("HasMedia = true for a message without files")
	}
}

func TestSlackMediaResolver_HappyPath(t *testing.T) {
	pdf := []byte("%PDF-1.4 fake body")
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": pdf}, "application/pdf")
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "report.pdf", Mimetype: "application/pdf", Size: int64(len(pdf)), DownloadURL: env.fileURL("F1"),
	})

	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)

	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want 1", len(got.MediaRefs))
	}
	ref := got.MediaRefs[0]
	if ref.Type != channel.MsgTypeFile {
		t.Errorf("Type = %q, want file", ref.Type)
	}
	if ref.Filename != "report.pdf" || ref.MimeType != "application/pdf" || ref.SizeBytes != int64(len(pdf)) {
		t.Errorf("ref = %+v", ref)
	}
	if len(env.ledger.records) != 1 || env.ledger.records[0].StorageKey != ref.StorageKey {
		t.Errorf("ledger records = %+v", env.ledger.records)
	}
	if string(env.store.uploads[ref.StorageKey]) != string(pdf) {
		t.Error("uploaded bytes do not match the served file")
	}
	if env.authHeader != "Bearer xoxb-test-token" {
		t.Errorf("Authorization = %q, want the bot token", env.authHeader)
	}
	if !strings.Contains(ref.StorageKey, "workspaces/") || !strings.Contains(ref.StorageKey, "/slack/") {
		t.Errorf("StorageKey = %q", ref.StorageKey)
	}
}

func TestSlackMediaResolver_MediaKinds(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, make([]byte, 16)...)
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": png}, "image/png")
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "shot.png", Mimetype: "image/png", DownloadURL: env.fileURL("F1"),
	})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].Type != channel.MsgTypeImage {
		t.Fatalf("MediaRefs = %+v, want one image ref", got.MediaRefs)
	}
}

func TestSlackMediaResolver_BlockedHost(t *testing.T) {
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": []byte("data")}, "")
	env.resolver.allowedHost = isSlackFileHost
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "x.bin", DownloadURL: env.fileURL("F1"),
	})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for a non-Slack host", len(got.MediaRefs))
	}
	if env.authHeader != "" {
		t.Error("bot token was sent to a non-Slack host")
	}
	if len(env.store.uploads) != 0 {
		t.Error("nothing should be uploaded")
	}
}

func TestSlackMediaResolver_HTMLLoginPageIsNotAFile(t *testing.T) {
	html := []byte("<!DOCTYPE html><html><body>Sign in to Slack</body></html>")
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": html}, "text/html; charset=utf-8")
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "report.pdf", Mimetype: "application/pdf", DownloadURL: env.fileURL("F1"),
	})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0 for an HTML auth page", len(got.MediaRefs))
	}
	if len(env.store.uploads) != 0 {
		t.Error("the login page must not be stored as the file")
	}
}

func TestSlackMediaResolver_ReconcilerOwnedKeySkips(t *testing.T) {
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": []byte("data")}, "")
	env.ledger.refuse = true
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "x.bin", DownloadURL: env.fileURL("F1"),
	})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 0 || len(env.store.uploads) != 0 {
		t.Fatal("a reconciler-owned key must not be re-uploaded")
	}
}

func TestSlackMediaResolver_OneFailureDoesNotStopTheRest(t *testing.T) {
	env := newSlackMediaTestEnv(t, map[string][]byte{"F2": []byte("second file")}, "")
	inst, chatMessageID, msg := slackMediaFixture(t,
		slackRawFile{ID: "F1", Name: "missing.bin", DownloadURL: env.fileURL("F1")},
		slackRawFile{ID: "F2", Name: "there.txt", Mimetype: "text/plain", DownloadURL: env.fileURL("F2")},
	)
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 1 || got.MediaRefs[0].Filename != "there.txt" {
		t.Fatalf("MediaRefs = %+v, want only the second file", got.MediaRefs)
	}
}

func TestSlackFileName_Fallbacks(t *testing.T) {
	if got := slackFileName(slackRawFile{ID: "F123", Name: "../../etc/passwd"}, 0, "text/plain"); got != "passwd" {
		t.Errorf("traversal name = %q", got)
	}
	if got := slackFileName(slackRawFile{ID: "F123", Name: ".."}, 1, "image/png"); got != "slack-file-F123-2.png" {
		t.Errorf("dot name fallback = %q", got)
	}
	if got := slackFileName(slackRawFile{ID: "F123"}, 0, "application/pdf"); got != "slack-file-F123-1.pdf" {
		t.Errorf("empty name fallback = %q", got)
	}
}

func TestIsSlackFileHost(t *testing.T) {
	for host, want := range map[string]bool{
		"files.slack.com":     true,
		"slack.com":           true,
		"FILES.SLACK.COM":     true,
		"evil.com":            false,
		"slack.com.evil.com":  false,
		"notslack.com":        false,
		"files.slack.com.br":  false,
		"fakeslack.com":       false,
		"sub.files.slack.com": true,
	} {
		if got := isSlackFileHost(host); got != want {
			t.Errorf("isSlackFileHost(%q) = %v, want %v", host, got, want)
		}
	}
}
