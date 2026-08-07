package wecom

// media_upload_test.go — the three-cmd upload protocol, and the one decision
// inside it that is easy to get backwards: which failures are worth a second
// offer. A chunk whose ack was lost must be sent again, because losing one
// slice of a hundred would otherwise cost the whole file. A chunk the server
// refused must NOT be, because the refusal is its answer and will be its
// answer again.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// mediaConn is a socket double that answers the upload cmds with the bodies
// the real server sends — recordingConn acks with an empty body, which the
// upload cannot use because init and finish are read for their payload.
type mediaConn struct {
	mu     sync.Mutex
	frames []frameEnvelope
	sender *wsSender

	uploadID string
	mediaID  string

	// refuse[cmd] answers that cmd with an errcode instead of a result.
	refuse map[string]int
	// dropAcks[cmd] withholds the verdict for that many frames of the cmd —
	// what a lost ack looks like from this side of the wire.
	dropAcks map[string]int
}

func newMediaConn() *mediaConn {
	return &mediaConn{
		uploadID: "UPLOAD_1",
		mediaID:  "MEDIA_1",
		refuse:   map[string]int{},
		dropAcks: map[string]int{},
	}
}

func (c *mediaConn) attach(s *wsSender) *wsSender {
	c.sender = s
	return s
}

func (c *mediaConn) WriteMessage(_ int, data []byte) error {
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	c.mu.Lock()
	c.frames = append(c.frames, env)
	s := c.sender
	code := c.refuse[env.Cmd]
	drop := c.dropAcks[env.Cmd] > 0
	if drop {
		c.dropAcks[env.Cmd]--
	}
	var body json.RawMessage
	switch env.Cmd {
	case cmdUploadMediaInit:
		body = json.RawMessage(`{"upload_id":"` + c.uploadID + `"}`)
	case cmdUploadMediaFinish:
		body = json.RawMessage(`{"media_id":"` + c.mediaID + `"}`)
	}
	c.mu.Unlock()

	if s == nil || drop {
		return nil // no verdict comes back
	}
	if code != 0 {
		body = nil
	}
	s.routeResponse(frameEnvelope{
		Headers: frameHeaders{ReqID: env.Headers.ReqID},
		ErrCode: code,
		Body:    body,
	})
	return nil
}

func (c *mediaConn) ReadMessage() (int, []byte, error) { return 0, nil, nil }
func (c *mediaConn) SetReadDeadline(time.Time) error   { return nil }
func (c *mediaConn) SetWriteDeadline(time.Time) error  { return nil }
func (c *mediaConn) Close() error                      { return nil }

// cmdFrames returns every recorded frame carrying one cmd.
func (c *mediaConn) cmdFrames(cmd string) []frameEnvelope {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []frameEnvelope
	for _, f := range c.frames {
		if f.Cmd == cmd {
			out = append(out, f)
		}
	}
	return out
}

func (c *mediaConn) newSender() *wsSender { return c.attach(newWSSender(c, nil)) }

