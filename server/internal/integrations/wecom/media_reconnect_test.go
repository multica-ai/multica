package wecom

// media_reconnect_test.go — a file delivery whose connection WeCom replaces
// while the upload is in flight.
//
// Each test runs the whole attachment worker (Outbound.sendAttachments) with
// two sockets: the one registered when the delivery starts, and the one the
// registry holds after the swap. The old socket "dies" on a chosen frame the
// way a closed gorilla connection does — the write fails with "use of closed
// network connection" — and the swap happens at that same moment, so no sleep
// stands in for the ordering.

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// dyingConn is a mediaConn whose socket closes on the first frame carrying
// dieOn. That frame and every one after it fail at the write, as they do on a
// connection WeCom has dropped; onDeath runs once, at the moment of the close.
type dyingConn struct {
	*mediaConn
	dieOn   string
	onDeath func()
	dead    atomic.Bool
	once    sync.Once
}

var errClosedSocket = fmt.Errorf("write tcp 10.0.0.1:443: %w", net.ErrClosed)

func (c *dyingConn) WriteMessage(mt int, data []byte) error {
	if c.dead.Load() {
		return errClosedSocket
	}
	var env frameEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return err
	}
	if env.Cmd == c.dieOn {
		c.once.Do(func() {
			c.dead.Store(true)
			if c.onDeath != nil {
				c.onDeath()
			}
		})
		return errClosedSocket
	}
	return c.mediaConn.WriteMessage(mt, data)
}

// reconnectRig is a revocationRig whose delivery starts on a socket that dies.
// r.conn and r.sender are the REPLACEMENT; old is the one the delivery begins
// on.
type reconnectRig struct {
	*revocationRig
	old       *dyingConn
	oldSender *wsSender
	logs      *syncBuf
}

// newReconnectRig registers the old socket and arranges for the registry to
// hold the replacement from the moment the old one closes on dieOn.
func newReconnectRig(t *testing.T, dieOn string) *reconnectRig {
	t.Helper()
	r := &reconnectRig{revocationRig: newRevocationRig(t, []byte("DATA"))}
	r.old = &dyingConn{mediaConn: newMediaConn(), dieOn: dieOn}
	r.oldSender = r.old.mediaConn.attach(newWSSender(r.old, nil))
	r.o.senders.set(r.q.id, r.oldSender)
	r.old.onDeath = func() { r.o.senders.set(r.q.id, r.sender) }
	r.o.logger, r.logs = capturingLogger()
	return r
}

const restartLogLine = "connection replaced during the upload, uploading the file again on the new one"

// The production incident: the socket closed under a chunk, WeCom had already
// subscribed a new connection for the same installation, and the file was
// reported as failed. It is now uploaded again, whole, on the new connection.
func TestSendAttachments_AConnectionReplacedMidUploadDeliversOnTheNewOne(t *testing.T) {
	t.Parallel()
	r := newReconnectRig(t, cmdUploadMediaChunk)
	r.mustFinish(t, r.run(t))

	if n := len(r.old.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Fatalf("%d init frames on the old socket, want 1 — the upload has to have started there", n)
	}
	for _, cmd := range []string{cmdUploadMediaInit, cmdUploadMediaChunk, cmdUploadMediaFinish} {
		if n := len(r.conn.cmdFrames(cmd)); n != 1 {
			t.Errorf("%d %s frames on the new socket, want 1 — the upload starts over from init", n, cmd)
		}
	}
	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 1 {
		t.Errorf("%d send frames on the new socket, want exactly the file", n)
	}
	if got := markdownSends(t, r.conn); len(got) != 0 {
		t.Errorf("told the user %q about a file that arrived", got)
	}
	if got := r.mx.get("attachment_delivered"); got != 1 {
		t.Errorf("attachment_delivered = %d, want 1", got)
	}
	for _, k := range []string{"attachment_dropped", "attachment_unconfirmed", "outbound_dropped"} {
		if got := r.mx.get(k); got != 0 {
			t.Errorf("%s = %d, want 0 — the first attempt is not a drop when the restart delivers", k, got)
		}
	}
	logs := r.logs.String()
	line := ""
	for _, l := range strings.Split(logs, "\n") {
		if strings.Contains(l, restartLogLine) {
			line = l
		}
	}
	if line == "" {
		t.Fatalf("no restart line in the log:\n%s", logs)
	}
	for _, want := range []string{"level=WARN", "installation_id=", "attachment_id="} {
		if !strings.Contains(line, want) {
			t.Errorf("restart line lacks %q: %s", want, line)
		}
	}
	if strings.Contains(logs, "cdn.example") {
		t.Errorf("the object's URL reached the log:\n%s", logs)
	}
}

