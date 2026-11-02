package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/testutil"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// briefPushTestChat is the chat app the fixture registers its notifier under.
const briefPushTestChat = channel.Type("telegram")

// fakeBriefNotifier stands in for the platform. It records each message and
// answers with the errors it is given, in order, then with err.
type fakeBriefNotifier struct {
	mu    sync.Mutex
	sent  []briefSend
	errs  []error
	err   error
	calls atomic.Int32
}

type briefSend struct {
	binding channel.Binding
	msg     channel.Message
}

func (f *fakeBriefNotifier) Send(_ context.Context, b channel.Binding, m channel.Message) error {
	f.calls.Add(1)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, briefSend{binding: b, msg: m})
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return err
	}
	return f.err
}

var briefPushSeq atomic.Int64

// briefPushFixture is a workspace with an owner, a service on a settable clock,
// and a fake notifier. Rows are inserted through testutil, which registers
// their cleanup.
type briefPushFixture struct {
	t        *testing.T
	pool     *pgxpool.Pool
	q        *db.Queries
	fx       *testutil.Fixture
	ws       string
	owner    string
	slug     string
	svc      *BriefPushService
	notifier *fakeBriefNotifier
	clock    time.Time
	n        int64
}

// briefPushStart is the test clock's starting instant: 09:00 UTC on a weekday.
var briefPushStart = time.Date(2026, 1, 10, 9, 0, 0, 0, time.UTC)

func newBriefPushFixture(t *testing.T) *briefPushFixture {
	t.Helper()
	pool := sharedTestPool(t)
	n := briefPushSeq.Add(1)
	fx := testutil.New(pool, "", "")
	owner := fx.User(t, "boss", fmt.Sprintf("brief-push-boss-%d@multica.test", n))
	slug := fmt.Sprintf("brief-push-ws-%d", n)
	ws := fx.Workspace(t, "brief push ws", slug)
	fx.Member(t, ws, owner, "owner")
	fx.WorkspaceID = ws
	fx.UserID = owner

	f := &briefPushFixture{
		t: t, pool: pool, q: db.New(pool), fx: fx,
		ws: ws, owner: owner, slug: slug, notifier: &fakeBriefNotifier{},
		clock: briefPushStart, n: n,
	}
	f.svc = NewBriefPushService(f.q, "https://app.example")
	f.svc.now = func() time.Time { return f.clock }
	f.svc.RegisterNotifier(briefPushTestChat, f.notifier)
	return f
}

// member adds a workspace member with a bound Telegram chat. The chat id is the
// binding's channel_user_id, as the Telegram binding stores it. extra overrides
// the user row (a timezone, for example).
func (f *briefPushFixture) member(extra testutil.Cols) (userID, bindingID string) {
	f.t.Helper()
	seq := briefPushSeq.Add(1)
	userID = f.fx.User(f.t, fmt.Sprintf("member %d", seq), fmt.Sprintf("brief-push-member-%d@multica.test", seq), extra)
	f.fx.Member(f.t, f.ws, userID, "member")
	bindingID = f.binding(userID, fmt.Sprintf("%d", 4000+seq))
	return userID, bindingID
}

// binding inserts a Telegram binding for userID with the given chat id.
func (f *briefPushFixture) binding(userID, chatID string) string {
	f.t.Helper()
	return f.fx.Insert(f.t, "channel_user_binding", testutil.Cols{
		"workspace_id":    f.ws,
		"multica_user_id": userID,
		"installation_id": uuid.NewString(),
		"channel_type":    string(briefPushTestChat),
		"channel_user_id": chatID,
	})
}

// preference writes a member's preference row.
func (f *briefPushFixture) preference(userID string, push bool, bindingID string, quietStart, quietEnd *int) {
	f.t.Helper()
	cols := testutil.Cols{
		"workspace_id": f.ws,
		"member_id":    userID,
		"brief_push":   push,
	}
	if bindingID != "" {
		cols["binding_id"] = bindingID
	}
	if quietStart != nil && quietEnd != nil {
		cols["quiet_hours_start"] = int16(*quietStart)
		cols["quiet_hours_end"] = int16(*quietEnd)
	}
	f.fx.Insert(f.t, "brief_push_preference", cols)
}

