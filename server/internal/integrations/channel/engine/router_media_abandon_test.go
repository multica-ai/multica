package engine

// router_media_abandon_test.go — what a person is owed when the photo they
// sent runs out of budget before it is bound.
//
// The Router already handles the budget expiring: it drops the refs, logs a
// warning and lets the run proceed on the "[Image]" placeholder. From the
// resolver's side nothing failed — it was cut off, or it never started — so
// the notice a resolver owns for its own failures cannot fire. The photo is
// gone, the agent answers the line typed beside it as if there were no photo,
// and the only record is a server log the sender cannot read.
//
// The tests here drive the real Router through the real detached media path.
// One is the guard for the channels that have not opted in: the interface is
// optional, and a resolver that does not implement it must come out of this
// path with exactly the behaviour it had before.

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// abandonAwareMedia is a MediaResolver that also implements
// MediaAbandonNotifier — the shape an adapter takes when it can push an
// unsolicited message into the chat the attachment came from.
type abandonAwareMedia struct {
	*fakeMedia
	// block, when set, holds every call until it is closed, whatever the
	// context says: the worst a resolver can do with a chat that is busy or a
	// server that does not answer.
	block <-chan struct{}

	noteMu      sync.Mutex
	notes       int
	liveCtx     bool
	hasDeadline bool
	deadline    time.Time
	noteMsg     channel.InboundMessage
	noteInst    ResolvedInstallation
	noteWhat    MediaAbandonment
}

func (m *abandonAwareMedia) NotifyMediaAbandoned(ctx context.Context, inst ResolvedInstallation, msg channel.InboundMessage, a MediaAbandonment) {
	m.noteMu.Lock()
	m.notes++
	// Recorded, not asserted here: a notice handed the context that just
	// expired cannot be written to the chat, which would make the whole call
	// ceremonial.
	m.liveCtx = ctx.Err() == nil
	m.deadline, m.hasDeadline = ctx.Deadline()
	m.noteInst = inst
	m.noteMsg = msg
	m.noteWhat = a
	block := m.block
	m.noteMu.Unlock()
	if block != nil {
		<-block
	}
}

func (m *abandonAwareMedia) abandonment() MediaAbandonment {
	m.noteMu.Lock()
	defer m.noteMu.Unlock()
	return m.noteWhat
}

func (m *abandonAwareMedia) observed() (int, bool, ResolvedInstallation, channel.InboundMessage) {
	m.noteMu.Lock()
	defer m.noteMu.Unlock()
	return m.notes, m.liveCtx, m.noteInst, m.noteMsg
}

// registerMediaSet re-registers the harness's resolver set with a different
// media resolver, leaving every other seam as newHarness built it.
func registerMediaSet(h *harness, media MediaResolver) {
	h.router.Register(channel.TypeFeishu, ResolverSet{
		Installation: h.inst,
		Identity:     h.ident,
		Dedup:        h.dedup,
		Session:      h.binder,
		Audit:        h.audit,
		Replier:      h.replier,
		Typing:       h.typing,
		Media:        media,
		OriginType:   "lark_chat",
	})
}

// TestRouter_MediaBudgetExpiredTellsAResolverThatCanSaySo is the defect. The
// budget runs out while ResolveMedia is still working, so the resolver returns
// having observed no failure of its own; without this call nothing anywhere
// tells the person who sent the attachment that it did not arrive.
func TestRouter_MediaBudgetExpiredTellsAResolverThatCanSaySo(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: 10 * time.Millisecond, Logger: discardLogger()})
	h.media.waitForCancel = true
	media := &abandonAwareMedia{fakeMedia: h.media}
	registerMediaSet(h, media)

	msg := p2pMessage(t)
	if err := h.router.Handle(context.Background(), msg); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { n, _, _, _ := media.observed(); return n > 0 }) {
		t.Fatal("the media budget expired, the refs were dropped, and the resolver was never told: " +
			"nothing on this path can tell the sender their attachment did not arrive, " +
			"so the bot answers the line they typed beside the picture as if there were no picture")
	}
	notes, liveCtx, inst, gotMsg := media.observed()
	if notes != 1 {
		t.Fatalf("the resolver was told %d times about one abandoned message; the sender reads one apology per notice", notes)
	}
	if !liveCtx {
		t.Fatal("the notice was handed the context that had just expired — nothing can be written to the chat on it, " +
			"so the sender is told nothing after all")
	}
	if !inst.ID.Valid {
		t.Fatal("the notice carries no installation, so the resolver cannot work out which socket to write to")
	}
	if gotMsg.MessageID != msg.MessageID {
		t.Fatalf("the notice carries message %q, want %q — the resolver needs the original message to find the chat and the attachments",
			gotMsg.MessageID, msg.MessageID)
	}
	// The refs are still dropped and the run still proceeds on the
	// placeholder. Telling the sender is an addition to that outcome, not a
	// replacement for it.
	if refs := h.binder.boundMedia().MediaRefs; len(refs) != 0 {
		t.Fatalf("timed-out media refs must still not attach: %+v", refs)
	}
}

