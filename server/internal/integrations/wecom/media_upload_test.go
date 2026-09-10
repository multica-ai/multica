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

	// chunkArrived and chunkRelease make concurrency observable. When
	// chunkArrived is non-nil, every chunk frame drops a token into it and its
	// verdict is withheld until chunkRelease closes — so the tokens in the
	// buffer count the chunks awaiting an answer, which is what "in flight"
	// means here.
	//
	// The withholding happens OFF the write path, on a goroutine, and that is
	// the whole trick: wsSender.write serializes every frame under one mutex,
	// so blocking inside WriteMessage would measure the write lock rather than
	// the upload's concurrency and report 1 whatever the limit is.
	//
	// chunkArrived is buffered past any file this test package uploads: a send
	// that blocked would itself become the thing limiting concurrency, and the
	// measurement would report the harness rather than the code.
	chunkArrived chan struct{}
	chunkRelease chan struct{}
}

func newMediaConn() *mediaConn {
	return &mediaConn{
		uploadID: "UPLOAD_1",
		mediaID:  "MEDIA_1",
		refuse:   map[string]int{},
		dropAcks: map[string]int{},
	}
}

// holdChunks arms the in-flight gate. Every chunk frame stops on arrival until
// the returned release func is called.
func (c *mediaConn) holdChunks() (arrived <-chan struct{}, release func()) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.chunkArrived = make(chan struct{}, 4*maxMediaChunks)
	c.chunkRelease = make(chan struct{})
	ch, rel := c.chunkArrived, c.chunkRelease
	return ch, func() { close(rel) }
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
	arrived, release := c.chunkArrived, c.chunkRelease
	c.mu.Unlock()

	if s == nil || drop {
		return nil // no verdict comes back
	}
	if code != 0 {
		body = nil
	}
	verdict := frameEnvelope{
		Headers: frameHeaders{ReqID: env.Headers.ReqID},
		ErrCode: code,
		Body:    body,
	}
	// A held chunk is recorded as arrived and then left unanswered. The wait
	// runs on its own goroutine so this returns and frees the sender's write
	// mutex — otherwise every chunk after the first would be queued behind the
	// lock and the count would report the socket, not the uploader.
	if env.Cmd == cmdUploadMediaChunk && arrived != nil {
		arrived <- struct{}{}
		go func() {
			<-release
			s.routeResponse(verdict)
		}()
		return nil
	}
	s.routeResponse(verdict)
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

// The cap is 20 MiB, and it is WeCom's number rather than ours: the OpenClaw
// plugin they publish (WecomTeam/wecom-openclaw-plugin src/const.ts) sets
// FILE_MAX_BYTES to 20 MiB and defines ABSOLUTE_MAX_BYTES — the size past which
// nothing can be sent — as the same value. The 50 MB the chunk arithmetic
// allows is what the FRAMING can describe, not what the platform takes.
func TestSplitMediaChunks_RejectsWhatCannotBeUploaded(t *testing.T) {
	t.Parallel()
	if _, err := splitMediaChunks(nil); !errors.Is(err, errMediaUploadEmpty) {
		t.Errorf("empty file error = %v, want errMediaUploadEmpty", err)
	}
	// One byte past 20 MiB. The boundary is asserted a byte either side because
	// a cap is only a cap where it changes the answer.
	if _, err := splitMediaChunks(make([]byte, 20<<20+1)); !errors.Is(err, errMediaUploadTooLarge) {
		t.Errorf("a %d byte file was accepted; WeCom's own hard limit is %d", 20<<20+1, 20<<20)
	}
	chunks, err := splitMediaChunks(make([]byte, 20<<20))
	if err != nil {
		t.Fatalf("a file at exactly 20 MiB was rejected: %v", err)
	}
	if want := (20 << 20) / mediaChunkBytes; len(chunks) != want {
		t.Errorf("chunks at the cap = %d, want %d", len(chunks), want)
	}
	// Stated as the constant too, so raising the constant without revisiting
	// this test still leaves the boundary asserted at the number above.
	if maxMediaUploadBytes != 20<<20 {
		t.Errorf("maxMediaUploadBytes = %d, want %d — the source is WeCom's ABSOLUTE_MAX_BYTES", maxMediaUploadBytes, 20<<20)
	}
}

