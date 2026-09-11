package lark

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeMediaAPIClient is the APIClient the patcher is built with, plus the
// four media methods it type-asserts for. Embedding the ordinary fake keeps
// this from drifting when APIClient grows a method.
type fakeMediaAPIClient struct {
	*fakeAPIClient

	mu        sync.Mutex
	images    []UploadImageParams
	files     []UploadFileParams
	imagesOut []SendImageParams
	filesOut  []SendFileParams

	uploadErr error
	sendErr   error
	imageKey  string
	fileKey   string
}

func (f *fakeMediaAPIClient) UploadImage(_ context.Context, p UploadImageParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.images = append(f.images, p)
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	if f.imageKey == "" {
		return "img_fake", nil
	}
	return f.imageKey, nil
}

func (f *fakeMediaAPIClient) UploadFile(_ context.Context, p UploadFileParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.files = append(f.files, p)
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	if f.fileKey == "" {
		return "file_fake", nil
	}
	return f.fileKey, nil
}

func (f *fakeMediaAPIClient) SendImageMessage(_ context.Context, p SendImageParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.imagesOut = append(f.imagesOut, p)
	return "lark_img_msg", f.sendErr
}

func (f *fakeMediaAPIClient) SendFileMessage(_ context.Context, p SendFileParams) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.filesOut = append(f.filesOut, p)
	return "lark_file_msg", f.sendErr
}

// fakeObjectStore is an in-memory mediaObjectStore. URLs are the configured
// prefix plus the key, which is the shape both real implementations use.
type fakeObjectStore struct {
	objects map[string][]byte
	readErr error
}

const fakeObjectPrefix = "https://cdn.example/"

func (s fakeObjectStore) KeyFromURL(rawURL string) string {
	if !strings.HasPrefix(rawURL, fakeObjectPrefix) {
		return ""
	}
	return strings.TrimPrefix(rawURL, fakeObjectPrefix)
}

func (s fakeObjectStore) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	if s.readErr != nil {
		return nil, s.readErr
	}
	data, ok := s.objects[key]
	if !ok {
		return nil, fmt.Errorf("fake store: no object %q", key)
	}
	return io.NopCloser(bytes.NewReader(data)), nil
}

func newMediaTestPatcher(t *testing.T) (*Patcher, *fakePatcherQueries, *fakeMediaAPIClient) {
	t.Helper()
	q := &fakePatcherQueries{
		binding: ChatSessionBinding{
			ChatSessionID:  uuidFromString(t, "cccccccc-cccc-cccc-cccc-cccccccccccc"),
			InstallationID: uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111"),
			ChannelChatID:  "oc_test_chat",
			ChatType:       "p2p",
		},
		installation: Installation{
			ID:                 uuidFromString(t, "1111aaaa-1111-1111-1111-111111111111"),
			WorkspaceID:        uuidFromString(t, "99999999-9999-9999-9999-999999999999"),
			AppID:              "cli_test_app",
			AppSecretEncrypted: []byte("ciphertext"),
			Status:             string(InstallationActive),
			AgentID:            uuidFromString(t, "aaaa1111-aaaa-aaaa-aaaa-aaaaaaaaaaaa"),
		},
		agent: db.Agent{Name: "TestAgent"},
	}
	api := &fakeMediaAPIClient{
		fakeAPIClient: &fakeAPIClient{sendReturn: "lark_card_msg_1", textSendReturn: "lark_text_msg_1"},
	}
	p := NewPatcher(q, fakeCredentials{secret: "shh"}, api, PatcherConfig{
		Logger: newDiscardLogger(),
		Now:    time.Now,
	})
	return p, q, api
}

// pngBytes is a non-empty body with a plausible image header — enough for a
// path that only counts bytes and reads a content type off the row.
var pngBytes = append([]byte{0x89, 'P', 'N', 'G'}, bytes.Repeat([]byte{0}, 32)...)

