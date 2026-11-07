package wecom

// regression_media_budget_silence_test.go — what a person is owed when the
// photo they sent does not make it.
//
// The adapter already apologises for an attachment the resolver itself failed
// on: a dead COS link, a file over the size cap. But the resolver is not the
// only thing that can give up on an attachment. The engine Router wraps the
// whole ResolveMedia call in a media budget of its own, and when that budget
// runs out the Router drops the attachment references and lets the agent run
// on the "[Image]" placeholder. From the resolver's side nothing failed — it
// was cut off, or it never started — so the notice it owns is never written.
// The photo is gone and the chat says nothing about it: the person watches
// the bot answer the sentence they typed beside the picture as if the picture
// had never existed, and the only place it is recorded is a server log they
// cannot see.
//
// The tests here drive the two calls engine.Router.resolveAndBindMedia makes,
// in its order and with its contexts, against the real resolver over a real
// aibot socket. What they read is what the person reads: the frames on the
// socket.

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
)

// liveSocketThatAcks is notifierWithLiveSocket with the server's ack wired
// back, so a notice completes at once instead of waiting out the ack timeout.
func liveSocketThatAcks(id pgtype.UUID) (*sendersRegistry, *recordingConn) {
	conn := &recordingConn{}
	reg := newSendersRegistry()
	reg.set(id, conn.autoAck(newWSSender(conn, testLogger())))
	return reg, conn
}

// noticesOn returns the markdown text of every message pushed to the chat.
func noticesOn(t *testing.T, conn *recordingConn) []string {
	t.Helper()
	conn.mu.Lock()
	n := len(conn.frames)
	conn.mu.Unlock()
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		body := conn.sendBody(t, i)
		md, ok := body["markdown"].(map[string]any)
		if !ok {
			t.Fatalf("frame %d has no markdown body: %#v", i, body)
		}
		text, _ := md["content"].(string)
		out = append(out, text)
	}
	return out
}

// asTheRouterDoes runs the two calls engine.Router.resolveAndBindMedia makes,
// with what it hands them: resolve under the media budget, and, when that
// budget is gone, tell the resolver it was abandoned, with the refs it is
// dropping, on a FRESH bounded context, because the one that just expired
// cannot carry a message out. It returns once both notices have finished.
//
// deadline in the past is the "never started" case: the Router skips
// ResolveMedia entirely rather than churn through intent writes on a dead
// context, which is exactly why the resolver observes no failure.
func asTheRouterDoes(t *testing.T, r *wecomMediaResolver, deadline time.Time, inst engine.ResolvedInstallation, msg channel.InboundMessage) {
	t.Helper()
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	resolved, started := msg, false
	if ctx.Err() == nil {
		resolved = r.ResolveMedia(ctx, inst, engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
		started = true
	}
	budgetErr := ctx.Err()
	cancel()
	if budgetErr == nil {
		t.Fatal("the media budget did not expire; this test says nothing unless it does")
	}
	notifier, ok := engine.MediaResolver(r).(engine.MediaAbandonNotifier)
	if !ok {
		t.Fatal("the wecom resolver does not implement engine.MediaAbandonNotifier, so when the Router's " +
			"media budget expires nothing tells the sender their attachment did not arrive: the bot answers " +
			"the line they typed beside the picture as if there were no picture")
	}
	noticeCtx, noticeCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer noticeCancel()
	notifier.NotifyMediaAbandoned(noticeCtx, inst, msg, engine.MediaAbandonment{
		Cause: budgetErr, Started: started, Dropped: resolved.MediaRefs,
	})
	r.notices.Wait()
}

// stallingCOS serves a real encrypted payload, but only after hold. It is the
// ordinary shape of a file slower than the budget, not an exotic one.
func stallingCOS(t *testing.T, hold time.Duration, plaintext []byte) *httptest.Server {
	t.Helper()
	ciphertext := encryptLikeWecom(t, testAESKey, plaintext)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(hold)
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write(ciphertext)
	}))
}

// TestAPhotoTheMediaBudgetGaveUpOnBeforeItStartedIsNotSilentlyDropped: the
// budget is already spent by the time the job comes up, so the download never
// even starts. The resolver observes no failure because it is never asked to
// do anything — which is exactly why the notice it owns cannot fire, and why
// this has to be told to it from outside.
func TestAPhotoTheMediaBudgetGaveUpOnBeforeItStartedIsNotSilentlyDropped(t *testing.T) {
	t.Parallel()
	cos := cosServer(t, []byte("\x89PNG\r\n\x1a\nthe photo they wanted looked at"), `attachment; filename="chart.png"`)
	defer cos.Close()

	senders, conn := liveSocketThatAcks(uuidOf(1))
	r := newTestResolver(&fakeMediaStorage{}, newFakeMediaLedger(nil), senders)
	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": cos.URL, "aeskey": testAESKey},
	})

	// A budget already gone is what a real 45s budget looks like behind a
	// burst of earlier attachments queued on the same session.
	asTheRouterDoes(t, r, time.Now().Add(-time.Millisecond), mediaInstallation(), msg)

	said := noticesOn(t, conn)
	if len(said) == 0 {
		t.Fatal("the photo never reached the chat message and the chat was told nothing about it: " +
			"the person sees the bot answer the line they typed beside the picture as if there were no " +
			"picture, with nothing to tell them to send it again")
	}
	if len(said) != 1 {
		t.Fatalf("the sender got %d notices for one abandoned message: %q", len(said), said)
	}
	if want := copyFor(DefaultLocale).MediaUnreadable; said[0] != want {
		t.Fatalf("the notice reads %q, want the line that tells them to send it again (%q)", said[0], want)
	}
}

