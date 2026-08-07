package wecom

// outbound_media_test.go — the last hop for a file an agent produced.
//
// `multica attachment upload` already bound the file to the assistant message;
// everything downstream of that bind assumed a browser, so a WeCom
// conversation received the words and nothing else. These drive the whole path
// through processEvent, because the bug was never in the upload protocol — it
// was that nothing ever called it.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

const (
	testWorkspaceID = "33333333-3333-3333-3333-333333333333"
	testMessageID   = "44444444-4444-4444-4444-444444444444"
	testSessionID   = "22222222-2222-2222-2222-222222222222"
	testTaskID      = "55555555-5555-5555-5555-555555555555"
)

// fakeObjectStore stands in for the deployment's object storage.
type fakeObjectStore struct {
	key  string
	data []byte
	err  error
}

func (f *fakeObjectStore) KeyFromURL(string) string { return f.key }
func (f *fakeObjectStore) GetReader(context.Context, string) (io.ReadCloser, error) {
	if f.err != nil {
		return nil, f.err
	}
	return io.NopCloser(bytes.NewReader(f.data)), nil
}

// newOutboundWithMedia builds the subscriber over a socket double that can
// answer the upload cmds. spawn runs inline so the assertions observe the
// delivery instead of racing it.
func newOutboundWithMedia(t *testing.T, q outboundQueries, objects mediaObjectStore) (*Outbound, pgtype.UUID, *mediaConn) {
	t.Helper()
	reg := newSendersRegistry()
	instID := mustTestUUID(t)
	conn := newMediaConn()
	reg.set(instID, conn.newSender())
	var opts []OutboundOption
	if objects != nil {
		opts = append(opts, WithAttachments(objects))
	}
	o := NewOutbound(q, reg, slog.Default(), opts...)
	o.spawn = func(f func()) { f() }
	return o, instID, conn
}

// oneAttachmentQueries wires a session that has a binding, an active
// installation, and one file bound to this turn's assistant message.
func oneAttachmentQueries(t *testing.T, row db.Attachment) *fakeOutboundQueries {
	t.Helper()
	q := &fakeOutboundQueries{
		sessionBinding: db.ChannelChatSessionBinding{ChannelChatID: "CHAT_1", ChatType: "group"},
		installation:   db.ChannelInstallation{Status: string(InstallationActive)},
		attachments:    []db.Attachment{row},
		// Asked in the room. A file is delivered on the far side of the origin
		// gate, so every rig here has to say where the question came from —
		// see askedInTheWebUI below for the other answer.
		channelIngested: askedOverWecom(),
	}
	q.fileTask(t, testTaskID)
	return q
}

func chatDoneEvent(content string) events.Event {
	return events.Event{
		ChatSessionID: testSessionID,
		WorkspaceID:   testWorkspaceID,
		Payload: protocol.ChatDonePayload{
			Content:   content,
			MessageID: testMessageID,
			TaskID:    testTaskID,
		},
	}
}

// markdownSends returns the text content of every plain-text push, which is
// how the answer and the failure notice are distinguished from a media frame
// (they all ride cmdSendMsg).
func markdownSends(t *testing.T, conn *mediaConn) []string {
	t.Helper()
	var out []string
	for _, f := range conn.cmdFrames(cmdSendMsg) {
		var body struct {
			MsgType  string `json:"msgtype"`
			Markdown struct {
				Content string `json:"content"`
			} `json:"markdown"`
		}
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode send body: %v", err)
		}
		if body.MsgType == "markdown" {
			out = append(out, body.Markdown.Content)
		}
	}
	return out
}

// mediaSends returns the msgtype + media_id of every media push.
func mediaSends(t *testing.T, conn *mediaConn) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, f := range conn.cmdFrames(cmdSendMsg) {
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode send body: %v", err)
		}
		if body["msgtype"] != "markdown" {
			out = append(out, body)
		}
	}
	return out
}

