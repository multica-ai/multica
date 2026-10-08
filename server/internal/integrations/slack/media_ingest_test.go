package slack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

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
	// requests counts download attempts that reached the server.
	requests int
}

// newSlackMediaTestEnv serves files at /files/<id>. The resolver's host
// allowlist is widened to the httptest host — production keeps isSlackFileHost.
func newSlackMediaTestEnv(t *testing.T, files map[string][]byte, contentType string) *slackMediaTestEnv {
	t.Helper()
	env := &slackMediaTestEnv{store: newFakeMediaStorage(), ledger: &fakeMediaLedger{}}
	env.host = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.authHeader = r.Header.Get("Authorization")
		env.requests++
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

func TestSlackMediaResolver_HTMLResponseRequiresExplicitDeclaration(t *testing.T) {
	html := []byte("<!DOCTYPE html><html><body>Sign in to Slack</body></html>")
	for _, tt := range []struct {
		name         string
		declaredType string
		wantRefs     int
	}{
		{name: "non-HTML file", declaredType: "application/pdf", wantRefs: 0},
		{name: "missing MIME type", declaredType: "", wantRefs: 0},
		{name: "explicit HTML file is case-insensitive", declaredType: "TEXT/HTML", wantRefs: 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newSlackMediaTestEnv(t, map[string][]byte{"F1": html}, "Text/HTML; charset=utf-8")
			inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
				ID: "F1", Name: "report.html", Mimetype: tt.declaredType, DownloadURL: env.fileURL("F1"),
			})
			got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
			if len(got.MediaRefs) != tt.wantRefs {
				t.Fatalf("MediaRefs = %d, want %d", len(got.MediaRefs), tt.wantRefs)
			}
			if tt.wantRefs == 0 && len(env.store.uploads) != 0 {
				t.Error("the login page must not be stored as the file")
			}
			if tt.wantRefs == 1 && got.MediaRefs[0].MimeType != "text/html" {
				t.Errorf("MimeType = %q, want normalized text/html", got.MediaRefs[0].MimeType)
			}
		})
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

// The redirect policy is the credential boundary: net/http hands the bot token
// to every hop this returns nil for, so each destination shape is pinned here.
func TestSlackMediaResolver_RedirectPolicy(t *testing.T) {
	r := NewMediaResolver(nil, newFakeMediaStorage(), &fakeMediaLedger{}, nil).(*slackMediaResolver)
	initial := mustParseURL(t, "https://files.slack.com/files-pri/T1-F1/report.pdf")
	for _, tt := range []struct {
		name    string
		hop     string
		wantErr bool
	}{
		{name: "same Slack host", hop: "https://files.slack.com/files-pri/T1-F1/report.pdf?t=2", wantErr: false},
		{name: "sibling Slack subdomain", hop: "https://files-origin.slack.com/report.pdf", wantErr: false},
		{name: "Slack apex", hop: "https://slack.com/report.pdf", wantErr: false},
		{name: "off-domain CDN", hop: "https://slack-files2.s3.amazonaws.com/report.pdf?X-Amz-Signature=z", wantErr: true},
		{name: "attacker host", hop: "https://evil.com/report.pdf", wantErr: true},
		{name: "lookalike host", hop: "https://files.slack.com.evil.com/report.pdf", wantErr: true},
		{name: "downgrade to http", hop: "http://files.slack.com/report.pdf", wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			via := []*http.Request{{URL: initial, Header: http.Header{"Authorization": {"Bearer xoxb-test-token"}}}}
			next := &http.Request{URL: mustParseURL(t, tt.hop), Header: http.Header{}}
			err := r.fetch.CheckRedirect(next, via)
			if (err != nil) != tt.wantErr {
				t.Fatalf("CheckRedirect(%q) err = %v, wantErr = %v", tt.hop, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			// net/http drops Authorization on any hop that is not the same
			// host or a subdomain of it; the policy restores it because the
			// host check above already proved the destination is Slack's.
			if got := next.Header.Get("Authorization"); got != "Bearer xoxb-test-token" {
				t.Errorf("Authorization on permitted hop = %q, want the bot token restored", got)
			}
		})
	}
}

func TestSlackMediaResolver_RedirectHonorsHopLimit(t *testing.T) {
	r := NewMediaResolver(nil, newFakeMediaStorage(), &fakeMediaLedger{}, nil).(*slackMediaResolver)
	hop := &http.Request{URL: mustParseURL(t, "https://files.slack.com/x"), Header: http.Header{}}
	via := make([]*http.Request, maxDownloadRedirects)
	for i := range via {
		via[i] = &http.Request{URL: mustParseURL(t, "https://files.slack.com/x"), Header: http.Header{}}
	}
	if err := r.fetch.CheckRedirect(hop, via); err == nil {
		t.Fatalf("redirect %d must be refused", maxDownloadRedirects+1)
	}
}

// A redirect that stays on the same host must still deliver the file: this is
// the path a working Slack download takes when it bounces internally.
func TestSlackMediaResolver_FollowsSameHostRedirect(t *testing.T) {
	pdf := []byte("%PDF-1.4 redirected body")
	var hops int
	var lastAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hops++
		lastAuth = r.Header.Get("Authorization")
		if r.URL.Path == "/first" {
			http.Redirect(w, r, "/second", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		_, _ = w.Write(pdf)
	}))
	defer srv.Close()

	env := newSlackMediaTestEnv(t, nil, "")
	env.resolver.fetch.Transport = srv.Client().Transport
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "report.pdf", Mimetype: "application/pdf", DownloadURL: srv.URL + "/first",
	})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %d, want 1 across a redirect", len(got.MediaRefs))
	}
	if hops != 2 {
		t.Errorf("server hops = %d, want 2", hops)
	}
	if lastAuth != "Bearer xoxb-test-token" {
		t.Errorf("Authorization after redirect = %q, want the bot token", lastAuth)
	}
	if string(env.store.uploads[got.MediaRefs[0].StorageKey]) != string(pdf) {
		t.Error("uploaded bytes do not match the redirected file")
	}
}