func TestLarkFileTypeFor(t *testing.T) {
	cases := map[string]string{
		"report.pdf": "pdf",
		"clip.mp4":   "mp4",
		"voice.opus": "opus",
		"legacy.doc": "doc",
		"legacy.xls": "xls",
		"legacy.ppt": "ppt",
		"REPORT.PDF": "pdf",
		// Everything else is `stream`. Deliberately not mapped by family: a
		// wrong specific type is refused by Lark, while `stream` always
		// sends.
		"sheet.xlsx":     "stream",
		"deck.pptx":      "stream",
		"modern.docx":    "stream",
		"archive.tar.gz": "stream",
		"no-extension":   "stream",
	}
	for name, want := range cases {
		if got := larkFileTypeFor(name); got != want {
			t.Errorf("larkFileTypeFor(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestIsImageAttachment(t *testing.T) {
	cases := []struct {
		contentType, filename string
		want                  bool
	}{
		{"image/png", "a.png", true},
		{"image/jpeg; charset=binary", "a.jpg", true},
		// Content type wins when present, even against the extension —
		// the uploader recorded it and Lark's renderer keys off it.
		{"application/octet-stream", "a.png", false},
		// Falls back to the extension only when there is no content type.
		{"", "a.png", true},
		{"", "a.PNG", true},
		{"", "a.pdf", false},
		{"", "", false},
	}
	for _, c := range cases {
		if got := isImageAttachment(c.contentType, c.filename); got != c.want {
			t.Errorf("isImageAttachment(%q, %q) = %v, want %v", c.contentType, c.filename, got, c.want)
		}
	}
}

func TestChatDoneMessageID(t *testing.T) {
	if got := chatDoneMessageID(map[string]any{"message_id": "abc"}); got != "abc" {
		t.Errorf("map payload: got %q", got)
	}
	if got := chatDoneMessageID(map[string]any{}); got != "" {
		t.Errorf("empty map payload: got %q", got)
	}
	if got := chatDoneMessageID("not a payload"); got != "" {
		t.Errorf("unexpected payload type: got %q", got)
	}
}

func imageRow(t *testing.T, id, name string) db.Attachment {
	t.Helper()
	return db.Attachment{
		ID:          uuidFromString(t, id),
		Filename:    name,
		Url:         fakeObjectPrefix + name,
		ContentType: "image/png",
	}
}

func fileRow(t *testing.T, id, name, contentType string) db.Attachment {
	t.Helper()
	return db.Attachment{
		ID:          uuidFromString(t, id),
		Filename:    name,
		Url:         fakeObjectPrefix + name,
		ContentType: contentType,
	}
}

// TestDeliverAttachmentsRoutesByKind pins the one branch in this path: an
// image goes to the image endpoint as `msg_type=image`, anything else goes
// to the file endpoint as `msg_type=file`. Getting this backwards is not a
// cosmetic bug — Lark refuses a body whose bytes disagree with the declared
// kind, so a misrouted file is lost rather than degraded.
func TestDeliverAttachmentsRoutesByKind(t *testing.T) {
	p, q, api := newMediaTestPatcher(t)
	p.SetAttachments(fakeObjectStore{objects: map[string][]byte{
		"chart.png":  pngBytes,
		"report.pdf": []byte("%PDF-1.4 fake"),
	}})
	q.attachments = []db.Attachment{
		imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png"),
		fileRow(t, "bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "report.pdf", "application/pdf"),
	}

	p.deliverAttachments(context.Background(), testCreds(), q.binding, ReplyTarget{},
		q.installation.WorkspaceID, uuidString(uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")))

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.images) != 1 || len(api.imagesOut) != 1 {
		t.Fatalf("expected one image upload + send; uploads=%d sends=%d", len(api.images), len(api.imagesOut))
	}
	if len(api.files) != 1 || len(api.filesOut) != 1 {
		t.Fatalf("expected one file upload + send; uploads=%d sends=%d", len(api.files), len(api.filesOut))
	}
	if got := api.images[0].Filename; got != "chart.png" {
		t.Errorf("image uploaded from wrong row: %q", got)
	}
	if got := api.files[0].FileType; got != "pdf" {
		t.Errorf("pdf should be declared pdf, got %q", got)
	}
	if !bytes.Equal(api.images[0].Data, pngBytes) {
		t.Error("image upload did not carry the object's bytes")
	}
	if api.imagesOut[0].ImageKey != "img_fake" || api.filesOut[0].FileKey != "file_fake" {
		t.Error("send did not use the key the upload returned")
	}
	// No notice on the happy path: a delivery is what the user sees.
	if len(api.textSent) != 0 {
		t.Errorf("happy path must not warn; got %d notices", len(api.textSent))
	}
}

// TestDeliverAttachmentsLookupFailureTellsUser: we could not read what was
// attached, so we do not know whether a file existed. Silence here is what
// leaves a user waiting for something that was never attempted.
func TestDeliverAttachmentsLookupFailureTellsUser(t *testing.T) {
	p, q, api := newMediaTestPatcher(t)
	p.SetAttachments(fakeObjectStore{})
	q.attachmentsErr = fmt.Errorf("boom")

	p.deliverAttachments(context.Background(), testCreds(), q.binding, ReplyTarget{},
		q.installation.WorkspaceID, uuidString(uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")))

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 1 || api.textSent[0].Text != mediaLookupFailedText {
		t.Fatalf("expected the lookup-failure notice; textSent=%+v", api.textSent)
	}
}

// TestDeliverAttachmentsWarnsOnceForManyFailures: files are independent, so
// one bad object must not stop the rest — but the user gets one line, not
// one per file.
func TestDeliverAttachmentsWarnsOnceForManyFailures(t *testing.T) {
	p, q, api := newMediaTestPatcher(t)
	// The store has no objects, so every read fails.
	p.SetAttachments(fakeObjectStore{objects: map[string][]byte{}})
	q.attachments = []db.Attachment{
		imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "one.png"),
		imageRow(t, "bbbb3333-bbbb-bbbb-bbbb-bbbbbbbbbbbb", "two.png"),
	}

	p.deliverAttachments(context.Background(), testCreds(), q.binding, ReplyTarget{},
		q.installation.WorkspaceID, uuidString(uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")))

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.imagesOut) != 0 {
		t.Fatalf("nothing should have been sent; got %d", len(api.imagesOut))
	}
	if len(api.textSent) != 1 || api.textSent[0].Text != mediaSendFailedText {
		t.Fatalf("expected exactly one failure notice; textSent=%+v", api.textSent)
	}
}

// TestDeliverAttachmentsNoOpsWithoutWiring: the stub client does not
// implement the media interface and a deployment may have no storage. Both
// must be quiet no-ops, not lookups or panics.
func TestDeliverAttachmentsNoOpsWithoutWiring(t *testing.T) {
	t.Run("no object store", func(t *testing.T) {
		p, q, api := newMediaTestPatcher(t)
		// SetAttachments deliberately not called.
		q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
		p.deliverAttachmentsAsync(testCreds(), q.binding, ReplyTarget{},
			q.installation.WorkspaceID, map[string]any{"message_id": "dddd4444-dddd-dddd-dddd-dddddddddddd"})
		api.mu.Lock()
		defer api.mu.Unlock()
		if len(api.imagesOut) != 0 || len(api.textSent) != 0 {
			t.Error("no store means no delivery and no notice")
		}
	})

	t.Run("no media client", func(t *testing.T) {
		// The ordinary fakeAPIClient does not implement mediaAPIClient,
		// which is exactly how the production stub behaves.
		p, q, api := newTestPatcher(t)
		p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes}})
		q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
		p.deliverAttachmentsAsync(testCreds(), q.binding, ReplyTarget{},
			q.installation.WorkspaceID, map[string]any{"message_id": "dddd4444-dddd-dddd-dddd-dddddddddddd"})
		api.mu.Lock()
		defer api.mu.Unlock()
		if len(api.textSent) != 0 {
			t.Error("no media client means no delivery and no notice")
		}
	})

	t.Run("no message id", func(t *testing.T) {
		p, q, _ := newMediaTestPatcher(t)
		p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes}})
		q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}
		p.deliverAttachmentsAsync(testCreds(), q.binding, ReplyTarget{},
			q.installation.WorkspaceID, map[string]any{"content": "no message id here"})
		q.mu.Lock()
		defer q.mu.Unlock()
		if len(q.attachmentLookups) != 0 {
			t.Error("a payload with no message id must not trigger a lookup")
		}
	})
}