// The restart fails too: one file, one drop, one notice — on the connection
// that is live, since the old one is gone.
func TestSendAttachments_ARestartThatFailsTooIsOneDropAndOneNotice(t *testing.T) {
	t.Parallel()
	r := newReconnectRig(t, cmdUploadMediaChunk)
	r.conn.refuse[cmdUploadMediaInit] = 40058
	r.mustFinish(t, r.run(t))

	if n := len(r.conn.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Errorf("%d init frames on the new socket, want 1", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
	if got := r.mx.get("outbound_dropped"); got != 1 {
		t.Errorf("outbound_dropped = %d, want 1 — the file was the reply", got)
	}
	if got := r.mx.get("attachment_delivered"); got != 0 {
		t.Errorf("attachment_delivered = %d, want 0", got)
	}
	if got := markdownSends(t, r.conn); len(got) != 1 {
		t.Errorf("%d notices on the live socket, want 1: %q", len(got), got)
	}
}

// Once per file. A connection that is replaced again during the restart is
// flapping, and a third upload on it is not offered.
func TestSendAttachments_AFileIsUploadedAgainOnlyOnce(t *testing.T) {
	t.Parallel()
	r := newReconnectRig(t, cmdUploadMediaChunk)
	// The replacement dies the same way, and a third connection takes over.
	third := newMediaConn()
	thirdSender := third.newSender()
	second := &dyingConn{mediaConn: r.conn, dieOn: cmdUploadMediaChunk,
		onDeath: func() { r.o.senders.set(r.q.id, thirdSender) }}
	r.sender = r.conn.attach(newWSSender(second, nil))
	r.mustFinish(t, r.run(t))

	if n := len(r.conn.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Fatalf("%d init frames on the second socket, want 1 — the restart has to have run", n)
	}
	if n := len(third.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("%d init frames on the third socket, want 0", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
	if got := strings.Count(r.logs.String(), restartLogLine); got != 1 {
		t.Errorf("%d restart lines, want 1", got)
	}
}

// The connection was NOT replaced: a chunk whose verdict never comes is offered
// twice on the same socket, as before, and the upload is not started over.
// Slow by necessity — request waits the full ackTimeout for each of the two
// offers. Not parallel for the same reason: it holds one of the process-wide
// attachmentSlots for those ten seconds, and the parallel tests that wait a
// few seconds for a slot of their own would time out behind it.
func TestSendAttachments_AChunkTimeoutOnTheSameConnectionDoesNotRestart(t *testing.T) {
	r := newRevocationRig(t, []byte("DATA"))
	_, release := r.conn.holdChunks()
	t.Cleanup(release)
	r.mustFinish(t, r.run(t))

	if n := len(r.conn.cmdFrames(cmdUploadMediaChunk)); n != 2 {
		t.Errorf("%d chunk frames, want 2 — the existing second offer of a timed-out chunk", n)
	}
	if n := len(r.conn.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Errorf("%d init frames, want 1 — the same connection is no reason to start over", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
}

// The upload finished and the file message itself is what the closed socket
// swallowed. That message may be in the chat, so nothing is uploaded or sent
// again on the new connection; the user hears the "may have arrived" wording.
func TestSendAttachments_AFailureAfterTheFileMessageIsNotRetried(t *testing.T) {
	t.Parallel()
	r := newReconnectRig(t, cmdSendMsg)
	r.mustFinish(t, r.run(t))

	if n := len(r.old.cmdFrames(cmdUploadMediaFinish)); n != 1 {
		t.Fatalf("%d finish frames on the old socket, want 1 — the upload has to have completed there", n)
	}
	if n := len(r.conn.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("%d init frames on the new socket, want 0", n)
	}
	notices := markdownSends(t, r.conn)
	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != len(notices) {
		t.Errorf("%d send frames on the new socket but %d notices — the file was sent again", n, len(notices))
	}
	if len(notices) != 1 {
		t.Errorf("%d notices, want 1: %q", len(notices), notices)
	}
	if got := r.mx.get("attachment_unconfirmed"); got != 1 {
		t.Errorf("attachment_unconfirmed = %d, want 1", got)
	}
	if got := r.mx.get("attachment_dropped"); got != 0 {
		t.Errorf("attachment_dropped = %d, want 0", got)
	}
	if strings.Contains(r.logs.String(), restartLogLine) {
		t.Error("the restart ran after the file message")
	}
}

// WeCom refused a chunk on the old connection, and the registry moved on in the
// same instant. A refusal is the server's verdict on the file from a socket
// that was alive enough to give one, so the upload is not started over.
func TestSendAttachments_ARefusalIsNotUploadedAgainOnTheNewConnection(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, []byte("DATA"))
	old := newMediaConn()
	old.refuse[cmdUploadMediaChunk] = 40058
	r.o.senders.set(r.q.id, old.newSender())
	old.onWrite = func(cmd string) {
		if cmd == cmdUploadMediaChunk {
			r.o.senders.set(r.q.id, r.sender)
		}
	}
	r.mustFinish(t, r.run(t))

	if n := len(old.cmdFrames(cmdUploadMediaChunk)); n != 1 {
		t.Fatalf("%d chunk frames on the old socket, want 1 — the refusal has to have happened there", n)
	}
	if n := len(r.conn.cmdFrames(cmdUploadMediaInit)); n != 0 {
		t.Errorf("%d init frames on the new socket, want 0", n)
	}
	if got := r.mx.get("attachment_dropped:" + string(dropPlatformRefused)); got != 1 {
		t.Errorf("attachment_dropped:platform_refused = %d, want 1", got)
	}
}

// The installation is revoked between the failed first upload and the restart.
// The restart's first frame meets the write guard: nothing reaches the new
// socket, the file is one drop, and the notice is withheld like every other
// write after a revocation.
func TestSendAttachments_ARevocationBeforeTheRestartWritesNothing(t *testing.T) {
	t.Parallel()
	r := newReconnectRig(t, cmdUploadMediaChunk)
	r.old.onDeath = func() {
		r.q.revoked.Store(true)
		r.o.senders.set(r.q.id, r.sender)
	}
	r.mustFinish(t, r.run(t))

	if n := frameCount(r.conn); n != 0 {
		t.Errorf("%d frames on the new socket after the revoke, want 0", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
	if got := r.mx.get("attachment_delivered"); got != 0 {
		t.Errorf("attachment_delivered = %d, want 0", got)
	}
	if !strings.Contains(r.logs.String(), errMediaPermissionWithdrawn.Error()) {
		t.Errorf("the drop is not attributed to the withdrawn permission:\n%s", r.logs.String())
	}
}

// Each file starts on the connection the registry holds when its turn comes,
// not the one resolved before the first file.
func TestSendAttachments_EachFileStartsOnTheLiveConnection(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, []byte("DATA"))
	second := db.Attachment{
		ID: mustTestUUID(t), Filename: "second.bin", ContentType: "application/octet-stream",
		Url: "https://cdn.example/obj/big", SizeBytes: 4,
	}
	r.q.attachments = append(r.q.attachments, second)
	old := newMediaConn()
	oldSender := old.newSender()
	r.o.senders.set(r.q.id, oldSender)
	// The registry moves on once the first file's message is out; the old
	// socket still answers, so only the resolution decides where file two goes.
	var swapped sync.Once
	old.onWrite = func(cmd string) {
		if cmd == cmdSendMsg {
			swapped.Do(func() { r.o.senders.set(r.q.id, r.sender) })
		}
	}
	r.mustFinish(t, r.run(t))

	if n := len(old.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Errorf("%d init frames on the old socket, want 1 (the first file)", n)
	}
	if n := len(r.conn.cmdFrames(cmdUploadMediaInit)); n != 1 {
		t.Errorf("%d init frames on the live socket, want 1 (the second file)", n)
	}
	if got := r.mx.get("attachment_delivered"); got != 2 {
		t.Errorf("attachment_delivered = %d, want 2", got)
	}
}

// uploadMayRestart rules out the failures a new connection would meet again.
func TestUploadMayRestart_RulesOutWhatANewConnectionWouldRepeat(t *testing.T) {
	t.Parallel()
	ended, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		err  error
		want bool
	}{
		{"closed socket", context.Background(), fmt.Errorf("upload chunk 2: %w: %w", errWriteAttempted, errClosedSocket), true},
		{"lost ack", context.Background(), errAckTimeout, true},
		{"stated refusal", context.Background(), &wecomAPIError{Cmd: cmdUploadMediaInit, Code: 40058}, false},
		{"permission withdrawn", context.Background(), fmt.Errorf("%w: %w", errNotAttempted, errMediaPermissionWithdrawn), false},
		{"empty file", context.Background(), errMediaUploadEmpty, false},
		{"over the cap", context.Background(), errMediaUploadTooLarge, false},
		{"budget spent", ended, errAckTimeout, false},
	} {
		if got := uploadMayRestart(tc.ctx, tc.err); got != tc.want {
			t.Errorf("%s: uploadMayRestart = %v, want %v", tc.name, got, tc.want)
		}
	}
}
