package infoflow

// outbound_media_test.go — what the last hop does with a file.
//
// Two things are worth pinning and both are about a wrong message being worse
// than a slow one: an attachment must not be able to delay or lose the reply
// that carried it, and an upload must not be able to produce a link that
// silently rewrites itself.

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/dolfly/infoflow/bot/im"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeStore is an in-memory object store: attachment rows carry URLs shaped
// like the ones storage hands out, and KeyFromURL reduces them back.
type fakeStore struct {
	objects map[string][]byte // key -> bytes
	reads   int
}

func newFakeStore() *fakeStore { return &fakeStore{objects: map[string][]byte{}} }

func (f *fakeStore) put(key string, data []byte) { f.objects[key] = data }

func (f *fakeStore) KeyFromURL(rawURL string) string {
	const prefix = "memory://"
	if strings.HasPrefix(rawURL, prefix) {
		return strings.TrimPrefix(rawURL, prefix)
	}
	return ""
}

func (f *fakeStore) GetReader(_ context.Context, key string) (io.ReadCloser, error) {
	data, ok := f.objects[key]
	if !ok {
		return nil, errors.New("no such object")
	}
	f.reads++
	return io.NopCloser(bytes.NewReader(data)), nil
}

// recordingSender records what the delivery path would have sent.
type recordingSender struct {
	uploads []im.UploadFileOptions
	sent    []string // "<kind>:<content>" in order
	failOn  string   // a substring of the content that makes send* fail
}

func (r *recordingSender) upload(_ context.Context, _ []byte, name string) (im.UploadFileResult, error) {
	r.uploads = append(r.uploads, im.UploadFileOptions{
		FileName:          name,
		ExpirationSeconds: outboundUploadExpirationSeconds,
	})
	return im.UploadFileResult{
		ObjectKey: "sdk-uploads/" + name,
		URL:       "https://bos.example/presigned/" + name,
	}, nil
}

func (r *recordingSender) sendImage(_ context.Context, target sendTarget, objectKey string) (channel.SendResult, error) {
	return r.record("image", objectKey, target)
}

func (r *recordingSender) sendMarkdown(_ context.Context, target sendTarget, content string) (channel.SendResult, error) {
	return r.record("md", content, target)
}

func (r *recordingSender) sendText(_ context.Context, target sendTarget, text string) (channel.SendResult, error) {
	return r.record("text", text, target)
}

func (r *recordingSender) record(kind, content string, target sendTarget) (channel.SendResult, error) {
	r.sent = append(r.sent, kind+":"+target.ChatID+":"+content)
	if r.failOn != "" && strings.Contains(content, r.failOn) {
		return channel.SendResult{}, errors.New("simulated send failure")
	}
	return channel.SendResult{MessageID: "1"}, nil
}

// attachmentQueries is a minimal outboundQueries: only the attachment lookup
// matters here, and the rest is present so the interface is satisfied honestly.
type attachmentQueries struct {
	rows []db.Attachment
	err  error
}

func (a attachmentQueries) ListChatInputMessages(context.Context, pgtype.UUID) ([]db.ChatMessage, error) {
	return nil, errors.New("not used")
}

func (a attachmentQueries) GetAgentTask(context.Context, pgtype.UUID) (db.AgentTaskQueue, error) {
	return db.AgentTaskQueue{}, errors.New("not used")
}

func (a attachmentQueries) TaskHasChannelIngestedMessages(context.Context, pgtype.UUID) (bool, error) {
	return false, errors.New("not used")
}

func (a attachmentQueries) GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error) {
	return db.ChannelTaskDelivery{}, errors.New("not used")
}

func (a attachmentQueries) GetChannelInstallation(context.Context, db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	return db.ChannelInstallation{}, errors.New("not used")
}

func (a attachmentQueries) ListAttachmentsByChatMessage(context.Context, db.ListAttachmentsByChatMessageParams) ([]db.Attachment, error) {
	return a.rows, a.err
}

// attachment builds one bound attachment row.
func attachment(t *testing.T, id, filename, contentType, url string) db.Attachment {
	t.Helper()
	return db.Attachment{ID: mustUUID(t, id), Filename: filename, ContentType: contentType, Url: url}
}

