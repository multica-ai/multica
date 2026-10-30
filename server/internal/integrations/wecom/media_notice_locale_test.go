package wecom

// media_notice_locale_test.go — the attachment-failure notice is written in
// the language of the person it lands in front of, a profile lookup that
// never answers delays it by its own small budget and no more, and nothing
// the notice waits on holds up the caller or outlives the context it is given.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// TestMediaFailureNoticeReadsTheSendersLanguage drives the whole ingest, not
// tellTheSender directly: the notice runs after the download has already
// failed, on a context the caller may well have let expire, and resolving the
// language is the part of that path most likely to be skipped by accident.
func TestMediaFailureNoticeReadsTheSendersLanguage(t *testing.T) {
	t.Parallel()
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	for _, tc := range localeCases {
		t.Run(tc.name, func(t *testing.T) {
			storage := &fakeMediaStorage{}
			senders, conn := notifierWithLiveSocket(uuidOf(1))
			// mediaMessage sends as T-alex in a 1:1, so the notice is
			// addressed to one person and reads their profile.
			r := NewMediaResolver(storage, newFakeMediaLedger(storage), senders,
				activeInstallation(fakeLanguages{senderID: "T-alex", userID: localeTestUserID, language: tc.language}),
				testLogger()).(*wecomMediaResolver)
			r.http = testMediaClient()

			msg := mediaMessage(t, "image", map[string]any{
				"image": map[string]any{"url": expired.URL, "aeskey": testAESKey},
			})
			r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
			r.notices.Wait()

			if got, want := sentMarkdown(t, conn, 0), copyPacks[tc.locale].MediaUnreadable; got != want {
				t.Fatalf("failure notice = %q, want the %s copy %q", got, tc.locale, want)
			}
		})
	}
}

// noticeQueries is the notice's two reads: a language lookup, and the
// installation row replyWriteGuard reads as the notice is written. The row is
// answered by id, as production answers it: the one installation it holds,
// pgx.ErrNoRows for any other.
type noticeQueries struct {
	languageLookup
	instID  pgtype.UUID
	revoked bool
}

func (q noticeQueries) GetChannelInstallation(_ context.Context, arg db.GetChannelInstallationParams) (db.ChannelInstallation, error) {
	if arg.ID != q.instID || arg.ChannelType != channelTypeWecom {
		return db.ChannelInstallation{}, pgx.ErrNoRows
	}
	status := InstallationActive
	if q.revoked {
		status = InstallationRevoked
	}
	return db.ChannelInstallation{ID: q.instID, Status: string(status)}, nil
}

// activeInstallation pairs a language lookup with mediaInstallation's row,
// still active.
func activeInstallation(languages languageLookup) noticeQueries {
	return noticeQueries{languageLookup: languages, instID: mediaInstallation().ID}
}

// stalledLanguages is a languageLookup that never answers. It stands for the
// case the notice's own budget exists for: the profile row is behind a lock,
// or the pool is drained by whatever also broke the download.
type stalledLanguages struct{}

func (stalledLanguages) GetChannelUserBindingByUserID(ctx context.Context, _ db.GetChannelUserBindingByUserIDParams) (db.ChannelUserBinding, error) {
	<-ctx.Done()
	return db.ChannelUserBinding{}, ctx.Err()
}

func (stalledLanguages) GetUser(ctx context.Context, _ pgtype.UUID) (db.User, error) {
	<-ctx.Done()
	return db.User{}, ctx.Err()
}

// A lookup that never answers must not cost the notice: it goes out once the
// lookup's budget is spent, in the deployment's language, rather than being
// dropped along with the attachment that already failed.
//
// The lower bound on the wait is what keeps this honest — a notice that went
// out sooner did not wait on the lookup at all, and then nothing here was
// tested.
func TestMediaFailureNoticeSurvivesAStalledLookup(t *testing.T) {
	restoreLocale(t, LocaleEn)

	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	storage := &fakeMediaStorage{}
	senders, conn := notifierWithLiveSocket(uuidOf(1))
	r := NewMediaResolver(storage, newFakeMediaLedger(storage), senders, activeInstallation(stalledLanguages{}), testLogger()).(*wecomMediaResolver)
	r.http = testMediaClient()

	msg := mediaMessage(t, "image", map[string]any{
		"image": map[string]any{"url": expired.URL, "aeskey": testAESKey},
	})

	started := time.Now()
	r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
	r.notices.Wait()
	waited := time.Since(started)

	if got, want := sentMarkdown(t, conn, 0), copyPacks[LocaleEn].MediaUnreadable; got != want {
		t.Fatalf("notice = %q, want the deployment's %q — a lookup that never answers must not change what is said, only who it is said to", got, want)
	}
	if waited < mediaNoticeLocaleTimeout {
		t.Errorf("the notice went out in %s, before the %s budget was spent — the lookup was not actually waited on, so this proves nothing", waited, mediaNoticeLocaleTimeout)
	}
	if waited > 4*mediaNoticeLocaleTimeout {
		t.Errorf("the notice took %s against a %s budget; something other than the lookup is holding it", waited, mediaNoticeLocaleTimeout)
	}
}

