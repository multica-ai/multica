package wecom

// attachment_revocation_test.go — the whole attachment worker
// (Outbound.sendAttachments) against an installation that is revoked, or whose
// permission cannot be read, while a delivery is in flight.
//
// One rule: once the permission is gone, NOTHING more is written to WeCom for
// that installation — not a chunk, not the file message, not the sentence that
// says a file failed. These tests run the worker, because the notice is only
// reachable from there; calling sendAttachment alone never exercises it.
//
// Each test holds one of the waits a frame can sit in (the socket's writer,
// the chat's turn, the acknowledgments of chunks already out), changes the
// permission while the frame is in it, and counts what reached the socket.
// The sender-level versions of the same orderings are in media_upload_test.go.

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// revocableQueries is fakeOutboundQueries with an installation whose status the
// test can change after the delivery has started — the UPDATE a person's
// removing the bot performs. It answers the way production does: by id, and
// pgx.ErrNoRows for any other.
type revocableQueries struct {
	*fakeOutboundQueries
	id         pgtype.UUID
	revoked    atomic.Bool
	unreadable atomic.Bool
	// stalled makes the read hang until its context ends, the way a database
	// that has stopped answering does.
	stalled atomic.Bool
}

func (r *revocableQueries) GetChannelInstallation(ctx context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	if r.stalled.Load() {
		<-ctx.Done()
		return db.ChannelInstallation{}, ctx.Err()
	}
	if arg.ID != r.id || arg.ChannelType != channelTypeWecom {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	if r.unreadable.Load() {
		return db.ChannelInstallation{}, errors.New("wecom test: database is unwell")
	}
	status := InstallationActive
	if r.revoked.Load() {
		status = InstallationRevoked
	}
	return db.ChannelInstallation{ID: r.id, Status: string(status)}, nil
}

type revocationRig struct {
	o      *Outbound
	q      *revocableQueries
	conn   *mediaConn
	sender *wsSender
	mx     *countingMetrics
	to     attachmentTarget
}

// newRevocationRig delivers one file of the given bytes to one chat.
func newRevocationRig(t *testing.T, data []byte) *revocationRig {
	t.Helper()
	inner := oneAttachmentQueries(t, db.Attachment{
		ID: mustTestUUID(t), Filename: "big.bin", ContentType: "application/octet-stream",
		Url: "https://cdn.example/obj/big", SizeBytes: int64(len(data)),
	})
	r := &revocationRig{
		q:    &revocableQueries{fakeOutboundQueries: inner, id: mustTestUUID(t)},
		conn: newMediaConn(),
		mx:   newCountingMetrics(),
	}
	r.sender = r.conn.newSender()
	reg := newSendersRegistry()
	reg.set(r.q.id, r.sender)
	r.o = NewOutbound(r.q, reg, nil, slog.Default(),
		WithOutboundMetrics(r.mx), WithAttachments(&fakeObjectStore{key: "obj/big", data: data}))
	r.o.spawn = func(f func()) { f() }
	r.to = attachmentTarget{InstallationID: r.q.id, ChatID: "CHAT_1", ChatType: chatTypeSingleInt, SessionID: testSessionID}
	return r
}

// run starts the whole worker, as the spawned goroutine would run it.
func (r *revocationRig) run(t *testing.T) <-chan struct{} {
	return r.runCtx(t, context.Background())
}

func (r *revocationRig) runCtx(t *testing.T, ctx context.Context) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		r.o.sendAttachments(ctx, mustParseTaskUUID(t, testMessageID), mustParseTaskUUID(t, testWorkspaceID), r.to, true)
	}()
	return done
}

func (r *revocationRig) mustFinish(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(15 * time.Second):
		t.Fatal("the attachment worker never returned")
	}
}

func awaitChunks(t *testing.T, arrived <-chan struct{}, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-arrived:
		case <-time.After(10 * time.Second):
			t.Fatalf("chunk %d never reached the socket", i+1)
		}
	}
}

// fiveChunks runs three at a time, so the fourth and fifth wait for a slot.
func fiveChunks() []byte { return make([]byte, mediaChunkBytes*4+1) }