// The product cap has to stay inside what the transport can express. If
// maxMediaUploadBytes were ever raised past 50 MB the split would emit more
// than the 100 chunks the protocol accepts, and every large upload would be
// refused at init with an errcode that says nothing about why.
func TestMediaUploadCapFitsTheChunkProtocol(t *testing.T) {
	t.Parallel()
	if maxMediaUploadBytes > maxMediaTransportBytes {
		t.Fatalf("upload cap %d exceeds what %d chunks of %d bytes can carry (%d)",
			maxMediaUploadBytes, maxMediaChunks, mediaChunkBytes, maxMediaTransportBytes)
	}
	chunks, err := splitMediaChunks(make([]byte, maxMediaUploadBytes))
	if err != nil {
		t.Fatalf("a file at the cap could not be split: %v", err)
	}
	if len(chunks) > maxMediaChunks {
		t.Errorf("a file at the cap needs %d chunks, protocol allows %d", len(chunks), maxMediaChunks)
	}
}

// The concurrency ladder is WeCom's, copied from the SDK they publish
// (WecomTeam/aibot-node-sdk src/client.ts, uploadMedia): `totalChunks <= 4 ?
// totalChunks : totalChunks <= 10 ? 3 : 2`. The step at eleven is the one with
// a stated reason — past ten chunks their backend answers a burst of concurrent
// chunks with a system error — so it is a server-side limit no retry gets
// around, and a fixed concurrency fails exactly the large files that most need
// to arrive.
func TestMediaChunkParallelism_FollowsWeComsOwnLadder(t *testing.T) {
	t.Parallel()
	cases := []struct {
		chunks int
		want   int
		why    string
	}{
		{chunks: 0, want: 1, why: "never zero — errgroup with a limit of zero admits no goroutine at all and the upload would hang"},
		{chunks: 1, want: 1, why: "one chunk, one flight"},
		{chunks: 4, want: 4, why: "at or below four every chunk goes at once; a 2 MB file is not slowed by a caution meant for big ones"},
		{chunks: 5, want: 3, why: "the middle step"},
		{chunks: 10, want: 3, why: "ten is still the middle step — the drop is past ten, not at it"},
		{chunks: 11, want: 2, why: "eleven is where WeCom's backend starts answering a burst with a system error"},
		{chunks: 40, want: 2, why: "a 20 MiB file, the largest we send"},
	}
	for _, tc := range cases {
		if got := mediaChunkParallelism(tc.chunks); got != tc.want {
			t.Errorf("mediaChunkParallelism(%d) = %d, want %d — %s", tc.chunks, got, tc.want, tc.why)
		}
	}
}

// And the ladder has to be wired to the errgroup, not merely defined. Eleven
// chunks are offered with every frame held unanswered, so the chunks sitting on
// the socket are exactly the ones in flight; a third arriving would mean the
// limit came from somewhere other than the file's size.
func TestUploadMediaChunks_HoldsToTheParallelismForTheFileSize(t *testing.T) {
	t.Parallel()
	conn := newMediaConn()
	arrived, release := conn.holdChunks()
	sender := conn.newSender()

	// 10 full chunks and one byte: eleven, the first size past the ladder's
	// last step.
	data := make([]byte, mediaChunkBytes*10+1)
	done := make(chan error, 1)
	go func() {
		_, err := sender.uploadMedia(context.Background(), outboundMedia{
			Kind: mediaTypeFile, Filename: "big.bin", Data: data,
		})
		done <- err
	}()

	const wantInFlight = 2
	for i := 0; i < wantInFlight; i++ {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d chunks reached the socket, want %d in flight", i, wantInFlight)
		}
	}
	select {
	case <-arrived:
		t.Errorf("a third chunk went out with %d already unanswered — an 11-chunk upload must run %d at a time",
			wantInFlight, wantInFlight)
	case <-time.After(250 * time.Millisecond):
	}

	release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("uploadMedia: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("uploadMedia did not finish after the chunks were released")
	}
	if n := len(conn.cmdFrames(cmdUploadMediaChunk)); n != 11 {
		t.Errorf("chunk frames = %d, want 11 — every slice must still be sent", n)
	}
}

// The other end of the ladder, and the reason it is a ladder rather than a
// constant 2: a small file is not serialized. Four chunks all go at once.
func TestUploadMediaChunks_ASmallFileGoesAllAtOnce(t *testing.T) {
	t.Parallel()
	conn := newMediaConn()
	arrived, release := conn.holdChunks()
	sender := conn.newSender()

	data := make([]byte, mediaChunkBytes*3+1) // four chunks
	done := make(chan error, 1)
	go func() {
		_, err := sender.uploadMedia(context.Background(), outboundMedia{
			Kind: mediaTypeFile, Filename: "small.bin", Data: data,
		})
		done <- err
	}()

	for i := 0; i < 4; i++ {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("only %d of 4 chunks reached the socket; a file this size must not be serialized", i)
		}
	}
	release()
	if err := <-done; err != nil {
		t.Fatalf("uploadMedia: %v", err)
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