// TestRouter_MediaBudgetExpiredLeavesAResolverThatCannotNotifyAlone is the
// blast-radius guard. MediaAbandonNotifier is shared engine code, and lark and
// dingtalk hand the Router resolvers that do not implement it. For them the
// type assertion must simply miss and the expiry path must end exactly where
// it ended before: refs dropped, message still ingested, placeholder task
// still promoted, no panic, no extra call to anything.
func TestRouter_MediaBudgetExpiredLeavesAResolverThatCannotNotifyAlone(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: 10 * time.Millisecond, Logger: discardLogger()})
	h.media.waitForCancel = true
	// fakeMedia is the sibling shape: HasMedia + ResolveMedia and nothing else.
	if _, ok := MediaResolver(h.media).(MediaAbandonNotifier); ok {
		t.Fatal("this test is only a guard while the plain resolver does NOT implement the notifier")
	}
	registerMediaSet(h, h.media)

	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { return h.media.calls() == 1 }) {
		t.Fatalf("media resolver calls = %d, want 1", h.media.calls())
	}
	if refs := h.binder.boundMedia().MediaRefs; len(refs) != 0 {
		t.Fatalf("timed-out media refs must not attach: %+v", refs)
	}
	if !waitFor(2*time.Second, h.tasks.wasCalled) {
		t.Fatal("a channel that cannot send a notice lost its chat run: the message must still be ingested")
	}
	if !waitFor(2*time.Second, func() bool { return h.tasks.promotionCalls() >= 1 }) {
		t.Fatal("a channel that cannot send a notice lost its placeholder promotion")
	}
	if h.dedup.releases() != 0 {
		t.Fatalf("media timeout must not release a durably appended message, got %d", h.dedup.releases())
	}
}

// TestRouter_AnAbandonNoticeThatNeverReturnsDoesNotHoldUpFinalization is the
// latency guard. The notice is a chat write and can wait on a profile lookup,
// the chat's turn and the server's acknowledgement; the bind, the clearing of
// the pending-media marker and the task promotion must not wait behind it,
// nor run on a budget it has spent. The notifier here never returns until the
// test lets it, whatever its context says.
//
// REVERSE VERIFICATION: call NotifyMediaAbandoned inline on finalizeCtx, as
// before, and the bind and promotion never happen while the notice is stuck.
func TestRouter_AnAbandonNoticeThatNeverReturnsDoesNotHoldUpFinalization(t *testing.T) {
	h := newHarness(t)
	h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: 10 * time.Millisecond, Logger: discardLogger()})
	h.media.waitForCancel = true
	stuck := make(chan struct{})
	var unstick sync.Once
	t.Cleanup(func() { unstick.Do(func() { close(stuck) }) })
	media := &abandonAwareMedia{fakeMedia: h.media, block: stuck}
	registerMediaSet(h, media)

	asked := time.Now()
	if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if !waitFor(2*time.Second, func() bool { n, _, _, _ := media.observed(); return n > 0 }) {
		t.Fatal("the resolver was never told the budget ran out")
	}
	if !waitFor(2*time.Second, func() bool { return h.binder.boundMedia().MessageID.Valid }) {
		t.Fatal("the message was never bound while the notice was stuck: its pending-media marker stays set " +
			"and every later message in the session waits on it")
	}
	if !waitFor(2*time.Second, func() bool { return h.tasks.promotionCalls() >= 1 }) {
		t.Fatal("the task was never promoted while the notice was stuck: the agent does not run until recovery finds it")
	}

	media.noteMu.Lock()
	hasDeadline, deadline := media.hasDeadline, media.deadline
	media.noteMu.Unlock()
	if !hasDeadline {
		t.Fatal("the notice was handed a context with no deadline: a chat that never answers holds its goroutine forever")
	}
	if limit := asked.Add(mediaAbandonNoticeTimeout + time.Second); deadline.After(limit) {
		t.Fatalf("the notice's deadline %s is past the %s bound", deadline.Sub(asked), mediaAbandonNoticeTimeout)
	}

	// Drain counts the notice as media work in flight: it reports the stuck
	// one, and finishes once it returns.
	shortCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if h.router.Drain(shortCtx) {
		t.Fatal("Drain reported everything finished while the abandon notice was still running")
	}
	unstick.Do(func() { close(stuck) })
	if !h.router.Drain(context.Background()) {
		t.Fatal("Drain did not finish after the notice returned")
	}
}