func TestSlackMediaResolver_DeclaredSizeLimit(t *testing.T) {
	for _, tt := range []struct {
		name         string
		size         int64
		wantRefs     int
		wantRequests int
	}{
		{name: "at the cap", size: maxInboundFileBytes, wantRefs: 1, wantRequests: 1},
		{name: "one byte over", size: maxInboundFileBytes + 1, wantRefs: 0, wantRequests: 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			env := newSlackMediaTestEnv(t, map[string][]byte{"F1": []byte("small body")}, "text/plain")
			inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
				ID: "F1", Name: "big.txt", Mimetype: "text/plain", Size: tt.size, DownloadURL: env.fileURL("F1"),
			})
			got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
			if len(got.MediaRefs) != tt.wantRefs {
				t.Fatalf("MediaRefs = %d, want %d", len(got.MediaRefs), tt.wantRefs)
			}
			if env.requests != tt.wantRequests {
				t.Errorf("download attempts = %d, want %d", env.requests, tt.wantRequests)
			}
			// An oversized file must not even reserve a key: no transfer, no
			// intent row for the reconciler to settle.
			if tt.wantRefs == 0 && len(env.ledger.records) != 0 {
				t.Errorf("ledger records = %d, want 0 for a file rejected on declared size", len(env.ledger.records))
			}
		})
	}
}

// A file that under-reports its size is still caught by the byte cap on the
// response body.
func TestSlackMediaResolver_BodyExceedingCapIsRejected(t *testing.T) {
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": make([]byte, maxInboundFileBytes+1)}, "application/octet-stream")
	inst, chatMessageID, msg := slackMediaFixture(t, slackRawFile{
		ID: "F1", Name: "liar.bin", Mimetype: "application/octet-stream", Size: 10, DownloadURL: env.fileURL("F1"),
	})
	got := env.resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0", len(got.MediaRefs))
	}
	if len(env.store.uploads) != 0 {
		t.Error("an oversized body must not be stored")
	}
}

func TestFileFetchBudget(t *testing.T) {
	if got := fileFetchBudget(context.Background(), 10); got != fileFetchTimeout {
		t.Errorf("no deadline: budget = %s, want the flat %s", got, fileFetchTimeout)
	}
	// The Router's media deadline shared across a full ten-file message must
	// land well under the flat per-file timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	got := fileFetchBudget(ctx, maxFilesPerMessage)
	if got >= fileFetchTimeout || got < 3*time.Second {
		t.Errorf("ten files under a 45s deadline: budget = %s, want roughly a tenth of it", got)
	}
	// The last file may use whatever is left, capped by the flat timeout.
	if got := fileFetchBudget(ctx, 1); got != fileFetchTimeout {
		t.Errorf("last file: budget = %s, want the flat %s", got, fileFetchTimeout)
	}
}

// Once the Router's deadline has passed every ref is discarded anyway, so the
// resolver must stop rather than keep reserving keys and downloading.
func TestSlackMediaResolver_StopsOnceBudgetIsSpent(t *testing.T) {
	env := newSlackMediaTestEnv(t, map[string][]byte{"F1": []byte("a"), "F2": []byte("b")}, "text/plain")
	inst, chatMessageID, msg := slackMediaFixture(t,
		slackRawFile{ID: "F1", Name: "a.txt", Mimetype: "text/plain", DownloadURL: env.fileURL("F1")},
		slackRawFile{ID: "F2", Name: "b.txt", Mimetype: "text/plain", DownloadURL: env.fileURL("F2")},
	)
	expired, cancel := context.WithCancel(context.Background())
	cancel()
	got := env.resolver.ResolveMedia(expired, inst, engine.ResolvedIdentity{}, pgtype.UUID{}, chatMessageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %d, want 0", len(got.MediaRefs))
	}
	if env.requests != 0 || len(env.ledger.records) != 0 {
		t.Errorf("downloads = %d, ledger records = %d, want 0 and 0", env.requests, len(env.ledger.records))
	}
}

func TestIsFetchableSlackFileURL(t *testing.T) {
	for rawURL, want := range map[string]bool{
		"https://files.slack.com/files-pri/T1-F1/report.pdf": true,
		"https://slack.com/files-pri/T1-F1/report.pdf":       true,
		// Remote "external" files: url_private points at the host that owns them.
		"https://docs.google.com/document/d/abc/edit":  false,
		"https://www.dropbox.com/s/abc/report.pdf":     false,
		"http://files.slack.com/files-pri/T1-F1/x.pdf": false,
		"https://user:pass@files.slack.com/x.pdf":      false,
		"https://files.slack.com.evil.com/x.pdf":       false,
		"":                                             false,
		"not a url":                                    false,
	} {
		if got := isFetchableSlackFileURL(rawURL); got != want {
			t.Errorf("isFetchableSlackFileURL(%q) = %v, want %v", rawURL, got, want)
		}
	}
}

func mustParseURL(t *testing.T, rawURL string) *url.URL {
	t.Helper()
	parsed, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse %q: %v", rawURL, err)
	}
	return parsed
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
