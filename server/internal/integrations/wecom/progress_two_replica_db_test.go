package wecom

// progress_two_replica_db_test.go — a run's progress, reported to a replica
// that does not hold the bubble, against a real database.
//
// The shape is the one outbound_two_replica_db_test.go builds for the answer:
// each replica is its own process — its own bus, senders registry, stream
// store, typing indicator, outbound subscriber and relay dispatcher — and
// everything below the adapter is shared: the migrated database, the relay
// (fanoutRelay) and the claim store (sharedDedupe). The daemon's transcript
// request is served by whichever replica the load balancer picked, so the
// rows are committed to task_message and the event is published on THAT
// replica's bus, exactly as handler.ReportTaskMessages does.

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/integrations/channel"
	"github.com/multica-ai/multica/server/internal/integrations/channel/engine"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// progressTurn is a boundTurn whose asker is the bot's principal in their own
// one-to-one, so the bubble is on the tier that shows steps.
type progressTurn struct {
	boundTurn
	installer pgtype.UUID
	sender    string
	pool      *pgxpool.Pool

	mu  sync.Mutex
	seq int32
}

func seedProgressTurn(t *testing.T, pool *pgxpool.Pool) *progressTurn {
	t.Helper()
	ctx := context.Background()
	turn := &progressTurn{boundTurn: seedBoundTurn(t, pool), pool: pool}
	var workspaceID string
	if err := pool.QueryRow(ctx,
		`SELECT installer_user_id, workspace_id::text FROM channel_installation WHERE id = $1`,
		turn.instID).Scan(&turn.installer, &workspaceID); err != nil {
		t.Fatalf("seed: read installation: %v", err)
	}
	turn.sender = "SENDER_" + strings.ReplaceAll(turn.chatID, "CHAT_", "")
	if _, err := pool.Exec(ctx, `INSERT INTO channel_user_binding
	        (workspace_id, multica_user_id, installation_id, channel_type, channel_user_id)
	      VALUES ($1, $2, $3, 'wecom', $4)`,
		workspaceID, turn.installer, turn.instID, turn.sender); err != nil {
		t.Fatalf("seed: bind the principal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM channel_user_binding WHERE installation_id = $1`, turn.instID)
	})
	return turn
}

// progressReplica is one backend process, wired the way router.go wires the
// WeCom block: indicator registered, indicator and subscriber both on the
// relay, subscriber attached as the relay's handler.
type progressReplica struct {
	bus     *events.Bus
	conn    *bubbleConn // nil when this replica holds no socket
	reg     *sendersRegistry
	streams *streamStore
	typing  *TypingIndicatorManager
	router  *RelayOutbound
	instID  pgtype.UUID
	clock   *testClock
}