// The headline. Before this, the answer arrived and the file did not.
func TestProcessEvent_SendsTheAnswerAndThenTheFile(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{
		ID:          mustTestUUID(t),
		Filename:    "q3-forecast.xlsx",
		Url:         "https://cdn.example/obj/abc",
		ContentType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
		SizeBytes:   9,
	})
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "obj/abc", data: []byte("SPREADSHEE")})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	if err := o.processEvent(context.Background(), chatDoneEvent("Here is the forecast.")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}

	if got := markdownSends(t, conn); len(got) != 1 || got[0] != "Here is the forecast." {
		t.Fatalf("text sends = %v, want the agent's answer alone", got)
	}
	// The upload must actually have happened — not just a send frame.
	if n := len(conn.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Fatalf("upload init frames = %d, want 1 — the file was never uploaded", n)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaFinish)); n != 1 {
		t.Fatalf("upload finish frames = %d, want 1", n)
	}
	media := mediaSends(t, conn)
	if len(media) != 1 {
		t.Fatalf("media sends = %d, want 1 — the file never reached the chat", len(media))
	}
	if media[0]["msgtype"] != string(mediaTypeFile) {
		t.Errorf("msgtype = %v, want file", media[0]["msgtype"])
	}
	if media[0]["chatid"] != "CHAT_1" {
		t.Errorf("media chatid = %v, want the chat that asked", media[0]["chatid"])
	}
	if media[0]["chat_type"] != float64(chatTypeGroupInt) {
		t.Errorf("media chat_type = %v, want group", media[0]["chat_type"])
	}
	nested := media[0][string(mediaTypeFile)].(map[string]any)
	if nested["media_id"] != "MEDIA_1" {
		t.Errorf("media_id = %v, want the one finish handed back", nested["media_id"])
	}

	// Order matters: the words are what the user is waiting for, and the file
	// is allowed to be slow. A file ahead of its explanation reads as noise.
	frames := conn.cmdFrames(cmdSendMsg)
	if len(frames) < 2 {
		t.Fatalf("send frames = %d, want the answer and the file", len(frames))
	}
	var first map[string]any
	if err := json.Unmarshal(frames[0].Body, &first); err != nil {
		t.Fatalf("decode first frame: %v", err)
	}
	if first["msgtype"] != "markdown" {
		t.Errorf("first send was %v, want the answer's text ahead of the file", first["msgtype"])
	}
}

// A file is a second way an answer reaches the room, so the origin gate has to
// refuse it on the same terms it refuses the words. A session that started in
// WeCom can be continued from the Multica web UI, and that answer's artifacts
// belong in Multica — pushing them into a group chat puts a file in front of
// everyone in the room, which is the failure the gate exists to prevent.
//
// This is also what keeps the gate ahead of the upload: the upload is a write
// to WeCom, and running it before the gate would leak the file even if the
// send that follows were refused.
func TestProcessEvent_AWebUIAnswerDeliversNeitherWordsNorFile(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{
		ID:          mustTestUUID(t),
		Filename:    "internal-notes.pdf",
		Url:         "https://cdn.example/obj/abc",
		ContentType: "application/pdf",
		SizeBytes:   9,
	})
	q.channelIngested = askedInTheWebUI() // asked in Multica, not over WeCom
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "obj/abc", data: []byte("SECRETS!!!")})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	if err := o.processEvent(context.Background(), chatDoneEvent("Here are the notes.")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}

	if got := markdownSends(t, conn); len(got) != 0 {
		t.Errorf("a web-UI answer was pushed into the WeCom chat: %v", got)
	}
	if got := mediaSends(t, conn); len(got) != 0 {
		t.Errorf("a web-UI answer's file was pushed into the WeCom chat: %v", got)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("upload init frames = %d — the file was uploaded to WeCom before the gate refused the answer", n)
	}
}

// An empty completion with a file bound to it is a real outcome: the agent
// produced an artifact and said nothing about it. Returning early there threw
// the work away.
func TestProcessEvent_EmptyCompletionStillDeliversABoundFile(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{
		ID:          mustTestUUID(t),
		Filename:    "chart.png",
		Url:         "https://cdn.example/obj/png",
		ContentType: "image/png",
	})
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "obj/png", data: []byte("PNGDATA")})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	if err := o.processEvent(context.Background(), chatDoneEvent("")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if got := markdownSends(t, conn); len(got) != 0 {
		t.Errorf("text sends = %v, want none — an empty bubble ahead of the file is noise", got)
	}
	media := mediaSends(t, conn)
	if len(media) != 1 {
		t.Fatalf("media sends = %d, want 1 — the agent's file was thrown away", len(media))
	}
	// A png inside the image ceiling travels as an image, not a file card.
	if media[0]["msgtype"] != string(mediaTypeImage) {
		t.Errorf("msgtype = %v, want image", media[0]["msgtype"])
	}
}