func TestUploadMedia_ChunksTheFileAndSealsIt(t *testing.T) {
	t.Parallel()
	conn := newMediaConn()
	sender := conn.newSender()

	// Two and a bit chunks, so the split is exercised rather than assumed.
	data := make([]byte, mediaChunkBytes*2+17)
	for i := range data {
		data[i] = byte(i)
	}

	mediaID, err := sender.uploadMedia(context.Background(), outboundMedia{
		Kind: mediaTypeFile, Filename: "report.pdf", Data: data,
	})
	if err != nil {
		t.Fatalf("uploadMedia: %v", err)
	}
	if mediaID != "MEDIA_1" {
		t.Errorf("media_id = %q, want the one finish returned", mediaID)
	}

	init := conn.cmdFrames(cmdUploadMediaInit)
	if len(init) != 1 {
		t.Fatalf("init frames = %d, want 1", len(init))
	}
	var initBody map[string]any
	if err := json.Unmarshal(init[0].Body, &initBody); err != nil {
		t.Fatalf("decode init body: %v", err)
	}
	if initBody["total_size"] != float64(len(data)) {
		t.Errorf("total_size = %v, want %d", initBody["total_size"], len(data))
	}
	if initBody["total_chunks"] != float64(3) {
		t.Errorf("total_chunks = %v, want 3", initBody["total_chunks"])
	}
	if initBody["filename"] != "report.pdf" {
		t.Errorf("filename = %v, want report.pdf — it is the only format hint the server gets", initBody["filename"])
	}

	// Every chunk must arrive exactly once, and the bytes must reassemble to
	// the original: the indexes go out concurrently, so an off-by-one in the
	// split would corrupt the file rather than fail the upload.
	chunks := conn.cmdFrames(cmdUploadMediaChunk)
	if len(chunks) != 3 {
		t.Fatalf("chunk frames = %d, want 3", len(chunks))
	}
	seen := map[int][]byte{}
	for _, f := range chunks {
		var b struct {
			UploadID string `json:"upload_id"`
			Index    int    `json:"chunk_index"`
			Data     string `json:"base64_data"`
		}
		if err := json.Unmarshal(f.Body, &b); err != nil {
			t.Fatalf("decode chunk body: %v", err)
		}
		if b.UploadID != "UPLOAD_1" {
			t.Errorf("chunk upload_id = %q, want the id init handed back", b.UploadID)
		}
		raw, err := base64.StdEncoding.DecodeString(b.Data)
		if err != nil {
			t.Fatalf("chunk %d is not base64: %v", b.Index, err)
		}
		if _, dup := seen[b.Index]; dup {
			t.Fatalf("chunk index %d sent twice", b.Index)
		}
		seen[b.Index] = raw
	}
	var rebuilt []byte
	for i := 0; i < 3; i++ {
		part, ok := seen[i]
		if !ok {
			t.Fatalf("chunk index %d never sent", i)
		}
		rebuilt = append(rebuilt, part...)
	}
	if string(rebuilt) != string(data) {
		t.Error("the chunks do not reassemble to the original file")
	}
}

// A lost verdict is the one failure worth a second offer — the protocol makes
// re-sending a chunk idempotent precisely so this is safe.
func TestUploadMediaChunk_ResendsAChunkWhoseVerdictNeverCame(t *testing.T) {
	t.Parallel()
	conn := newMediaConn()
	conn.dropAcks[cmdUploadMediaChunk] = 1 // the first offer is never answered
	sender := conn.newSender()

	// One chunk, so "the first offer" is unambiguous.
	if _, err := sender.uploadMedia(context.Background(), outboundMedia{
		Kind: mediaTypeFile, Filename: "small.txt", Data: []byte("hello"),
	}); err != nil {
		t.Fatalf("uploadMedia gave up on a lost ack instead of offering the chunk again: %v", err)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaChunk)); n != 2 {
		t.Errorf("chunk frames = %d, want 2 (the lost one and its retry)", n)
	}
}

// The opposite case, and the reason the retry is conditional: a refusal is the
// server's answer and will be its answer again. Retrying it wastes the budget
// and can only end the same way.
func TestUploadMediaChunk_DoesNotResendARefusedChunk(t *testing.T) {
	t.Parallel()
	conn := newMediaConn()
	conn.refuse[cmdUploadMediaChunk] = 40058
	sender := conn.newSender()

	_, err := sender.uploadMedia(context.Background(), outboundMedia{
		Kind: mediaTypeFile, Filename: "small.txt", Data: []byte("hello"),
	})
	if err == nil {
		t.Fatal("a refused chunk reported success")
	}
	var apiErr *wecomAPIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error is not a *wecomAPIError: %v", err)
	}
	if n := len(conn.cmdFrames(cmdUploadMediaChunk)); n != 1 {
		t.Errorf("chunk frames = %d, want 1 — a refusal must not be offered again", n)
	}
	// The upload must stop there rather than seal a file the server rejected.
	if n := len(conn.cmdFrames(cmdUploadMediaFinish)); n != 0 {
		t.Errorf("finish frames = %d, want 0 — a failed upload must not be sealed", n)
	}
}