// TestRouter_TellsTheResolverWhatItDroppedAndWhetherItRan carries the two
// facts only the Router has. When the budget runs out mid-way, the refs
// ResolveMedia did return are discarded after it has returned, so it cannot
// count them; when the budget is gone before the job comes up, ResolveMedia is
// never called, so it saw none of the attachments. Without these the resolver
// either misses attachments or counts the ones it already counted.
//
// REVERSE VERIFICATION: pass a MediaAbandonment with only Cause set and the
// cut-off case fails. (The never-started case is the zero value on both
// fields, so it pins the Router not to report a run that did not happen.)
func TestRouter_TellsTheResolverWhatItDroppedAndWhetherItRan(t *testing.T) {
	t.Run("cut off after one ref landed", func(t *testing.T) {
		h := newHarness(t)
		h.router = NewRouter(h.issues, h.tasks, h.reader, RouterConfig{MediaTimeout: 20 * time.Millisecond, Logger: discardLogger()})
		h.media.resolve = func(ctx context.Context, msg channel.InboundMessage) channel.InboundMessage {
			msg.MediaRefs = append(msg.MediaRefs, channel.MediaRef{Type: channel.MsgTypeImage, StorageKey: "landed-in-time"})
			<-ctx.Done()
			return msg
		}
		media := &abandonAwareMedia{fakeMedia: h.media}
		registerMediaSet(h, media)

		if err := h.router.Handle(context.Background(), p2pMessage(t)); err != nil {
			t.Fatalf("Handle: %v", err)
		}
		if !waitFor(2*time.Second, func() bool { n, _, _, _ := media.observed(); return n > 0 }) {
			t.Fatal("the resolver was never told the budget ran out")
		}
		a := media.abandonment()
		if !a.Started {
			t.Error("Started = false, but ResolveMedia ran: the resolver would count again what it already counted")
		}
		if len(a.Dropped) != 1 || a.Dropped[0].StorageKey != "landed-in-time" {
			t.Errorf("Dropped = %+v, want the one ref ResolveMedia returned", a.Dropped)
		}
		if !errors.Is(a.Cause, context.DeadlineExceeded) {
			t.Errorf("Cause = %v, want context.DeadlineExceeded", a.Cause)
		}
		if refs := h.binder.boundMedia().MediaRefs; len(refs) != 0 {
			t.Fatalf("the dropped ref was bound anyway: %+v", refs)
		}
	})

	t.Run("never started", func(t *testing.T) {
		h := newHarness(t)
		media := &abandonAwareMedia{fakeMedia: h.media}
		h.router.resolveAndBindMedia(
			ResolverSet{Session: h.binder, Media: media},
			h.inst.inst, h.ident.id,
			uuidFromString(t, "77777777-7777-4777-8777-777777777777"),
			p2pMessage(t), h.binder.ensureID,
			db.Issue{}, pgtype.Text{}, "", pgtype.UUID{},
			true, time.Now().Add(-time.Second),
		)
		if !waitFor(2*time.Second, func() bool { n, _, _, _ := media.observed(); return n > 0 }) {
			t.Fatal("the resolver was never told the budget ran out before it started")
		}
		if h.media.calls() != 0 {
			t.Fatalf("ResolveMedia ran %d time(s) on a budget already spent", h.media.calls())
		}
		a := media.abandonment()
		if a.Started {
			t.Error("Started = true, but ResolveMedia never ran: the resolver would count none of the attachments")
		}
		if len(a.Dropped) != 0 {
			t.Errorf("Dropped = %+v, want none", a.Dropped)
		}
		if !errors.Is(a.Cause, context.DeadlineExceeded) {
			t.Errorf("Cause = %v, want context.DeadlineExceeded", a.Cause)
		}
	})
}

// noticeDrainingMedia is a MediaResolver that sends its own failure notices on
// goroutines and exposes them to Drain, as the WeCom resolver does. Its one
// notice is held until the test lets go.
type noticeDrainingMedia struct {
	*fakeMedia
	held <-chan struct{}
}

func (m *noticeDrainingMedia) DrainMediaNotices() { <-m.held }

// TestRouter_DrainWaitsForAResolversOwnNotices: a resolver that moved its
// notice off the bind path still has it waited for at shutdown, as it was
// when the notice was sent inline inside the media goroutine. The WeCom
// package drives the same thing through the real resolver and a real
// database (TestDrainWaitsForTheResolversOwnNotice).
//
// REVERSE VERIFICATION: drop the DrainMediaNotices loop from Drain and the
// first Drain reports done.
func TestRouter_DrainWaitsForAResolversOwnNotices(t *testing.T) {
	h := newHarness(t)
	held := make(chan struct{})
	var letGo sync.Once
	t.Cleanup(func() { letGo.Do(func() { close(held) }) })
	registerMediaSet(h, &noticeDrainingMedia{fakeMedia: h.media, held: held})

	short, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if h.router.Drain(short) {
		t.Fatal("Drain reported everything finished while a resolver's notice was still in flight")
	}
	letGo.Do(func() { close(held) })
	if !h.router.Drain(context.Background()) {
		t.Fatal("Drain did not finish after the resolver's notices did")
	}
}