// A deployment with no object storage has nothing to read an attachment out
// of, and must behave exactly as it did before this path existed.
func TestProcessEvent_WithoutObjectStorageDeliversTextOnly(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{ID: mustTestUUID(t), Filename: "x.pdf", Url: "u"})
	o, instID, conn := newOutboundWithMedia(t, q, nil) // no WithAttachments
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	if err := o.processEvent(context.Background(), chatDoneEvent("just words")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if got := markdownSends(t, conn); len(got) != 1 || got[0] != "just words" {
		t.Errorf("text sends = %v, want the answer", got)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("upload init frames = %d, want 0 with no storage configured", n)
	}
	// And with no content and no storage, the turn still ends silently.
	if err := o.processEvent(context.Background(), chatDoneEvent("")); err != nil {
		t.Fatalf("processEvent (empty): %v", err)
	}
	if got := markdownSends(t, conn); len(got) != 1 {
		t.Errorf("text sends after an empty completion = %v, want no new message", got)
	}
}

// The answer is already on screen and may well refer to the file. Silence
// leaves the user looking for something that never arrives.
func TestSendAttachments_TellsTheUserWhenAFileFailed(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{
		ID: mustTestUUID(t), Filename: "big.bin", Url: "https://cdn.example/obj/bin",
	})
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "obj/bin", data: []byte("DATA")})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID
	conn.refuse[cmdUploadMediaInit] = 40058 // the server will not take the file

	if err := o.processEvent(context.Background(), chatDoneEvent("See the attached dump.")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	got := markdownSends(t, conn)
	if len(got) != 2 {
		t.Fatalf("text sends = %v, want the answer and a note that the file failed", got)
	}
	if got[1] != mediaSendFailedText {
		t.Errorf("second message = %q, want the failure notice", got[1])
	}
	if n := len(mediaSends(t, conn)); n != 0 {
		t.Errorf("media sends = %d, want 0 — nothing was uploaded", n)
	}
}

// A file nobody could read is reported, once, rather than sent as zero bytes.
func TestSendAttachments_ReportsAnUnreadableObject(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{
		ID: mustTestUUID(t), Filename: "gone.pdf", Url: "https://cdn.example/obj/gone",
	})
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "obj/gone", err: errors.New("no such key")})
	q.sessionBinding.InstallationID = instID
	q.installation.ID = instID

	if err := o.processEvent(context.Background(), chatDoneEvent("done")); err != nil {
		t.Fatalf("processEvent: %v", err)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("upload init frames = %d, want 0 — there were no bytes to upload", n)
	}
	got := markdownSends(t, conn)
	if len(got) != 2 || got[1] != mediaSendFailedText {
		t.Errorf("text sends = %v, want the answer and the failure notice", got)
	}
}

func TestReadObject_RefusesAnObjectPastTheUploadCap(t *testing.T) {
	t.Parallel()
	o := &Outbound{objects: &fakeObjectStore{key: "k", data: make([]byte, maxMediaUploadBytes+1)}}
	if _, err := o.readObject(context.Background(), "https://cdn.example/obj/k"); !errors.Is(err, errMediaUploadTooLarge) {
		t.Errorf("oversize object error = %v, want errMediaUploadTooLarge", err)
	}
	// Exactly the cap is fine — the LimitReader's extra byte is what tells the
	// two apart.
	o = &Outbound{objects: &fakeObjectStore{key: "k", data: make([]byte, maxMediaUploadBytes)}}
	data, err := o.readObject(context.Background(), "https://cdn.example/obj/k")
	if err != nil {
		t.Fatalf("an object at exactly the cap was refused: %v", err)
	}
	if len(data) != maxMediaUploadBytes {
		t.Errorf("read %d bytes, want %d", len(data), maxMediaUploadBytes)
	}
	// An object this deployment does not store is named as such rather than
	// fetched with an empty key.
	o = &Outbound{objects: &fakeObjectStore{key: ""}}
	if _, err := o.readObject(context.Background(), "https://elsewhere.example/x"); err == nil {
		t.Error("a URL outside this deployment's storage was accepted")
	}
}