// A push whose verdict never came may already have arrived. Sending it again
// would put the same media_id out twice and the person sees the file twice
// with nothing to undo, so a timeout is reported rather than retried.
func TestSendMedia_ReportsALostAckWithoutSendingAgain(t *testing.T) {
	t.Parallel()
	conn := newMediaConn()
	conn.dropAcks[cmdSendMsg] = 5 // no verdict ever comes back for the push
	sender := conn.newSender()

	err := sender.sendMedia(context.Background(), "CHAT_1", chatTypeSingleInt, mediaSend{
		Kind: mediaTypeFile, MediaID: "MEDIA_1",
	})
	if !errors.Is(err, errAckTimeout) {
		t.Fatalf("a lost push ack reported as %v, want errAckTimeout", err)
	}
	if n := len(conn.cmdFrames(cmdSendMsg)); n != 1 {
		t.Errorf("send frames = %d, want 1 — a duplicate file is worse than an unconfirmed one", n)
	}
}

func TestSplitMediaChunks_RejectsWhatCannotBeUploaded(t *testing.T) {
	t.Parallel()
	if _, err := splitMediaChunks(nil); !errors.Is(err, errMediaUploadEmpty) {
		t.Errorf("empty file error = %v, want errMediaUploadEmpty", err)
	}
	if _, err := splitMediaChunks(make([]byte, maxMediaUploadBytes+1)); !errors.Is(err, errMediaUploadTooLarge) {
		t.Errorf("oversize file error = %v, want errMediaUploadTooLarge", err)
	}
	// Exactly the cap is allowed, and fills every chunk.
	chunks, err := splitMediaChunks(make([]byte, maxMediaUploadBytes))
	if err != nil {
		t.Fatalf("a file at exactly the cap was rejected: %v", err)
	}
	if len(chunks) != maxMediaChunks {
		t.Errorf("chunks at the cap = %d, want %d", len(chunks), maxMediaChunks)
	}
}

func TestMediaBodyFields_VideoAloneCarriesTitleAndDescription(t *testing.T) {
	t.Parallel()
	// Video's two fields are required and clipped to WeCom's byte budgets.
	body, err := mediaBodyFields(mediaSend{
		Kind: mediaTypeVideo, MediaID: "M", Title: strings.Repeat("题", 40), Description: "d",
	})
	if err != nil {
		t.Fatalf("video body: %v", err)
	}
	nested := body[string(mediaTypeVideo)].(map[string]any)
	if got := len(nested["title"].(string)); got > videoTitleBytes {
		t.Errorf("title = %d bytes, want <= %d", got, videoTitleBytes)
	}

	// The other kinds have no such field and reject a body that carries one,
	// so nothing beyond the media_id may be emitted for them.
	for _, kind := range []mediaMsgType{mediaTypeFile, mediaTypeImage, mediaTypeVoice} {
		body, err := mediaBodyFields(mediaSend{Kind: kind, MediaID: "M", Title: "t", Description: "d"})
		if err != nil {
			t.Fatalf("%s body: %v", kind, err)
		}
		nested := body[string(kind)].(map[string]any)
		if len(nested) != 1 {
			t.Errorf("%s nested fields = %v, want media_id alone", kind, nested)
		}
	}

	if _, err := mediaBodyFields(mediaSend{Kind: mediaTypeFile}); err == nil {
		t.Error("a media message with no media_id was accepted")
	}
	if _, err := mediaBodyFields(mediaSend{Kind: "sticker", MediaID: "M"}); err == nil {
		t.Error("an unknown msgtype was accepted")
	}
}

func TestClipUTF8_CutsOnARuneBoundary(t *testing.T) {
	t.Parallel()
	// Each of these is 3 bytes, so a budget of 7 lands mid-character.
	got := clipUTF8("中文标题", 7)
	if len(got) != 6 || got != "中文" {
		t.Errorf("clipUTF8 = %q (%d bytes), want %q — a cut through a character sends broken UTF-8", got, len(got), "中文")
	}
	if got := clipUTF8("short", 64); got != "short" {
		t.Errorf("a string inside the budget was altered: %q", got)
	}
}

func TestOutboundMediaValidate_RejectsUnsendableMedia(t *testing.T) {
	t.Parallel()
	if err := (outboundMedia{Kind: "document", Filename: "a.txt"}).validate(); err == nil {
		t.Error("an unknown media kind was accepted")
	}
	if err := (outboundMedia{Kind: mediaTypeFile, Filename: "  "}).validate(); err == nil {
		t.Error("media with no filename was accepted — the name is the only format hint WeCom gets")
	}
}