// TestSendAttachment_ImageGoesAsAnImage pins the one shape InfoFlow supports
// natively: an image is a real image message, keyed by the BOS object key the
// upload returned.
func TestSendAttachment_ImageGoesAsAnImage(t *testing.T) {
	store := newFakeStore()
	store.put("do60/image.png", []byte("\x89PNG\r\n\x1a\n"))
	o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(store))
	sender := &recordingSender{}

	row := attachment(t,
		"11111111-1111-1111-1111-111111111111",
		"diagram.png",
		"image/png",
		"memory://do60/image.png",
	)
	if err := o.sendAttachment(context.Background(), sender, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, row); err != nil {
		t.Fatalf("sendAttachment: %v", err)
	}

	if len(sender.sent) != 1 || !strings.HasPrefix(sender.sent[0], "image:") {
		t.Fatalf("sent = %v, want exactly one image message", sender.sent)
	}
	if got := sender.sent[0]; got != "image:u1:sdk-uploads/diagram.png" {
		t.Fatalf("sent = %q, want the uploaded object key", got)
	}
	if got := sender.uploads[0].FileName; got != "diagram.png" {
		t.Fatalf("upload name = %q, want diagram.png", got)
	}
	if got := sender.uploads[0].ExpirationSeconds; got != outboundUploadExpirationSeconds {
		t.Fatalf("expiration = %d, want %d (the platform maximum, so the link outlives the chat)", got, outboundUploadExpirationSeconds)
	}
}

// TestSendAttachment_FileGoesAsALink pins the shape the platform forces: there
// is no file message, so a document is delivered as a Markdown link to the
// uploaded object.
func TestSendAttachment_FileGoesAsALink(t *testing.T) {
	store := newFakeStore()
	store.put("do60/report.pdf", []byte("%PDF-1.7"))
	o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(store))
	sender := &recordingSender{}

	row := attachment(t,
		"11111111-1111-1111-1111-111111111111",
		"report.pdf",
		"application/pdf",
		"memory://do60/report.pdf",
	)
	if err := o.sendAttachment(context.Background(), sender, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, row); err != nil {
		t.Fatalf("sendAttachment: %v", err)
	}

	if len(sender.sent) != 1 {
		t.Fatalf("sent = %v, want exactly one message", sender.sent)
	}
	want := "md:u1:[report.pdf](https://bos.example/presigned/report.pdf)"
	if sender.sent[0] != want {
		t.Fatalf("sent = %q, want %q", sender.sent[0], want)
	}
}

// TestSendAttachment_GroupUsesTheGroupEnvelope covers the other half of the
// chat: a group target has no "touser", and a non-numeric group id must be
// refused before the SDK is asked to talk to it.
func TestSendAttachment_GroupUsesTheGroupEnvelope(t *testing.T) {
	store := newFakeStore()
	store.put("g/1.png", []byte("png"))
	o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(store))

	group := sendTarget{ChatType: channel.ChatTypeGroup, ChatID: "987654"}
	if err := o.sendAttachment(context.Background(), &recordingSender{}, group, attachment(t,
		"11111111-1111-1111-1111-111111111111", "chart.png", "image/png", "memory://g/1.png",
	)); err != nil {
		t.Fatalf("sendAttachment(group): %v", err)
	}

	// A malformed group id never reaches the platform: the adapter refuses it
	// locally, so a bad binding key cannot turn into a request.
	if _, err := groupIDInt64("g:987654"); err == nil {
		t.Fatalf("groupIDInt64(%q) = nil error, want a refusal", "g:987654")
	}
	if _, err := groupIDInt64("0"); err == nil {
		t.Fatalf("groupIDInt64(%q) = nil error, want a refusal", "0")
	}
	if _, err := groupIDInt64("987654"); err != nil {
		t.Fatalf("groupIDInt64(%q) = %v, want the parsed id", "987654", err)
	}
}