// TestChatReplySpawnsDeliveryAfterReply is the integration seam: a chat:done
// with a message id must send the answer AND then deliver the files bound to
// it, in that order.
func TestChatReplySpawnsDeliveryAfterReply(t *testing.T) {
	p, q, api := newMediaTestPatcher(t)
	p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes}})

	messageID := uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")
	taskID := uuidFromString(t, "ee777777-ee77-ee77-ee77-eeeeeeeeeeee")
	q.task = db.AgentTaskQueue{ChatInputTaskID: taskID}
	q.taskChannelIngested = true
	q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}

	p.handleEvent(newChatDoneEvent(t, taskID, q.binding.ChatSessionID, messageID, "Here is the chart."))

	// The spawn is asynchronous by design, so poll for its effect rather
	// than sleeping a fixed amount.
	waitFor(t, func() bool {
		api.mu.Lock()
		defer api.mu.Unlock()
		return len(api.imagesOut) == 1
	}, "image delivery")

	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.textSent) != 1 || api.textSent[0].Text != "Here is the chart." {
		t.Fatalf("the answer must still go out exactly once; textSent=%+v", api.textSent)
	}
	if api.imagesOut[0].ImageKey != "img_fake" {
		t.Error("image delivered without the uploaded key")
	}
}

// TestChatReplyDoesNotDeliverWhenReplyFails: the files are the second hop,
// so there is no first hop to follow when the answer itself did not land.
// A file arriving under no answer is worse than a missing file.
func TestChatReplyDoesNotDeliverWhenReplyFails(t *testing.T) {
	p, q, api := newMediaTestPatcher(t)
	p.SetAttachments(fakeObjectStore{objects: map[string][]byte{"chart.png": pngBytes}})
	api.textSendErr = fmt.Errorf("lark is down")

	messageID := uuidFromString(t, "dddd4444-dddd-dddd-dddd-dddddddddddd")
	taskID := uuidFromString(t, "ee888888-ee88-ee88-ee88-eeeeeeeeeeee")
	q.task = db.AgentTaskQueue{ChatInputTaskID: taskID}
	q.taskChannelIngested = true
	q.attachments = []db.Attachment{imageRow(t, "aaaa2222-aaaa-aaaa-aaaa-aaaaaaaaaaaa", "chart.png")}

	p.handleEvent(newChatDoneEvent(t, taskID, q.binding.ChatSessionID, messageID, "plain prose"))

	// Give the (supposedly absent) goroutine a chance to run, so this is
	// not passing merely because nothing was scheduled yet.
	time.Sleep(50 * time.Millisecond)
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.imagesOut) != 0 || len(api.images) != 0 {
		t.Fatalf("no delivery may follow a failed reply; uploads=%d sends=%d", len(api.images), len(api.imagesOut))
	}
}

// waitFor polls cond until it holds or the budget runs out. The delivery
// path is deliberately asynchronous, so a test cannot assert on it without
// waiting for the thing it is asserting on.
func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// newChatDoneEvent builds the bus event a completed chat task publishes. The
// MessageID is what the attachment hop keys off, so every test here sets one.
func newChatDoneEvent(t *testing.T, taskID, sessionID pgtype.UUID, messageID pgtype.UUID, content string) events.Event {
	t.Helper()
	return events.Event{
		Type:          protocol.EventChatDone,
		TaskID:        uuidString(taskID),
		ChatSessionID: uuidString(sessionID),
		Payload: protocol.ChatDonePayload{
			TaskID:        uuidString(taskID),
			ChatSessionID: uuidString(sessionID),
			MessageID:     uuidString(messageID),
			Content:       content,
		},
	}
}