// proposal files a pending decision for the owner and returns its id.
func (f *briefPushFixture) proposal(title string, snoozedUntil *time.Time) string {
	f.t.Helper()
	issue := f.fx.Issue(f.t, title)
	cols := testutil.Cols{
		"workspace_id":    f.ws,
		"squad_id":        uuid.NewString(),
		"issue_id":        issue,
		"created_by_type": "member",
		"created_by_id":   f.owner,
		"state":           "pending",
	}
	if snoozedUntil != nil {
		cols["snoozed_until"] = *snoozedUntil
	}
	return f.fx.Insert(f.t, "proposal", cols)
}

// brief publishes a brief that raises proposals, in that order, and returns its id.
func (f *briefPushFixture) brief(headline string, proposals ...string) string {
	f.t.Helper()
	ids := make([]pgtype.UUID, 0, len(proposals))
	for _, p := range proposals {
		id, err := util.ParseUUID(p)
		if err != nil {
			f.t.Fatalf("proposal id %q: %v", p, err)
		}
		ids = append(ids, id)
	}
	body := "## Headline\n" + headline + "\n\n## Needs your decision\nSee the decisions.\n\n## Progress\nQuiet.\n\n## Blockers and risks\nNone.\n\n## Watch\nNone.\n\n## FYI\nNone.\n"
	briefID := f.fx.Insert(f.t, "brief", testutil.Cols{
		"workspace_id":         f.ws,
		"kind":                 "daily",
		"period_start":         briefPushStart.Add(-24 * time.Hour),
		"period_end":           briefPushStart,
		"title":                "Daily brief",
		"body":                 body,
		"proposal_ids":         ids,
		"created_by_member_id": f.owner,
	})
	f.fx.Cleanup(f.t, "DELETE FROM brief_delivery WHERE brief_id = $1", briefID)
	return briefID
}

// enqueue queues the brief for every opted-in member.
func (f *briefPushFixture) enqueue(briefID string) int {
	f.t.Helper()
	n, err := f.svc.EnqueueForBrief(context.Background(), mustTestUUID(f.ws), mustTestUUID(briefID))
	if err != nil {
		f.t.Fatalf("EnqueueForBrief: %v", err)
	}
	return n
}

// deliverDue runs one pass of the retry job at the current clock.
func (f *briefPushFixture) deliverDue() int {
	f.t.Helper()
	n, err := f.svc.DeliverDue(context.Background())
	if err != nil {
		f.t.Fatalf("DeliverDue: %v", err)
	}
	return n
}

// deliveryRow is the state of one brief_delivery row as the database holds it.
type deliveryRow struct {
	State         string
	Attempts      int32
	NextAttemptAt time.Time
	LastError     string
	DeliveredAt   pgtype.Timestamptz
}

func (f *briefPushFixture) delivery(briefID, userID string) deliveryRow {
	f.t.Helper()
	var r deliveryRow
	err := f.pool.QueryRow(context.Background(),
		`SELECT state, attempts, next_attempt_at, last_error, delivered_at
		   FROM brief_delivery WHERE brief_id = $1 AND member_id = $2`,
		briefID, userID).Scan(&r.State, &r.Attempts, &r.NextAttemptAt, &r.LastError, &r.DeliveredAt)
	if err != nil {
		f.t.Fatalf("read delivery: %v", err)
	}
	return r
}

func (f *briefPushFixture) pointClock(t time.Time) { f.clock = t }

func mustTestUUID(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		panic(fmt.Sprintf("test uuid %q: %v", s, err))
	}
	return id
}

