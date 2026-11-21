package infoflow

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ---- test doubles ----

type mediaFakeStorage struct {
	mu      sync.Mutex
	uploads map[string]mediaFakeUpload
}

type mediaFakeUpload struct {
	data        []byte
	contentType string
	filename    string
}

func newMediaFakeStorage() *mediaFakeStorage {
	return &mediaFakeStorage{uploads: map[string]mediaFakeUpload{}}
}

func (f *mediaFakeStorage) Upload(_ context.Context, key string, data []byte, contentType, filename string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.uploads[key] = mediaFakeUpload{data: append([]byte(nil), data...), contentType: contentType, filename: filename}
	return f.ObjectURL(key), nil
}

func (f *mediaFakeStorage) ObjectURL(key string) string { return "https://cdn.example/" + key }

func (f *mediaFakeStorage) uploaded(key string) (mediaFakeUpload, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	got, ok := f.uploads[key]
	return got, ok
}

type mediaFakeLedger struct {
	mu      sync.Mutex
	records []engine.RecordPendingMediaObjectParams
	refuse  map[string]bool
	err     error
}

func (f *mediaFakeLedger) RecordPendingMediaObject(_ context.Context, p engine.RecordPendingMediaObjectParams) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.records = append(f.records, p)
	if f.err != nil {
		return false, f.err
	}
	if f.refuse[p.StorageKey] {
		// The reconciler owns this key; the resolver must not resurrect it.
		return false, nil
	}
	return true, nil
}

func (f *mediaFakeLedger) keys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, len(f.records))
	for _, r := range f.records {
		out = append(out, r.StorageKey)
	}
	return out
}

// mediaFakeDownloader answers by reference, recording what it was asked for.
type mediaFakeDownloader struct {
	mu       sync.Mutex
	requests []string
	byRef    map[string]mediaPayload
	errs     map[string]error
}

func (f *mediaFakeDownloader) Fetch(_ context.Context, _ sendTarget, _ string, d mediaDescriptor) (mediaPayload, error) {
	ref := ""
	if d.FID != "" {
		ref = d.FID
	} else if d.ObjectKey != "" {
		ref = d.ObjectKey
	} else {
		ref = d.Ref
	}
	f.mu.Lock()
	f.requests = append(f.requests, ref)
	f.mu.Unlock()
	if err := f.errs[ref]; err != nil {
		return mediaPayload{}, err
	}
	got, ok := f.byRef[ref]
	if !ok {
		return mediaPayload{}, errors.New("unknown reference")
	}
	return got, nil
}