func newProgressReplica(t *testing.T, turn *progressTurn, holdsSocket bool, relay *fanoutRelay, dedupe DedupeStore) *progressReplica {
	t.Helper()
	var instID pgtype.UUID
	if err := instID.Scan(turn.instID); err != nil {
		t.Fatalf("parse installation id: %v", err)
	}
	r := &progressReplica{bus: events.New(), reg: newSendersRegistry(), streams: newStreamStore(), instID: instID, clock: newTestClock()}
	r.streams.now = r.clock.now
	if holdsSocket {
		r.takeSocket()
	}
	log := slog.New(slog.NewTextHandler(&strings.Builder{}, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r.router = NewRelayOutbound(relay, dedupe, RelayConfig{}, log)
	relay.register(r.router)
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() { cancel(); r.router.Wait() })
	r.router.Start(ctx)

	q := db.New(turn.pool)
	r.typing = NewTypingIndicator(TypingIndicatorConfig{
		Senders: r.reg, Streams: r.streams,
		Tasks: q, Deliveries: q, Languages: q, Identities: q,
		Logger: log,
	})
	r.typing.guardAfter = -1  // fired by the tests, at the moment they mean to
	r.typing.trailAfter = nil // no timer to land a frame mid-assertion
	r.typing.Register(r.bus)
	r.typing.WithRelay(r.router)
	o := NewOutbound(q, r.reg, r.streams, log, WithRelay(r.router))
	o.Register(r.bus)
	r.router.Attach(o)
	return r
}

// takeSocket gives this replica a live connection for the installation — a
// lease won, or the second half of a hand-over.
func (r *progressReplica) takeSocket() {
	r.conn = &bubbleConn{}
	sender := newWSSender(r.conn, nil)
	r.conn.sender = sender
	r.reg.set(r.instID, sender)
}

// loseSocket is the lease leaving this replica.
func (r *progressReplica) loseSocket() {
	r.reg.clear(r.instID, r.conn.sender)
}

// ask is the question arriving on this replica's socket, and the debounced
// flush creating the run that answers it.
func (r *progressReplica) ask(t *testing.T, turn *progressTurn) {
	t.Helper()
	raw, err := json.Marshal(InboundMessage{
		BotID: "BOT", ChatID: turn.chatID, ChatType: "single", SenderUserID: turn.sender, ReqID: "REQ-" + turn.chatID,
	})
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	var session pgtype.UUID
	if err := session.Scan(turn.sessionID); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	r.typing.OnIngested(context.Background(),
		engine.ResolvedInstallation{ID: r.instID, InstallerUserID: turn.installer},
		channel.InboundMessage{
			Text:   "S270 的价格",
			Source: channel.Source{ChannelType: TypeWecom, ChatID: turn.chatID, ChatType: channel.ChatTypeP2P, SenderID: turn.sender},
			Raw:    raw,
		}, session, session)
	r.bus.Publish(events.Event{
		Type: protocol.EventTaskQueued, ChatSessionID: turn.sessionID, TaskID: turn.taskID,
		Payload: map[string]any{"task_id": turn.taskID, "status": "queued"},
	})
	if got := len(r.conn.streamFrames(t)); got != 1 {
		t.Fatalf("the question painted %d stream frames on its own replica, want the one opening frame", got)
	}
}

// report is the daemon's transcript request served by THIS replica: the row
// is committed, then the event is published on this replica's bus.
func (r *progressReplica) report(t *testing.T, turn *progressTurn, tool, path string) int32 {
	t.Helper()
	turn.mu.Lock()
	turn.seq++
	seq := turn.seq
	turn.mu.Unlock()
	input := map[string]any{"file_path": path}
	raw, _ := json.Marshal(input)
	if _, err := turn.pool.Exec(context.Background(),
		`INSERT INTO task_message (task_id, seq, type, tool, input) VALUES ($1, $2, 'tool_use', $3, $4)`,
		turn.taskID, seq, tool, raw); err != nil {
		t.Fatalf("file transcript row: %v", err)
	}
	r.bus.Publish(events.Event{
		Type: protocol.EventTaskMessage, TaskID: turn.taskID,
		Payload: protocol.TaskMessagePayload{TaskID: turn.taskID, Seq: int(seq), Type: "tool_use", Tool: tool, Input: input},
	})
	return seq
}

// refreshes is every in-flight refresh this replica wrote: not the opening
// placeholder, not a closing frame.
func (r *progressReplica) refreshes(t *testing.T) []string {
	t.Helper()
	if r.conn == nil {
		return nil
	}
	var out []string
	for _, f := range r.conn.streamFrames(t) {
		content, _ := f["content"].(string)
		if f["finish"] == true || content == streamThinkingPlaceholder {
			continue
		}
		out = append(out, content)
	}
	return out
}

func (r *progressReplica) frames(t *testing.T) int {
	if r.conn == nil {
		return 0
	}
	r.conn.mu.Lock()
	defer r.conn.mu.Unlock()
	return len(r.conn.frames)
}

// tick moves this replica's clock past the refresh interval, so the next
// report is worth a frame of its own.
func (r *progressReplica) tick() { r.clock.advance(2 * progressMinInterval) }

// settleProgress waits until every replica's lane is idle and nothing is in flight
// on the relay — the point after which a frame that was going to be written
// has been.
func settleProgress(t *testing.T, replicas ...*progressReplica) {
	t.Helper()
	for i := 0; i < 3; i++ { // a relayed frame lands on another lane; go round until still
		for _, r := range replicas {
			waitProgress(t, r.typing)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (f *fanoutRelay) progressFrames() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, q := range f.log {
		if q.frame.Kind == relayKindProgress {
			n++
		}
	}
	return n
}

// onlyRoundSeq is the store's own name for the one round open in a session.
func onlyRoundSeq(t *testing.T, s *streamStore, session pgtype.UUID) roundSeq {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	rounds := s.sessions[util.UUIDToString(session)]
	if len(rounds) != 1 {
		t.Fatalf("%d rounds open in the session, want 1", len(rounds))
	}
	return rounds[0].seq
}

func sessionUUID(t *testing.T, turn *progressTurn) pgtype.UUID {
	t.Helper()
	var s pgtype.UUID
	if err := s.Scan(turn.sessionID); err != nil {
		t.Fatalf("parse session: %v", err)
	}
	return s
}

// ---- the blocker ----

// Replica A holds the socket and painted the bubble; the daemon's transcript
// requests are served by B. Before the fix B returned at once — it had no
// bubble open — and the user watched a spinner for the whole run.
//
// Every report is published exactly once: B has no round for it, A does and
// does not pass it on, so the frames do not bounce between replicas.
func TestTwoReplicas_ProgressReportedOffLeaseIsShownOnTheHolder(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedProgressTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	a := newProgressReplica(t, turn, true, relay, dedupe)
	b := newProgressReplica(t, turn, false, relay, dedupe)
	a.ask(t, turn)

	b.report(t, turn, "Read", "/srv/app/router.go")
	settleProgress(t, a, b)
	a.tick()
	b.report(t, turn, "Read", "/srv/app/handler.go")
	settleProgress(t, a, b)

	refreshes := a.refreshes(t)
	if len(refreshes) != 2 {
		t.Fatalf("the holder wrote %d refresh frames for two reports served by the other replica, want 2: %q",
			len(refreshes), refreshes)
	}
	if !strings.Contains(refreshes[1], "/srv/app/router.go") || !strings.Contains(refreshes[1], "/srv/app/handler.go") {
		t.Errorf("the second refresh does not carry both steps: %q", refreshes[1])
	}
	if got := relay.progressFrames(); got != 2 {
		t.Errorf("%d progress frames published for 2 reports, want exactly one each — "+
			"a replica without the round must publish, a replica with it must not", got)
	}
	for _, f := range relay.log {
		if f.frame.Kind == relayKindProgress && f.frame.Content != "" {
			t.Errorf("a progress frame carried text %q through the relay; the holder reads the rows itself", f.frame.Content)
		}
	}
}

// The rotation guard hands a long run's bubble over only for a run that has
// stepped since the current stream opened, and the steps are counted on the
// replica holding the round. Reports that land elsewhere must count there, or
// the bubble runs into the stream window.
func TestTwoReplicas_TheRotationGuardCountsReportsThatLandedElsewhere(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedProgressTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	a := newProgressReplica(t, turn, true, relay, dedupe)
	b := newProgressReplica(t, turn, false, relay, dedupe)
	a.ask(t, turn)
	session := sessionUUID(t, turn)
	seq := onlyRoundSeq(t, a.streams, session)

	// Anti-vacuity: with no report at all the guard does nothing.
	before := a.frames(t)
	a.typing.fireGuard(context.Background(), session, seq)
	if got := a.frames(t); got != before {
		t.Fatalf("the guard wrote %d frames for a run that never stepped; the test below proves nothing", got-before)
	}

	b.report(t, turn, "Read", "/srv/app/go.mod")
	settleProgress(t, a, b)

	before = a.frames(t)
	a.typing.fireGuard(context.Background(), session, seq)
	frames := a.conn.streamFrames(t)
	if got := a.frames(t) - before; got != 2 {
		t.Fatalf("the guard wrote %d frames after a report served by the other replica, want the hand-over "+
			"(a closing frame on the old stream and an opening frame on the new one)", got)
	}
	if frames[len(frames)-2]["finish"] != true {
		t.Errorf("the hand-over did not seal the old stream: %v", frames[len(frames)-2])
	}
}

// Duplicates and frames that arrive out of order. The relay replays its window
// into a restarted reader and nothing orders frames across replicas, so the
// holder sees both. A frame at or below what is already shown is a no-op: no
// second line, no second frame, and not a sign of life for the guard.
func TestTwoReplicas_DuplicateAndOutOfOrderProgressFramesAreNoOps(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedProgressTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	a := newProgressReplica(t, turn, true, relay, dedupe)
	b := newProgressReplica(t, turn, false, relay, dedupe)
	a.ask(t, turn)
	session := sessionUUID(t, turn)

	// Three reports in one burst on B. Inside one refresh interval, so the
	// holder folds whatever it has read and the next frame carries the rest.
	b.report(t, turn, "Read", "/srv/a.go")
	b.report(t, turn, "Grep", "/srv/b.go")
	b.report(t, turn, "Read", "/srv/c.go")
	settleProgress(t, a, b)
	a.tick()
	// The newest frame again, then the older ones, then the newest once more:
	// a replay, out of order. Built from a frame the relay really carried, so
	// only the seq differs. (B may have merged the burst into fewer frames
	// than reports; a merged frame says the same as its last report.)
	var template relayFrame
	relay.mu.Lock()
	for _, q := range relay.log {
		if q.frame.Kind == relayKindProgress {
			template = q.frame
		}
	}
	relay.mu.Unlock()
	if template.Kind != relayKindProgress {
		t.Fatal("no progress frame was published at all; the test below proves nothing")
	}
	for _, seq := range []int32{3, 1, 2, 3} {
		f := template
		f.Seq = seq
		n := progressNudge{taskID: f.TaskID, seq: seq}
		body, _ := json.Marshal(f)
		a.router.DeliverWecomOutbound(turn.instID, body, progressEventID(n))
	}
	settleProgress(t, a, b)
	shown := a.refreshes(t)
	if len(shown) == 0 {
		t.Fatal("no refresh at all; the test below proves nothing")
	}
	last := shown[len(shown)-1]
	for _, path := range []string{"/srv/a.go", "/srv/b.go", "/srv/c.go"} {
		if strings.Count(last, path) != 1 {
			t.Errorf("%s appears %d times in the bubble, want once: %q", path, strings.Count(last, path), last)
		}
	}
	if strings.Contains(last, "×") {
		t.Errorf("a replayed frame was counted as a repeat of its step: %q", last)
	}

	// The guard: rotate once on the real reports, then replay every frame
	// again. A copy is not a sign of life, so a second rotation must not come.
	seq := onlyRoundSeq(t, a.streams, session)
	a.typing.fireGuard(context.Background(), session, seq)
	before := a.frames(t)
	relay.replayTo(a.router)
	settleProgress(t, a, b)
	if got := a.frames(t) - before; got != 0 {
		t.Errorf("replaying frames already shown wrote %d frames, want none", got)
	}
	a.typing.fireGuard(context.Background(), session, seq)
	if got := a.frames(t) - before; got != 0 {
		t.Errorf("replayed frames made the guard hand the bubble over again (%d frames); "+
			"a copy of an old report is not the run stepping", got)
	}
}

// Progress that arrives after the ending writes nothing. The answer reaches
// the holder by the relay and seals the bubble; a report the daemon flushed
// just before completing lands on the other replica a moment later.
func TestTwoReplicas_ProgressAfterTheEndingWritesNothing(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedProgressTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	a := newProgressReplica(t, turn, true, relay, dedupe)
	b := newProgressReplica(t, turn, false, relay, dedupe)
	a.ask(t, turn)

	b.bus.Publish(chatDoneFor(turn.boundTurn))
	waitFor(t, "the answer to seal the bubble on the holder", func() bool {
		frames := a.conn.streamFrames(t)
		return len(frames) > 0 && frames[len(frames)-1]["finish"] == true
	})
	before := a.frames(t)

	a.tick()
	b.report(t, turn, "Read", "/srv/late.go")
	settleProgress(t, a, b)

	if got := a.frames(t) - before; got != 0 {
		t.Errorf("a report after the ending wrote %d frames on the holder; the bubble is sealed", got)
	}
	if pushes := a.conn.pushes(t); len(pushes) != 0 {
		t.Errorf("a report after the ending went out as %d plain message(s)", len(pushes))
	}
}

// A lease hand-over mid-round. The bubble's state is on the replica that
// painted it, so that replica stays the one that shows the run's steps. While
// two replicas hold a socket for the bot only that one writes; once it has
// lost its socket nobody writes a step, the steps never turn into plain
// messages, and the answer still arrives exactly once, from the new holder.
func TestTwoReplicas_ALeaseHandoverMidRound(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedProgressTurn(t, pool)
	relay, dedupe := &fanoutRelay{}, newSharedDedupe()
	a := newProgressReplica(t, turn, true, relay, dedupe)
	b := newProgressReplica(t, turn, false, relay, dedupe)
	c := newProgressReplica(t, turn, false, relay, dedupe) // serves the daemon's requests
	a.ask(t, turn)

	c.report(t, turn, "Read", "/srv/one.go")
	settleProgress(t, a, b, c)
	if got := len(a.refreshes(t)); got != 1 {
		t.Fatalf("before the hand-over the holder wrote %d refreshes, want 1", got)
	}

	// The lease is moving: B has connected, A has not let go yet.
	b.takeSocket()
	a.tick()
	c.report(t, turn, "Read", "/srv/two.go")
	settleProgress(t, a, b, c)
	if got := len(a.refreshes(t)); got != 2 {
		t.Errorf("during the overlap the round's holder wrote %d refreshes in all, want 2", got)
	}
	if got := b.frames(t); got != 0 {
		t.Errorf("the replica that has a socket but not the round wrote %d frames; it has no bubble to write into", got)
	}

	// A has let go.
	a.loseSocket()
	aBefore := a.frames(t)
	a.tick()
	c.report(t, turn, "Read", "/srv/three.go")
	settleProgress(t, a, b, c)
	if got := a.frames(t) - aBefore; got != 0 {
		t.Errorf("the old holder wrote %d frames after losing its socket", got)
	}
	if got := b.frames(t); got != 0 {
		t.Errorf("the new holder wrote %d frames for a step; a step has no address but the bubble", got)
	}
	if got := relay.progressFrames(); got != 3 {
		t.Errorf("%d progress frames published for 3 reports, want exactly one each", got)
	}

	c.bus.Publish(chatDoneFor(turn.boundTurn))
	waitFor(t, "the answer to reach the new holder", func() bool { return len(b.conn.pushes(t)) == 1 })
	time.Sleep(50 * time.Millisecond)
	if got := len(b.conn.pushes(t)); got != 1 {
		t.Errorf("the answer went out %d times, want once", got)
	}
	if got := a.frames(t) - aBefore; got != 0 {
		t.Errorf("the old holder wrote %d frames for the answer with no socket", got)
	}
}

// ListTaskProgressSince names in SQL the kinds stepFromMessage shows. The two
// have to agree: a kind the query leaves out is never shown off-lease, and one
// it adds is read for nothing. One row of every kind the daemon writes, read
// back through the real query.
func TestTheTranscriptQueryReadsWhatTheBubbleShows(t *testing.T) {
	pool := twoReplicaDB(t)
	turn := seedBoundTurn(t, pool)
	ctx := context.Background()
	kinds := []string{"text", "thinking", "tool_use", "tool_result", "error"}
	for i, k := range kinds {
		if _, err := pool.Exec(ctx,
			`INSERT INTO task_message (task_id, seq, type, content) VALUES ($1, $2, $3, 'x')`,
			turn.taskID, i+1, k); err != nil {
			t.Fatalf("file %s row: %v", k, err)
		}
	}
	taskID, _ := util.ParseUUID(turn.taskID)
	rows, err := db.New(pool).ListTaskProgressSince(ctx, db.ListTaskProgressSinceParams{TaskID: taskID})
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	read := map[string]bool{}
	for _, r := range rows {
		read[r.Type] = true
	}
	for _, k := range kinds {
		_, shown := stepFromMessage(taskMessage{msgType: k})
		if read[k] != shown {
			t.Errorf("kind %q: the query reads it = %v, the bubble shows it = %v", k, read[k], shown)
		}
	}
	if rows2, _ := db.New(pool).ListTaskProgressSince(ctx, db.ListTaskProgressSinceParams{TaskID: taskID, AfterSeq: 3}); len(rows2) != 1 || rows2[0].Seq != 5 {
		t.Errorf("past seq 3 the query read %+v, want only the error row at seq 5", rows2)
	}
}