func TestWecomMediaKind_DecidesWhatWeComIsTold(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		contentType string
		filename    string
		size        int
		want        mediaMsgType
	}{
		{"an image inside the ceiling", "image/png", "a.png", 1 << 20, mediaTypeImage},
		{"content type wins over extension", "image/png", "a.pdf", 1 << 20, mediaTypeImage},
		{"a parameterised content type still matches", "image/png; charset=binary", "a.png", 10, mediaTypeImage},
		{"an oversize image is demoted, not refused", "image/png", "a.png", maxOutboundImageBytes + 1, mediaTypeFile},
		{"a video inside the ceiling", "video/mp4", "a.mp4", 1 << 20, mediaTypeVideo},
		{"an oversize video is demoted", "video/mp4", "a.mp4", maxOutboundVideoBytes + 1, mediaTypeFile},
		{"amr is the only voice", "audio/amr", "a.amr", 1 << 10, mediaTypeVoice},
		{"an oversize amr is demoted", "audio/amr", "a.amr", maxOutboundVoiceBytes + 1, mediaTypeFile},
		{"mp3 is a file, not a voice note", "audio/mpeg", "a.mp3", 1 << 10, mediaTypeFile},
		{"octet-stream falls back to the extension", "application/octet-stream", "a.png", 10, mediaTypeImage},
		{"no content type falls back to the extension", "", "a.png", 10, mediaTypeImage},
		{"an unknown type is a file", "application/pdf", "a.pdf", 10, mediaTypeFile},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := wecomMediaKind(tc.contentType, tc.filename, tc.size); got != tc.want {
				t.Errorf("wecomMediaKind(%q, %q, %d) = %q, want %q", tc.contentType, tc.filename, tc.size, got, tc.want)
			}
		})
	}
}

func TestOutboundMediaName_IsOneSegmentWithAnExtension(t *testing.T) {
	t.Parallel()
	cases := []struct {
		filename    string
		contentType string
		want        string
	}{
		{"report.pdf", "application/pdf", "report.pdf"},
		{"a/b/c/report.pdf", "application/pdf", "report.pdf"},
		{`windows\path\report.pdf`, "application/pdf", "report.pdf"},
		{"", "image/png", "attachment.png"},
		{"chart", "image/png", "chart.png"},
		{"chart", "image/jpeg", "chart.jpg"},
		{"..", "image/png", "attachment.png"},
		{"noext", "", "noext"},
	}
	for _, tc := range cases {
		if got := outboundMediaName(tc.filename, tc.contentType); got != tc.want {
			t.Errorf("outboundMediaName(%q, %q) = %q, want %q", tc.filename, tc.contentType, got, tc.want)
		}
	}
	// The name reaches the wire, so no result may still be a path.
	if got := outboundMediaName("../../etc/passwd", ""); strings.ContainsAny(got, `/\`) {
		t.Errorf("outboundMediaName kept a path separator: %q", got)
	}
}

func TestDeliverAttachments_ShedsPastThePendingCap(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{ID: mustTestUUID(t), Filename: "a.txt", Url: "u"})
	o, instID, _ := newOutboundWithMedia(t, q, &fakeObjectStore{key: "k", data: []byte("x")})
	q.sessionBinding.InstallationID = instID

	// Fill the backlog, then confirm one more is refused rather than queued.
	for i := 0; i < maxPendingAttachmentDeliveries; i++ {
		if !o.claimAttachmentSlot() {
			t.Fatalf("slot %d refused before the cap", i)
		}
	}
	if o.claimAttachmentSlot() {
		t.Error("a delivery past the pending cap was accepted — the backlog is unbounded")
	}
	o.releaseAttachmentSlot()
	if !o.claimAttachmentSlot() {
		t.Error("releasing a slot did not free it")
	}
}

func TestChatDoneMessageID_ReadsBothPayloadShapes(t *testing.T) {
	t.Parallel()
	if got := chatDoneMessageID(protocol.ChatDonePayload{MessageID: testMessageID}); got != testMessageID {
		t.Errorf("typed payload message id = %q, want %q", got, testMessageID)
	}
	// After a serialization round trip the payload arrives as a map.
	if got := chatDoneMessageID(map[string]any{"message_id": testMessageID}); got != testMessageID {
		t.Errorf("map payload message id = %q, want %q", got, testMessageID)
	}
	if got := chatDoneMessageID(map[string]any{}); got != "" {
		t.Errorf("payload with no message id = %q, want empty", got)
	}
}

// A turn the delivery path cannot address must not reach the queries at all.
func TestDeliverAttachments_IgnoresATurnItCannotAddress(t *testing.T) {
	t.Parallel()
	q := oneAttachmentQueries(t, db.Attachment{ID: mustTestUUID(t), Filename: "a.txt", Url: "u"})
	o, instID, conn := newOutboundWithMedia(t, q, &fakeObjectStore{key: "k", data: []byte("x")})

	target := attachmentTarget{InstallationID: instID, ChatID: "CHAT_1", ChatType: chatTypeSingleInt}
	// No message id on the payload — nothing was bound to this turn.
	o.deliverAttachments(events.Event{WorkspaceID: testWorkspaceID, Payload: protocol.ChatDonePayload{}}, target)
	// No workspace id.
	o.deliverAttachments(chatDoneEvent("x"), attachmentTarget{InstallationID: instID, ChatID: ""})

	if n := len(conn.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("upload init frames = %d, want 0", n)
	}
}