// The notice ResolveMedia sends about its own failures must not hold up its
// return. The Router binds the message, clears its pending-media marker and
// promotes its task only once ResolveMedia returns, and a notice waiting for
// the chat's turn — here, behind an answer that holds the chat — would spend
// that time and, past the deadline, the refs that did arrive.
//
// REVERSE VERIFICATION: call tellTheSender inline instead of on its goroutine
// and ResolveMedia does not return until the chat is released.
func TestResolveMediaDoesNotWaitForItsNotice(t *testing.T) {
	t.Parallel()
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	storage := &fakeMediaStorage{}
	senders, conn := notifierWithLiveSocket(uuidOf(1))
	r := newTestResolver(storage, newFakeMediaLedger(storage), senders)
	release, err := conn.sender.chats.acquire(context.Background(), "T-alex")
	if err != nil {
		t.Fatalf("hold the chat: %v", err)
	}

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5),
			mediaMessage(t, "image", map[string]any{"image": map[string]any{"url": expired.URL, "aeskey": testAESKey}}))
	}()
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		release()
		<-returned
		t.Fatal("ResolveMedia waited for its failure notice to get the chat's turn; the Router cannot bind the " +
			"message or promote its task until it returns")
	}

	release()
	r.notices.Wait()
	if got, want := sentMarkdown(t, conn, 0), copyFor(DefaultLocale).MediaUnreadable; got != want {
		t.Fatalf("the notice, once the chat was free, read %q, want %q", got, want)
	}
}

// ResolveMedia's notice is off the bind path, and the Router's Drain still
// waits for it, as it did when the notice was sent inline: a deploy must not
// cut it off mid-send. Real Router, real wecom ResolverSet, real database; the
// notice is held behind the chat's turn, and the media job has finished (the
// promotion ran) before Drain is asked.
//
// REVERSE VERIFICATION: drop the DrainMediaNotices loop from Router.Drain and
// Drain reports everything finished while the notice is still waiting.
func TestDrainWaitsForTheResolversOwnNotice(t *testing.T) {
	pool := mediaBindTestDB(t)
	fixture := seedMediaBindFixture(t, pool)
	ctx := context.Background()
	queries := db.New(pool)

	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()

	senders, conn := liveSocketThatAcks(fixture.installationID)
	release, err := conn.sender.chats.acquire(ctx, fixture.senderID)
	if err != nil {
		t.Fatalf("hold the chat: %v", err)
	}
	var once sync.Once
	letGo := func() { once.Do(release) }
	t.Cleanup(letGo)

	storage := &fakeMediaStorage{}
	resolver := newTestResolver(storage, engine.NewDBMediaIntentLedger(queries), senders)
	session := engine.NewChatSession(queries, pool, TypeWecom, engine.SessionTitles{
		Group: "群聊", Direct: "单聊", Fallback: "会话",
	})
	tasks := &bindTestTasks{}
	router := engine.NewRouter(bindTestIssues{}, tasks, queries, engine.RouterConfig{
		MediaTimeout: 20 * time.Second,
		Logger:       testLogger(),
	})
	router.Register(TypeWecom, NewResolverSet(NewStore(queries), session, nil, resolver))

	msgID := fmt.Sprintf("MSGID-DRAIN-%d", time.Now().UnixNano())
	if err := router.Handle(ctx, wecomImageCallback(t, fixture.botID, fixture.senderID, msgID, expired.URL)); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	deadline := time.Now().Add(15 * time.Second)
	for tasks.promotions() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the media job never finished: the notice held up the bind and the promotion")
		}
		time.Sleep(20 * time.Millisecond)
	}

	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()
	if router.Drain(short) {
		t.Fatal("Drain reported everything finished while the resolver's failure notice was still waiting " +
			"for the chat: a deploy at this moment cuts the notice off mid-send")
	}

	letGo()
	full, cancelFull := context.WithTimeout(ctx, 10*time.Second)
	defer cancelFull()
	if !router.Drain(full) {
		t.Fatal("Drain did not finish after the notice got the chat")
	}
	if said := noticesOn(t, conn); len(said) != 1 || said[0] != copyFor(DefaultLocale).MediaUnreadable {
		t.Fatalf("notices on the socket = %q, want the one did-not-arrive line", said)
	}
}