// TestAnAttachmentCutOffByTheBudgetIsApologisedForOnce is the overlap: a file
// slower than the budget. The download is cut off mid-flight, so the resolver
// classifies it as unreadable and would say so; then the Router, having given
// up on the same message, says the identical sentence. Two in a row reads as
// the bot glitching rather than as one picture that did not make it.
func TestAnAttachmentCutOffByTheBudgetIsApologisedForOnce(t *testing.T) {
	t.Parallel()
	cos := stallingCOS(t, 400*time.Millisecond, []byte("\x89PNG\r\n\x1a\na slow upload"))
	defer cos.Close()

	senders, conn := liveSocketThatAcks(uuidOf(1))
	r := newTestResolver(&fakeMediaStorage{}, newFakeMediaLedger(nil), senders)
	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": cos.URL, "aeskey": testAESKey},
	})

	asTheRouterDoes(t, r, time.Now().Add(80*time.Millisecond), mediaInstallation(), msg)

	said := noticesOn(t, conn)
	if len(said) == 0 {
		t.Fatal("the sender was told nothing at all about a photo that never made it")
	}
	if len(said) > 1 {
		t.Fatalf("the sender got %d notices for one failed attachment: %q — they read the same apology twice in a row", len(said), said)
	}
}

// TestTheVerdictTheSenderCanActOnSurvivesTheBudget is the carve-out. A "too
// big" verdict was reached on its own merits before the clock ran out, and it
// is the one line the sender can do something about (send a smaller file), so
// dropping the repeated line must not drop it too.
func TestTheVerdictTheSenderCanActOnSurvivesTheBudget(t *testing.T) {
	t.Parallel()
	got := withoutNotice([]mediaFailure{mediaFailureUnreadable, mediaFailureTooLarge}, mediaFailureUnreadable)
	if len(got) != 1 || got[0] != mediaFailureTooLarge {
		t.Fatalf("kept %v, want the too-large verdict to survive — it is the only one the sender can act on", got)
	}
	if left := withoutNotice([]mediaFailure{mediaFailureUnreadable}, mediaFailureUnreadable); len(left) != 0 {
		t.Fatalf("kept %v, want nothing: the Router is about to say that line itself", left)
	}
	// A refused address reads to the sender as "it did not arrive", the same
	// sentence the Router repeats, so it has to be dropped by which sentence
	// it would say rather than by which kind it is.
	if left := withoutNotice([]mediaFailure{mediaFailureBlocked}, mediaFailureUnreadable); len(left) != 0 {
		t.Fatalf("kept %v, want nothing: a blocked address says the same sentence the Router repeats", left)
	}
	if kept := withoutNotice([]mediaFailure{mediaFailureTooLarge}, mediaFailureUnreadable); len(kept) != 1 {
		t.Fatalf("kept %v, want the unrelated verdict untouched", kept)
	}
}

// TestAnAbandonedMessageWithNoAttachmentsSaysNothing: the Router's budget
// covers the whole detached path, and a message can reach it having had its
// attachments decoded away. Apologising for a picture nobody sent is its own
// small defect.
func TestAnAbandonedMessageWithNoAttachmentsSaysNothing(t *testing.T) {
	t.Parallel()
	senders, conn := liveSocketThatAcks(uuidOf(1))
	r := newTestResolver(&fakeMediaStorage{}, newFakeMediaLedger(nil), senders)
	notifier, ok := engine.MediaResolver(r).(engine.MediaAbandonNotifier)
	if !ok {
		t.Fatal("the wecom resolver does not implement engine.MediaAbandonNotifier")
	}

	notifier.NotifyMediaAbandoned(context.Background(), mediaInstallation(), mediaMessage(t, "text", map[string]any{
		"text": map[string]any{"content": "just a question"},
	}), engine.MediaAbandonment{Cause: context.DeadlineExceeded})

	if said := noticesOn(t, conn); len(said) != 0 {
		t.Fatalf("a message carrying no attachments was apologised for anyway: %q", said)
	}
}