// TestReadObject_RefusesUnreadableObjects covers the three ways reading can
// fail before any byte is sent: no storage wired, a URL that is not one of ours,
// and an object that is larger than the cap.
func TestReadObject_RefusesUnreadableObjects(t *testing.T) {
	t.Run("no storage wired", func(t *testing.T) {
		o := NewOutbound(attachmentQueries{}, nil, nil)
		if _, err := o.readObject(context.Background(), "memory://k"); err == nil {
			t.Fatal("readObject without storage = nil, want an error")
		}
	})
	t.Run("foreign url", func(t *testing.T) {
		o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(newFakeStore()))
		if _, err := o.readObject(context.Background(), "https://elsewhere.example/k"); err == nil {
			t.Fatal("readObject(foreign url) = nil, want an error")
		}
	})
	t.Run("missing object", func(t *testing.T) {
		o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(newFakeStore()))
		if _, err := o.readObject(context.Background(), "memory://nope"); err == nil {
			t.Fatal("readObject(missing) = nil, want an error")
		}
	})
	t.Run("over the cap", func(t *testing.T) {
		store := newFakeStore()
		store.put("big", bytes.Repeat([]byte("x"), maxOutboundBytes+1))
		o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(store))
		_, err := o.readObject(context.Background(), "memory://big")
		if err == nil || !strings.Contains(err.Error(), "larger than") {
			t.Fatalf("readObject(over cap) = %v, want a size error", err)
		}
	})
}

// TestDeliverAttachments_SaysWhenOneFails covers the degradation contract: a
// file that cannot be delivered must not be silent. The user is told, in one
// line, rather than left assuming it arrived.
func TestDeliverAttachments_SaysWhenOneFails(t *testing.T) {
	store := newFakeStore()
	store.put("a.png", []byte("a"))
	store.put("b.png", []byte("b"))
	q := attachmentQueries{rows: []db.Attachment{
		attachment(t, "11111111-1111-1111-1111-111111111111", "a.png", "image/png", "memory://a.png"),
		attachment(t, "22222222-2222-2222-2222-222222222222", "b.png", "image/png", "memory://b.png"),
	}}
	o := NewOutbound(q, nil, nil, WithAttachments(store))
	// The first send fails; the second still goes, and the notice follows.
	sender := &recordingSender{failOn: "a.png"}
	target := sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}
	o.deliverAttachments(context.Background(), sender, target, pgtype.UUID{}, mustUUID(t, "33333333-3333-3333-3333-333333333333"), pgtype.UUID{})

	if len(sender.sent) != 3 {
		t.Fatalf("sent = %v, want image, image and notice", sender.sent)
	}
	if last := sender.sent[len(sender.sent)-1]; !strings.HasPrefix(last, "text:") || !strings.Contains(last, "could not be delivered") {
		t.Fatalf("last message = %q, want the delivery notice", last)
	}
}

// TestDeliverAttachments_LookupFailureStaysQuiet pins that a query failure is a
// log line, not a message: the user cannot act on it, and a second sentence
// about files when the first never mentioned them is noise.
func TestDeliverAttachments_LookupFailureStaysQuiet(t *testing.T) {
	q := attachmentQueries{err: errors.New("db down")}
	o := NewOutbound(q, nil, nil, WithAttachments(newFakeStore()))
	sender := &recordingSender{}
	o.deliverAttachments(context.Background(), sender, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{})
	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v, want nothing", sender.sent)
	}
}