// NotifyMediaAbandoned runs on the context the Router gives it, and nothing
// inside may outlive that: not the profile lookup, not the wait for the
// server's acknowledgement. Each is tried alone against a context far shorter
// than its own fallback — two seconds for the lookup, five for the ack — so
// one that ignored the context would run to that fallback.
//
// REVERSE VERIFICATION: derive the lookup's context from context.Background()
// and "a lookup that never answers" takes two seconds; write with sendText
// (context.Background()) and "an ack that never comes" takes five.
func TestTheAbandonNoticeStaysInsideTheContextItIsGiven(t *testing.T) {
	t.Parallel()
	const budget = 300 * time.Millisecond
	msg := mediaMessage(t, "image", map[string]any{"image": map[string]any{"url": "https://cos.example/x", "aeskey": testAESKey}})

	for _, tc := range []struct {
		name      string
		languages languageLookup
		ack       bool
		// frames is what the socket sees. A stalled lookup spends the whole
		// context, so nothing is left to write with; a missing ack comes after
		// the frame is already out.
		frames int
	}{
		{"a lookup that never answers", stalledLanguages{}, true, 0},
		{"an ack that never comes", fakeLanguages{}, false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conn := &recordingConn{}
			sender := newWSSender(conn, testLogger())
			if tc.ack {
				sender = conn.autoAck(sender)
			}
			senders := newSendersRegistry()
			senders.set(uuidOf(1), sender)
			storage := &fakeMediaStorage{}
			r := NewMediaResolver(storage, newFakeMediaLedger(storage), senders,
				activeInstallation(tc.languages), testLogger()).(*wecomMediaResolver)

			ctx, cancel := context.WithTimeout(context.Background(), budget)
			defer cancel()
			started := time.Now()
			r.NotifyMediaAbandoned(ctx, mediaInstallation(), msg, engine.MediaAbandonment{Cause: context.DeadlineExceeded})
			took := time.Since(started)

			if took >= mediaNoticeLocaleTimeout {
				t.Fatalf("the notice took %s on a %s context: it waited past the context it was given", took, budget)
			}
			conn.mu.Lock()
			frames := len(conn.frames)
			conn.mu.Unlock()
			if frames != tc.frames {
				t.Fatalf("frames written = %d, want %d", frames, tc.frames)
			}
		})
	}
}

// A revoked installation gets no notice. The installation was read when the
// message came in, up to a whole media budget before the notice is written,
// and a person who removed the bot in between is owed nothing more from it.
// Both notices go through the same check: the one ResolveMedia sends about
// its own failures, and the one the Router asks for when its budget runs out.
//
// REVERSE VERIFICATION: drop guardReply from tellTheSender and both cases
// write a frame.
func TestNoMediaNoticeIsWrittenToARevokedInstallation(t *testing.T) {
	t.Parallel()
	expired := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer expired.Close()
	msg := mediaMessage(t, "image", map[string]any{"image": map[string]any{"url": expired.URL, "aeskey": testAESKey}})

	for _, tc := range []struct {
		name   string
		notify func(r *wecomMediaResolver)
	}{
		{"its own failure", func(r *wecomMediaResolver) {
			r.ResolveMedia(context.Background(), mediaInstallation(), engine.ResolvedIdentity{}, uuidOf(6), uuidOf(5), msg)
			r.notices.Wait()
		}},
		{"the budget's", func(r *wecomMediaResolver) {
			r.NotifyMediaAbandoned(context.Background(), mediaInstallation(), msg, engine.MediaAbandonment{Cause: context.DeadlineExceeded})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			storage := &fakeMediaStorage{}
			senders, conn := notifierWithLiveSocket(uuidOf(1))
			q := activeInstallation(fakeLanguages{})
			q.revoked = true
			r := NewMediaResolver(storage, newFakeMediaLedger(storage), senders, q, testLogger()).(*wecomMediaResolver)
			r.http = testMediaClient()

			tc.notify(r)

			conn.mu.Lock()
			defer conn.mu.Unlock()
			if len(conn.frames) != 0 {
				t.Fatalf("%d frame(s) written to an installation revoked since the message came in", len(conn.frames))
			}
		})
	}
}