func TestBriefPushQueuesOnlyMembersWhoOptedIn(t *testing.T) {
	f := newBriefPushFixture(t)
	optedIn, optedInBinding := f.member(nil)
	f.preference(optedIn, true, optedInBinding, nil, nil)

	// Push off, with a chat chosen: not queued.
	off, offBinding := f.member(nil)
	f.preference(off, false, offBinding, nil, nil)

	// Push on, but the chosen binding belongs to someone else: not queued.
	mismatched, _ := f.member(nil)
	_, otherBinding := f.member(nil)
	f.preference(mismatched, true, otherBinding, nil, nil)

	// Push on, no chat chosen: not queued.
	noChat, _ := f.member(nil)
	f.preference(noChat, true, "", nil, nil)

	// A member with no preference row at all: not queued.
	_, _ = f.member(nil)

	briefID := f.brief("Two decisions need you.", f.proposal("Approve the research squad", nil))
	if got := f.enqueue(briefID); got != 1 {
		t.Fatalf("queued %d deliveries, want 1 (only the opted-in member)", got)
	}
	if r := f.delivery(briefID, optedIn); r.State != briefStatePending || r.Attempts != 0 {
		t.Fatalf("opted-in row = %+v, want pending with no attempts", r)
	}
	for _, userID := range []string{off, mismatched, noChat} {
		var count int
		if err := f.pool.QueryRow(context.Background(),
			`SELECT count(*) FROM brief_delivery WHERE brief_id = $1 AND member_id = $2`,
			briefID, userID).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("member %s got a delivery row", userID)
		}
	}
	// The brief:created event can run twice for one brief. The second run adds nothing.
	if got := f.enqueue(briefID); got != 0 {
		t.Fatalf("second enqueue queued %d, want 0", got)
	}
}

func TestBriefPushSendsMessageAndRecordsSent(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)

	p1 := f.proposal("Approve the research squad", nil)
	snoozed := briefPushStart.Add(72 * time.Hour)
	p2 := f.proposal("Hire a copywriter", &snoozed)
	p3 := f.proposal("Raise the content budget", nil)
	briefID := f.brief("Two decisions need you today.", p1, p2, p3)
	f.enqueue(briefID)

	if got := f.deliverDue(); got != 1 {
		t.Fatalf("deliverDue took %d rows, want 1", got)
	}
	if f.notifier.calls.Load() != 1 {
		t.Fatalf("notifier calls = %d, want 1", f.notifier.calls.Load())
	}
	sent := f.notifier.sent[0]
	if sent.binding.ChannelUserID == "" || sent.binding.ChannelType != briefPushTestChat {
		t.Fatalf("binding = %+v", sent.binding)
	}

	text := sent.msg.Text
	if !strings.HasPrefix(text, "Two decisions need you today.") {
		t.Fatalf("message does not open with the headline:\n%s", text)
	}
	if !strings.Contains(text, "2 decisions need you:") {
		t.Fatalf("message does not count the pending decisions (snoozed one left out):\n%s", text)
	}
	if strings.Contains(text, "Hire a copywriter") {
		t.Fatalf("a snoozed decision was named:\n%s", text)
	}
	officeDecision := "https://app.example/" + f.slug + "/office?decision=" + p1
	if !strings.Contains(text, "- Approve the research squad "+officeDecision) {
		t.Fatalf("first decision line missing its link:\n%s", text)
	}
	briefURL := "https://app.example/" + f.slug + "/office?brief=" + briefID
	if len(sent.msg.Buttons) != 1 || sent.msg.Buttons[0].URL != briefURL {
		t.Fatalf("buttons = %+v, want one link to the brief", sent.msg.Buttons)
	}

	r := f.delivery(briefID, userID)
	if r.State != briefStateSent || r.Attempts != 1 || !r.DeliveredAt.Valid || r.LastError != "" {
		t.Fatalf("row after send = %+v", r)
	}
}