// TestMayCarryAttachments pins the admission test: only a completed turn with a
// message id can carry files, and only when storage is wired.
func TestMayCarryAttachments(t *testing.T) {
	const msgID = "44444444-4444-4444-4444-444444444444"
	withStorage := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(newFakeStore()))
	withoutStorage := NewOutbound(attachmentQueries{}, nil, nil)

	cases := []struct {
		name string
		o    *Outbound
		e    events.Event
		want bool
	}{
		{"done with a message id", withStorage, events.Event{Type: "chat:done", Payload: protocol.ChatDonePayload{MessageID: msgID}}, true},
		{"done without a message id", withStorage, events.Event{Type: "chat:done", Payload: map[string]any{"content": "hi"}}, false},
		{"a failure is never a file", withStorage, events.Event{Type: "task:failed", Payload: map[string]any{"message_id": msgID}}, false},
		{"no storage means no files", withoutStorage, events.Event{Type: "chat:done", Payload: map[string]any{"message_id": msgID}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.o.mayCarryAttachments(tc.e); got != tc.want {
				t.Fatalf("mayCarryAttachments() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestOutboundMediaName pins the sanitizing the upload path depends on: the
// name reaches a wire that wants a bare segment, and a name with no extension
// inherits one from the content type so the link says what it is.
func TestOutboundMediaName(t *testing.T) {
	tests := []struct {
		name        string
		filename    string
		contentType string
		want        string
	}{
		{"keeps a plain name", "report.pdf", "application/pdf", "report.pdf"},
		{"reduces a path to a segment", "../../etc/passwd", "text/plain", ".._.._etc_passwd"},
		{"strips backslashes", `dir\file.txt`, "text/plain", "dir_file.txt"},
		{"trims a trailing newline", "notes.txt\r\n", "text/plain", "notes.txt"},
		{"falls back when empty", "", "image/png", "attachment.png"},
		{"keeps a name that has an extension", "weird.name.bin", "application/octet-stream", "weird.name.bin"},
		{"gives a nameless file a format hint", "x", "application/pdf", "x.pdf"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := outboundMediaName(tt.filename, tt.contentType); got != tt.want {
				t.Fatalf("outboundMediaName(%q, %q) = %q, want %q", tt.filename, tt.contentType, got, tt.want)
			}
		})
	}
}

// TestIsImageAttachment pins the decision, including the octet-stream case where
// the extension is the only clue left.
func TestIsImageAttachment(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		filename    string
		want        bool
	}{
		{"declared image", "image/png", "a.bin", true},
		{"declared with parameters", "IMAGE/JPEG; charset=binary", "a.bin", true},
		{"octet-stream but a png name", "application/octet-stream", "a.png", true},
		{"pdf is not an image", "application/pdf", "a.pdf", false},
		{"nothing to go on", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isImageAttachment(tt.contentType, tt.filename); got != tt.want {
				t.Fatalf("isImageAttachment(%q, %q) = %v, want %v", tt.contentType, tt.filename, got, tt.want)
			}
		})
	}
}

// TestFileLinkMarkdown_EscapesLabel pins that a filename cannot rewrite the link
// it is the label of. A bracket in a filename is not exotic, and the consequence
// of not escaping it is a link pointing somewhere the user was not promised.
func TestFileLinkMarkdown_EscapesLabel(t *testing.T) {
	got := fileLinkMarkdown("a[b]c\\d.pdf", "  https://bos.example/x  ")
	want := `[a\[b\]c\\d.pdf](https://bos.example/x)`
	if got != want {
		t.Fatalf("fileLinkMarkdown() = %q, want %q", got, want)
	}
}

// TestChatDoneMessageID covers both payload shapes; the map form is what a Redis
// round trip produces for a second replica.
func TestChatDoneMessageID(t *testing.T) {
	const id = "55555555-5555-5555-5555-555555555555"
	if got := chatDoneMessageID(protocol.ChatDonePayload{MessageID: id}); got != id {
		t.Fatalf("typed payload = %q, want %q", got, id)
	}
	if got := chatDoneMessageID(map[string]any{"message_id": id}); got != id {
		t.Fatalf("map payload = %q, want %q", got, id)
	}
	if got := chatDoneMessageID(map[string]any{"message_id": 42}); got != "" {
		t.Fatalf("non-string message id = %q, want empty", got)
	}
	if got := chatDoneMessageID(nil); got != "" {
		t.Fatalf("nil payload = %q, want empty", got)
	}
}

// TestSpawnAttachmentDelivery_DropsWhenFull pins that admission never blocks:
// this runs on the publish path, and a blocked bus delays every other listener
// in the process.
func TestSpawnAttachmentDelivery_DropsWhenFull(t *testing.T) {
	o := NewOutbound(attachmentQueries{}, nil, nil, WithAttachments(newFakeStore()))
	sender := &recordingSender{}
	o.senderFor = func(credentials) (mediaSender, error) { return sender, nil }

	// Fill the semaphore; the next delivery must be dropped, not queued.
	for i := 0; i < maxConcurrentAttachmentDeliveries; i++ {
		attachmentSlots <- struct{}{}
	}
	t.Cleanup(func() {
		for i := 0; i < maxConcurrentAttachmentDeliveries; i++ {
			<-attachmentSlots
		}
	})

	o.spawnAttachmentDelivery(credentials{}, sendTarget{ChatType: channel.ChatTypeP2P, ChatID: "u1"}, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{})
	o.attachmentWG.Wait()
	if len(sender.sent) != 0 {
		t.Fatalf("sent = %v, want nothing (the delivery was not admitted)", sender.sent)
	}
}