// mediaFixture builds an installation, a chat message id and an inbound message
// carrying the given descriptors.
func mediaFixture(t *testing.T, box interface {
	Seal([]byte) ([]byte, error)
}, descs ...mediaDescriptor) (engine.ResolvedInstallation, pgtype.UUID, channel.InboundMessage) {
	t.Helper()
	sealed, err := box.Seal([]byte("app-secret"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	cfg, err := json.Marshal(installConfig{
		AppID:              "app-key",
		AppSecretEncrypted: base64.StdEncoding.EncodeToString(sealed),
	})
	if err != nil {
		t.Fatalf("marshal config: %v", err)
	}
	var ws, instID, messageID pgtype.UUID
	ws.Bytes[0], instID.Bytes[0], messageID.Bytes[0] = 0xAB, 0xCD, 0xEF
	ws.Valid, instID.Valid, messageID.Valid = true, true, true
	raw, err := json.Marshal(rawEvent{AppID: "app-key", MessageID: "if-msg", ChatID: "u:user-1", Media: descs})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	inst := engine.ResolvedInstallation{
		ID:          instID,
		WorkspaceID: ws,
		Platform:    db.ChannelInstallation{Config: cfg},
	}
	lines := make([]string, 0, len(descs))
	for _, d := range descs {
		lines = append(lines, mediaPlaceholderLine(d))
	}
	return inst, messageID, channel.InboundMessage{
		MessageID: "if-msg",
		Type:      channel.MsgTypeImage,
		Text:      strings.Join(lines, "\n"),
		Raw:       raw,
	}
}

// ---- HasMedia ----

func TestMediaResolver_HasMediaIsPure(t *testing.T) {
	box := testBox(t)
	resolver := NewMediaResolver(box.Open, nil, newMediaFakeStorage(), &mediaFakeLedger{}, nil)
	_, _, msg := mediaFixture(t, box, mediaDescriptor{Kind: channel.MsgTypeImage, FID: "fid-1"})
	if !resolver.HasMedia(msg) {
		t.Fatal("HasMedia = false for a message carrying a descriptor")
	}
	// A text-only message, and a broken envelope, must both report false rather
	// than panic: HasMedia runs synchronously on the ACK path.
	if resolver.HasMedia(channel.InboundMessage{Text: "hi"}) {
		t.Error("HasMedia = true for a message with no envelope")
	}
	if resolver.HasMedia(channel.InboundMessage{Raw: []byte("not json")}) {
		t.Error("HasMedia = true for an undecodable envelope")
	}
}

func TestMediaResolver_ResolvesEachAttachment(t *testing.T) {
	box := testBox(t)
	store := newMediaFakeStorage()
	ledger := &mediaFakeLedger{}
	downloader := &mediaFakeDownloader{byRef: map[string]mediaPayload{
		"fid-1": {Content: []byte("png-bytes"), ContentType: "image/png", Filename: "shot.png"},
		"fid-2": {Content: []byte("file-bytes"), ContentType: "text/plain", Filename: "trace.log"},
	}}
	resolver := NewMediaResolver(box.Open, func(credentials) (mediaDownloader, error) {
		return downloader, nil
	}, store, ledger, nil).(*mediaResolver)

	inst, messageID, msg := mediaFixture(t, box,
		mediaDescriptor{Kind: channel.MsgTypeImage, FID: "fid-1"},
		mediaDescriptor{Kind: channel.MsgTypeFile, FID: "fid-2"},
	)
	got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 2 {
		t.Fatalf("MediaRefs = %+v, want two", got.MediaRefs)
	}
	if got.MediaRefs[0].Type != channel.MsgTypeImage || got.MediaRefs[1].Type != channel.MsgTypeFile {
		t.Errorf("MediaRefs types = %q/%q", got.MediaRefs[0].Type, got.MediaRefs[1].Type)
	}
	if got.MediaRefs[0].Filename != "shot.png" || got.MediaRefs[0].MimeType != "image/png" {
		t.Errorf("MediaRefs[0] = %+v", got.MediaRefs[0])
	}
	if got.MediaRefs[0].SizeBytes != int64(len("png-bytes")) {
		t.Errorf("SizeBytes = %d", got.MediaRefs[0].SizeBytes)
	}
	if got.MediaRefs[0].StorageURL != "https://cdn.example/"+got.MediaRefs[0].StorageKey {
		t.Errorf("StorageURL is not the object URL for the key: %+v", got.MediaRefs[0])
	}
	// The intent ledger must have a row per attachment, written before the bytes
	// ever existed.
	if keys := ledger.keys(); len(keys) != 2 {
		t.Errorf("ledger keys = %v, want two", keys)
	}
	if keys := ledger.keys(); keys[0] == keys[1] {
		t.Error("both attachments share one storage key")
	}
	for i, ref := range got.MediaRefs {
		if _, ok := store.uploaded(ref.StorageKey); !ok {
			t.Errorf("attachment %d was not uploaded under its own key", i)
		}
	}
}

func TestMediaResolver_OneFailureDoesNotStopTheOthers(t *testing.T) {
	box := testBox(t)
	store := newMediaFakeStorage()
	ledger := &mediaFakeLedger{}
	downloader := &mediaFakeDownloader{
		byRef: map[string]mediaPayload{
			"fid-ok": {Content: []byte("data"), ContentType: "image/png"},
		},
		errs: map[string]error{"fid-bad": errors.New("platform says 404")},
	}
	resolver := NewMediaResolver(box.Open, func(credentials) (mediaDownloader, error) {
		return downloader, nil
	}, store, ledger, nil).(*mediaResolver)

	inst, messageID, msg := mediaFixture(t, box,
		mediaDescriptor{Kind: channel.MsgTypeFile, FID: "fid-bad", Filename: "gone.png"},
		mediaDescriptor{Kind: channel.MsgTypeImage, FID: "fid-ok", Filename: "ok.png"},
	)
	got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 1 {
		t.Fatalf("MediaRefs = %+v, want the one attachment that landed", got.MediaRefs)
	}
	if got.MediaRefs[0].Filename != "ok.png" {
		t.Errorf("MediaRefs[0] = %+v", got.MediaRefs[0])
	}
	// The failed one keeps its placeholder text: the agent must see that
	// something was sent and did not arrive.
	if !strings.Contains(got.Text, "[File: gone.png]") {
		t.Errorf("Text = %q, want the failed attachment's placeholder kept", got.Text)
	}
}

func TestMediaResolver_SkipsAKeyTheReconcilerOwns(t *testing.T) {
	box := testBox(t)
	store := newMediaFakeStorage()
	downloader := &mediaFakeDownloader{byRef: map[string]mediaPayload{
		"fid-1": {Content: []byte("x"), ContentType: "image/png"},
	}}
	// The first key is refused, so the resolver must never fetch or upload for
	// it: resurrecting a reconciler-owned row is exactly what the ledger exists
	// to prevent.
	ledger := &mediaFakeLedger{}
	probe := NewMediaResolver(box.Open, func(credentials) (mediaDownloader, error) {
		return downloader, nil
	}, store, ledger, nil).(*mediaResolver)
	inst, messageID, msg := mediaFixture(t, box, mediaDescriptor{Kind: channel.MsgTypeImage, FID: "fid-1"})
	firstKey := mediaObjectKey(inst, messageID, "if-msg", 0, channel.MsgTypeImage)
	ledger.refuse = map[string]bool{firstKey: true}

	got := probe.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Fatalf("MediaRefs = %+v, want none", got.MediaRefs)
	}
	downloader.mu.Lock()
	requests := len(downloader.requests)
	downloader.mu.Unlock()
	if requests != 0 {
		t.Errorf("downloader was called %d times for a reconciler-owned key", requests)
	}
}

func TestMediaResolver_DegradesWithoutStorageOrLedger(t *testing.T) {
	box := testBox(t)
	downloader := &mediaFakeDownloader{byRef: map[string]mediaPayload{
		"fid-1": {Content: []byte("x"), ContentType: "image/png"},
	}}
	cases := []struct {
		name   string
		store  mediaStorage
		ledger engine.MediaIntentLedger
	}{
		{"no storage", nil, &mediaFakeLedger{}},
		{"no ledger", newMediaFakeStorage(), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver := NewMediaResolver(box.Open, func(credentials) (mediaDownloader, error) {
				return downloader, nil
			}, tc.store, tc.ledger, nil).(*mediaResolver)
			inst, messageID, msg := mediaFixture(t, box, mediaDescriptor{Kind: channel.MsgTypeImage, FID: "fid-1"})
			got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
			if len(got.MediaRefs) != 0 {
				t.Errorf("MediaRefs = %+v, want none", got.MediaRefs)
			}
			if got.Text == "" {
				t.Error("the placeholder text was dropped along with the media")
			}
		})
	}
}