func TestBriefPushRetriesWithBackoffAndFailsAfterFiveAttempts(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)
	briefID := f.brief("Quiet day.")
	f.enqueue(briefID)

	f.notifier.err = errors.New("telegram api: 500 Internal Server Error")

	// The row is due at the enqueue instant. Each failed attempt waits 1, 2, 4
	// and 8 minutes; the fifth failure is final.
	wantNext := []time.Duration{time.Minute, 3 * time.Minute, 7 * time.Minute, 15 * time.Minute}
	at := briefPushStart
	for attempt := 1; attempt <= 4; attempt++ {
		f.pointClock(at)
		f.deliverDue()
		r := f.delivery(briefID, userID)
		if r.State != briefStatePending || r.Attempts != int32(attempt) {
			t.Fatalf("after attempt %d: %+v, want pending", attempt, r)
		}
		if want := briefPushStart.Add(wantNext[attempt-1]); !r.NextAttemptAt.Equal(want) {
			t.Fatalf("after attempt %d: next attempt %v, want %v", attempt, r.NextAttemptAt, want)
		}
		if r.LastError == "" {
			t.Fatalf("after attempt %d: last_error empty", attempt)
		}
		// Not yet due: a run now sends nothing.
		before := f.notifier.calls.Load()
		f.pointClock(r.NextAttemptAt.Add(-time.Second))
		if n := f.deliverDue(); n != 0 || f.notifier.calls.Load() != before {
			t.Fatalf("a row before its due time was taken (n=%d)", n)
		}
		at = r.NextAttemptAt
	}

	f.pointClock(at)
	f.deliverDue()
	r := f.delivery(briefID, userID)
	if r.State != briefStateFailed || r.Attempts != briefPushMaxAttempts {
		t.Fatalf("after the fifth attempt: %+v, want failed with %d attempts", r, briefPushMaxAttempts)
	}
	if !strings.Contains(r.LastError, "500") {
		t.Fatalf("last_error = %q, want the platform's error", r.LastError)
	}
	calls := f.notifier.calls.Load()
	f.pointClock(at.Add(24 * time.Hour))
	f.deliverDue()
	if f.notifier.calls.Load() != calls {
		t.Fatal("a failed row was sent again")
	}
}

func TestBriefPushBlockedBotFailsAtOnce(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)
	briefID := f.brief("Quiet day.")
	f.enqueue(briefID)

	f.notifier.err = channel.PermanentDeliveryError(errors.New("telegram api: 403 Forbidden: bot was blocked by the user"))
	f.deliverDue()

	r := f.delivery(briefID, userID)
	if r.State != briefStateFailed || r.Attempts != 1 {
		t.Fatalf("row after a 403 = %+v, want failed on the first attempt", r)
	}
	if !strings.Contains(r.LastError, "blocked") {
		t.Fatalf("last_error = %q", r.LastError)
	}
}

func TestBriefPushHonoursPlatformRetryAfter(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)
	briefID := f.brief("Quiet day.")
	f.enqueue(briefID)

	f.notifier.err = &channel.DeliveryError{Err: errors.New("telegram api: 429 Too Many Requests"), RetryAfter: 10 * time.Minute}
	f.deliverDue()

	r := f.delivery(briefID, userID)
	if want := briefPushStart.Add(10 * time.Minute); r.State != briefStatePending || !r.NextAttemptAt.Equal(want) {
		t.Fatalf("row after 429 = %+v, want pending until %v", r, want)
	}
}