// Review scenario 1, through the worker: the writer is held, the fourth chunk
// queues for it, permission goes while it queues, the writer frees. The
// permission check that preceded the wait had said yes; the chunk must not go.
func TestSendAttachments_AChunkQueuedForTheWriterIsNotWrittenAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, fiveChunks())
	arrived, release := r.conn.holdChunks()
	done := r.run(t)
	awaitChunks(t, arrived, 3)

	// Three chunks are out and unanswered, so the fourth has not been
	// dispatched. Take the writer, then let the acks through: the fourth is
	// dispatched into a held writer.
	if err := r.sender.lockWriter(context.Background()); err != nil {
		t.Fatal(err)
	}
	release()
	settle()
	r.q.revoked.Store(true)
	r.sender.unlockWriter()
	r.mustFinish(t, done)

	if n := len(r.conn.cmdFrames(cmdUploadMediaChunk)); n != 3 {
		t.Errorf("%d chunk frames on the wire, want 3: a chunk queued for the writer went out after the revoke", n)
	}
	if n := len(r.conn.cmdFrames(cmdUploadMediaFinish)); n != 0 {
		t.Errorf("%d finish frames, want 0", n)
	}
	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Errorf("%d send frames, want 0 — neither the file nor a notice about it", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
}

// Review scenario 2, through the worker: the upload and every check before the
// final send are done, the file message queues for the chat's turn behind
// another message, permission goes, the turn comes.
func TestSendAttachments_TheFileMessageQueuedForTheChatIsNotSentAfterTheRevoke(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, []byte("DATA"))
	releaseChat, err := r.sender.chats.acquire(context.Background(), r.to.ChatID)
	if err != nil {
		t.Fatal(err)
	}
	done := r.run(t)
	waitFor(t, "the upload to be sealed", func() bool {
		return len(r.conn.cmdFrames(cmdUploadMediaFinish)) == 1
	})
	settle() // the file message is now parked on the chat's turn
	r.q.revoked.Store(true)
	releaseChat()
	r.mustFinish(t, done)

	if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
		t.Errorf("%d send frames, want 0 — the file message went out after the revoke, or a notice followed it", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1", got)
	}
}

// Review scenario 3, through the worker, once for each way the permission can
// be lost: with chunks in flight the permission goes, their acknowledgments
// arrive, and the worker finds out. The guard stops what is left; the notice
// that "a file could not be sent" is a write into the chat and is stopped by
// the same guard.
func TestSendAttachments_LosingThePermissionMidUploadIsSilent(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		lose func(*revocableQueries)
	}{
		{"revoked", func(q *revocableQueries) { q.revoked.Store(true) }},
		// A row that cannot be read is not a row that says yes.
		{"unreadable", func(q *revocableQueries) { q.unreadable.Store(true) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			r := newRevocationRig(t, fiveChunks())
			arrived, release := r.conn.holdChunks()
			done := r.run(t)
			awaitChunks(t, arrived, 3)
			tc.lose(r.q)
			release()
			r.mustFinish(t, done)

			if n := len(r.conn.cmdFrames(cmdUploadMediaChunk)); n != 3 {
				t.Errorf("%d chunk frames, want 3", n)
			}
			if n := len(r.conn.cmdFrames(cmdUploadMediaFinish)); n != 0 {
				t.Errorf("%d finish frames, want 0", n)
			}
			if got := markdownSends(t, r.conn); len(got) != 0 {
				t.Errorf("told the user %q after the permission was gone", got)
			}
			if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
				t.Errorf("%d send frames, want 0", n)
			}
			if got := r.mx.get("attachment_dropped"); got != 1 {
				t.Errorf("attachment_dropped = %d, want 1 — the file is still accounted for", got)
			}
		})
	}
}