func TestMediaResolver_OverLimitMessageIsLeftAlone(t *testing.T) {
	box := testBox(t)
	descs := make([]mediaDescriptor, 0, maxInboundMediaPerMessage+1)
	for i := 0; i <= maxInboundMediaPerMessage; i++ {
		descs = append(descs, mediaDescriptor{Kind: channel.MsgTypeImage, FID: "fid"})
	}
	resolver := NewMediaResolver(box.Open, func(credentials) (mediaDownloader, error) {
		return &mediaFakeDownloader{}, nil
	}, newMediaFakeStorage(), &mediaFakeLedger{}, nil).(*mediaResolver)
	inst, messageID, msg := mediaFixture(t, box, descs...)
	got := resolver.ResolveMedia(context.Background(), inst, engine.ResolvedIdentity{}, pgtype.UUID{}, messageID, msg)
	if len(got.MediaRefs) != 0 {
		t.Errorf("MediaRefs = %+v, want none for an over-limit message", got.MediaRefs)
	}
}

func TestMediaResolver_GroupNameIsInTheObjectKey(t *testing.T) {
	// The object key is derived from the CHAT message, so a re-ingest of the same
	// platform message cannot run into the first ingest's ledger row.
	box := testBox(t)
	resolver := NewMediaResolver(box.Open, nil, newMediaFakeStorage(), &mediaFakeLedger{}, nil).(*mediaResolver)
	inst, messageID, _ := mediaFixture(t, box)
	k1 := mediaObjectKey(inst, messageID, "if-msg", 0, channel.MsgTypeImage)
	k2 := mediaObjectKey(inst, messageID, "if-msg", 1, channel.MsgTypeImage)
	if k1 == k2 {
		t.Error("two attachments in one message share a key")
	}
	if !strings.HasPrefix(k1, "workspaces/") || !strings.Contains(k1, "/infoflow/") {
		t.Errorf("key = %q, want a workspace-scoped infoflow key", k1)
	}
	if resolver == nil {
		t.Fatal("resolver is nil")
	}
}

func TestMediaResolver_MediaFetchTimeoutBudget(t *testing.T) {
	// The whole adapter budget must stay below the Router's 45-second media
	// deadline; if someone raises the fetch timeout, this is the tripwire.
	if mediaFetchTimeout >= 45*1e9 {
		t.Errorf("mediaFetchTimeout = %v, must stay under the router's 45s deadline", mediaFetchTimeout)
	}
	if maxInboundMediaBytes <= 0 || maxInboundMediaPerMessage <= 0 {
		t.Error("media budgets must be positive")
	}
}