func TestBriefPushDefersToEndOfQuietHoursInTheMembersTimezone(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(testutil.Cols{"timezone": "Asia/Ho_Chi_Minh"})
	start, end := 22, 7
	f.preference(userID, true, bindingID, &start, &end)

	// 15:30 UTC is 22:30 in Ho Chi Minh City (UTC+7): inside the window.
	f.pointClock(time.Date(2026, 1, 10, 15, 30, 0, 0, time.UTC))
	briefID := f.brief("Quiet day.")
	f.enqueue(briefID)
	f.deliverDue()

	if f.notifier.calls.Load() != 0 {
		t.Fatal("a message was sent inside quiet hours")
	}
	r := f.delivery(briefID, userID)
	// The window ends at 07:00 local, which is 00:00 UTC the next day.
	wantEnd := time.Date(2026, 1, 11, 0, 0, 0, 0, time.UTC)
	if r.State != briefStateDeferred || !r.NextAttemptAt.Equal(wantEnd) || r.Attempts != 0 {
		t.Fatalf("row inside quiet hours = %+v, want deferred to %v with no attempt", r, wantEnd)
	}

	// Still inside the window at 06:59 local: nothing is sent.
	f.pointClock(wantEnd.Add(-time.Minute))
	f.deliverDue()
	if f.notifier.calls.Load() != 0 {
		t.Fatal("a deferred message was sent before the window ended")
	}

	// At 07:00 local the window has ended and the row goes out.
	f.pointClock(wantEnd)
	f.deliverDue()
	if f.notifier.calls.Load() != 1 {
		t.Fatalf("notifier calls = %d after the window ended, want 1", f.notifier.calls.Load())
	}
	if r := f.delivery(briefID, userID); r.State != briefStateSent {
		t.Fatalf("row after the window = %+v, want sent", r)
	}
}

func TestBriefPushDoesNotSendAfterThePushWasTurnedOff(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)
	briefID := f.brief("Quiet day.")
	f.enqueue(briefID)

	if _, err := f.q.UpsertBriefPushPreference(context.Background(), db.UpsertBriefPushPreferenceParams{
		ID: mustTestUUID(uuid.NewString()), WorkspaceID: mustTestUUID(f.ws),
		MemberID: mustTestUUID(userID), BriefPush: false, BindingID: mustTestUUID(bindingID),
	}); err != nil {
		t.Fatalf("turn push off: %v", err)
	}
	f.deliverDue()

	if f.notifier.calls.Load() != 0 {
		t.Fatal("a brief was sent to a member who turned the push off")
	}
	if r := f.delivery(briefID, userID); r.State != briefStateFailed {
		t.Fatalf("row = %+v, want failed", r)
	}
}

func TestBriefPushDoesNotSendToAChatAppWithNoNotifier(t *testing.T) {
	f := newBriefPushFixture(t)
	userID, bindingID := f.member(nil)
	f.preference(userID, true, bindingID, nil, nil)
	briefID := f.brief("Quiet day.")
	f.enqueue(briefID)

	// Drop the notifier: the chat app is no longer able to receive briefs.
	f.svc.Notifiers = map[channel.Type]channel.Notifier{}
	f.deliverDue()

	if r := f.delivery(briefID, userID); r.State != briefStateFailed || r.LastError != briefPushReasonNoChannel {
		t.Fatalf("row = %+v, want failed for a missing notifier", r)
	}
}

func TestBriefPushOfficeLinksAreOmittedWithoutAnAppURL(t *testing.T) {
	msg := renderBriefPush("Quiet day.", []briefPushDecision{{ID: "p1", Title: "Approve"}}, "b1", "")
	if strings.Contains(msg.Text, "https://") || len(msg.Buttons) != 0 {
		t.Fatalf("message carries links with no app URL: %+v", msg)
	}
	if !strings.Contains(msg.Text, "1 decision needs you:\n- Approve") {
		t.Fatalf("decision line missing: %q", msg.Text)
	}
}