// The rig's notice path is live: with the permission intact each of these exits
// does tell the user, and with it gone none does. The control is what stops the
// silent half passing because the notice was never reachable from the setup.
//
// One case per call to tellUser in sendAttachments, because "the failure
// notice" is five places and the permission has to hold at every one.
func TestSendAttachments_EveryNoticeToTheUserNeedsThePermission(t *testing.T) {
	// Not parallel: the slot-wait case takes the package's attachmentSlots.
	for _, exit := range []struct {
		name    string
		arrange func(t *testing.T, r *revocationRig) context.Context
	}{
		{"the attachment lookup failed", func(_ *testing.T, r *revocationRig) context.Context {
			r.q.attachmentsErr = errors.New("wecom test: lookup failed")
			return context.Background()
		}},
		{"too many deliveries are pending", func(_ *testing.T, r *revocationRig) context.Context {
			r.o.pendingMu.Lock()
			r.o.pendingAttachments = maxPendingAttachmentDeliveries
			r.o.pendingMu.Unlock()
			return context.Background()
		}},
		{"the budget ran out waiting for a slot", func(t *testing.T, _ *revocationRig) context.Context {
			for i := 0; i < maxConcurrentAttachmentDeliveries; i++ {
				attachmentSlots <- struct{}{}
			}
			t.Cleanup(func() {
				for i := 0; i < maxConcurrentAttachmentDeliveries; i++ {
					<-attachmentSlots
				}
			})
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			return ctx
		}},
		{"the upload was refused", func(_ *testing.T, r *revocationRig) context.Context {
			r.conn.refuse[cmdUploadMediaInit] = 40058
			return context.Background()
		}},
		{"the file message was refused", func(_ *testing.T, r *revocationRig) context.Context {
			r.conn.refuse[cmdSendMsg] = 40058
			return context.Background()
		}},
	} {
		for _, perm := range []struct {
			name   string
			lose   func(*revocableQueries)
			notice int
		}{
			{"permission intact", func(*revocableQueries) {}, 1},
			{"revoked", func(q *revocableQueries) { q.revoked.Store(true) }, 0},
			{"unreadable", func(q *revocableQueries) { q.unreadable.Store(true) }, 0},
		} {
			t.Run(exit.name+"/"+perm.name, func(t *testing.T) {
				r := newRevocationRig(t, []byte("DATA"))
				ctx := exit.arrange(t, r)
				perm.lose(r.q)
				r.mustFinish(t, r.runCtx(t, ctx))

				// Every markdown push in this rig is a notice: the file itself
				// is a media frame, and no answer text is sent. Counted by
				// frame rather than by wording, so it holds in whatever
				// language the notice is written.
				notices := len(markdownSends(t, r.conn))
				if notices != perm.notice {
					t.Errorf("%d notices to the user, want %d", notices, perm.notice)
				}
				if perm.notice == 0 {
					if n := len(r.conn.cmdFrames(cmdSendMsg)); n != 0 {
						t.Errorf("%d send frames after the permission was gone, want 0", n)
					}
				}
			})
		}
	}
}

// A delivery whose permission cannot be read from the start writes nothing at
// all: not the upload's first frame either.
func TestSendAttachments_AnUnreadablePermissionWritesNothingFromTheStart(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, []byte("DATA"))
	r.q.unreadable.Store(true)
	r.mustFinish(t, r.run(t))
	if n := frameCount(r.conn); n != 0 {
		t.Errorf("%d frames reached the socket with an unreadable permission, want 0", n)
	}
}

// Wiring, not behaviour: the guard exists to be installed by the production
// worker, and the tests above install nothing themselves — they call
// sendAttachments, which is what the spawned goroutine calls. This pins the
// last step: that the spawn in deliverAttachmentsByID reaches it.
func TestDeliverAttachments_TheSpawnedWorkerIsGuarded(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, []byte("DATA"))
	r.q.revoked.Store(true)
	r.o.deliverAttachmentsByID(testMessageID, testWorkspaceID, r.to, true)
	if n := frameCount(r.conn); n != 0 {
		t.Errorf("%d frames reached the socket from the spawned worker with the installation revoked, want 0", n)
	}
	if got := r.mx.get("attachment_dropped"); got != 1 {
		t.Errorf("attachment_dropped = %d, want 1 — the worker must have run for this test to mean anything", got)
	}
}

// The guard runs while the socket's writer is held, so a permission read that
// never answers must end by itself and refuse the frame. Without the bound the
// read would sit on the writer for as long as the caller's context lasts —
// minutes, here — and the ping loop and every other send on this socket would
// queue behind it.
func TestAttachmentWriteGuard_AReadThatNeverAnswersEndsAndRefuses(t *testing.T) {
	t.Parallel()
	r := newRevocationRig(t, []byte("DATA"))
	r.q.stalled.Store(true)

	got := make(chan error, 1)
	go func() { got <- r.o.attachmentWriteGuard(r.q.id)(context.Background()) }() // no deadline of its own
	select {
	case err := <-got:
		if !errors.Is(err, errMediaPermissionWithdrawn) {
			t.Fatalf("guard = %v, want a refusal", err)
		}
	case <-time.After(permissionReadBudget + 2*time.Second):
		t.Fatalf("the permission read was still waiting after %s; the writer it runs under would be held that long", permissionReadBudget+2*time.Second)
	}
}