func TestQuietWindowEnd(t *testing.T) {
	hcm := time.FixedZone("UTC+7", 7*3600)
	utc := time.UTC
	at := func(loc *time.Location, day, hour, minute int) time.Time {
		return time.Date(2026, 1, day, hour, minute, 0, 0, loc)
	}
	cases := []struct {
		name       string
		now        time.Time
		loc        *time.Location
		start, end int
		inside     bool
		until      time.Time
	}{
		{"evening inside a midnight window", at(utc, 10, 23, 0), utc, 22, 7, true, at(utc, 11, 7, 0)},
		{"after midnight inside a midnight window", at(utc, 11, 3, 0), utc, 22, 7, true, at(utc, 11, 7, 0)},
		{"just before the window opens", at(utc, 10, 21, 59), utc, 22, 7, false, time.Time{}},
		{"at the end hour the window is over", at(utc, 11, 7, 0), utc, 22, 7, false, time.Time{}},
		{"daytime window, inside", at(utc, 10, 10, 30), utc, 9, 17, true, at(utc, 10, 17, 0)},
		{"daytime window, after its end", at(utc, 10, 17, 0), utc, 9, 17, false, time.Time{}},
		{"start equals end is no window", at(utc, 10, 12, 0), utc, 5, 5, false, time.Time{}},
		{"local hour, not UTC", at(utc, 10, 15, 30), hcm, 22, 7, true, at(hcm, 11, 7, 0)},
		{"local hour outside the window", at(utc, 10, 2, 0), hcm, 22, 7, false, time.Time{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			until, inside := quietWindowEnd(tc.now, tc.loc, tc.start, tc.end)
			if inside != tc.inside {
				t.Fatalf("inside = %v, want %v", inside, tc.inside)
			}
			if inside && !until.Equal(tc.until) {
				t.Fatalf("until = %v, want %v", until, tc.until)
			}
		})
	}
}

func TestRenderBriefPushLayout(t *testing.T) {
	office := "https://app.example/acme/office"
	long := strings.Repeat("x", 300)
	decisions := []briefPushDecision{
		{ID: "p1", Title: "First"},
		{ID: "p2", Title: long},
		{ID: "p3", Title: "Line one\nline two"},
		{ID: "p4", Title: "Fourth"},
		{ID: "p5", Title: "Fifth"},
	}
	msg := renderBriefPush("Headline here.", decisions, "b1", office)

	if !strings.HasPrefix(msg.Text, "Headline here.\n\n5 decisions need you:\n") {
		t.Fatalf("opening lines = %q", msg.Text)
	}
	if got := strings.Count(msg.Text, "\n- "); got != briefPushDecisionLines {
		t.Fatalf("decision lines = %d, want %d", got, briefPushDecisionLines)
	}
	if !strings.Contains(msg.Text, "- Line one line two "+office+"?decision=p3") {
		t.Fatalf("multi-line title not collapsed to one line:\n%s", msg.Text)
	}
	if strings.Contains(msg.Text, "Fourth") || strings.Contains(msg.Text, "Fifth") {
		t.Fatal("a decision beyond the top three was named")
	}
	if want := "- " + strings.Repeat("x", briefPushTitleRunes-1) + "…"; !strings.Contains(msg.Text, want) {
		t.Fatalf("long title not cut to %d runes with an ellipsis", briefPushTitleRunes)
	}
	if !strings.Contains(msg.Text, "...and 2 more in the Office.") {
		t.Fatalf("missing overflow line:\n%s", msg.Text)
	}
	if !strings.Contains(msg.Text, "Brief: "+office+"?brief=b1") {
		t.Fatalf("missing brief link:\n%s", msg.Text)
	}
	if len(msg.Buttons) != 1 || msg.Buttons[0].URL != office+"?brief=b1" {
		t.Fatalf("buttons = %+v", msg.Buttons)
	}
}

func TestRenderBriefPushQuietDay(t *testing.T) {
	msg := renderBriefPush("Nothing needs you today.", nil, "b1", "https://app.example/acme/office")
	if strings.Contains(msg.Text, "decision") {
		t.Fatalf("quiet day names decisions:\n%s", msg.Text)
	}
	if !strings.HasPrefix(msg.Text, "Nothing needs you today.") {
		t.Fatalf("text = %q", msg.Text)
	}
}

func TestBriefPushBackoffSchedule(t *testing.T) {
	want := map[int]time.Duration{1: time.Minute, 2: 2 * time.Minute, 3: 4 * time.Minute, 4: 8 * time.Minute}
	for attempt, d := range want {
		if got := briefPushBackoffFor(attempt); got != d {
			t.Fatalf("backoff after attempt %d = %v, want %v", attempt, got, d)
		}
	}
	if got := briefPushBackoffFor(0); got != time.Minute {
		t.Fatalf("backoff for attempt 0 = %v", got)
	}
}
