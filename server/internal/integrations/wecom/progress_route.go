package wecom

// progress_route.go — getting a run's progress to the one replica that can
// show it.
//
// A bubble lives on the replica that painted it: its stream id, its feed and
// its rotation guard are in that process's streamStore. Progress is reported
// to whichever replica served the daemon's HTTP request, a load-balancer
// decision. So a report is a NUDGE that names a run and how far its transcript
// has got, and the replica holding the round does the rest:
//
//   - The subscriber classifies the event and queues a nudge. Nothing else
//     runs on the daemon's request: no database read, no socket write.
//   - The lane works through nudges one run at a time, off that goroutine.
//   - A replica that holds the round counts the report (the rotation guard's
//     sign of life), reads the run's new transcript rows past what it has
//     already shown, folds them into the feed, redacts while rendering
//     (progress_render.go), throttles, and writes the frame.
//   - A replica that does not hold it publishes a relayKindProgress frame and
//     forgets the report. The frame carries ids and a seq, never text.
//
// Ordering and dedupe come from the transcript itself. The holder reads rows
// by seq, after the highest seq it has folded in (roundEntry.folded), so a
// duplicate frame, a replay, or a frame that overtakes an earlier one reads
// nothing new, and a frame for a round that has ended finds no round.

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// transcriptLookup reads the rows a bubble shows. *db.Queries satisfies it.
type transcriptLookup interface {
	ListTaskProgressSince(ctx context.Context, arg db.ListTaskProgressSinceParams) ([]db.ListTaskProgressSinceRow, error)
}

// progressWriteTimeout bounds the work for one nudge on the round's holder:
// the transcript read, the permission check and the frame. Well under the
// socket's own ack wait, because a step nobody could write in this long is a
// step the next one replaces anyway.
const progressWriteTimeout = 3 * time.Second

// progressNudge says one run has something new to show.
type progressNudge struct {
	taskID string
	// seq is the reporting message's task_message.seq. Zero for the
	// milestone, which is not a transcript row.
	seq       int32
	milestone bool
	// local: reported on this replica's own bus. Only a local nudge may be
	// published to the others; a relayed one that finds no round here is
	// dropped, or two replicas without the round would hand it back and forth.
	local bool
	// target is where the run belongs. A relayed nudge arrives with it; a
	// local one has it filled in by the lane (progressTarget).
	target   taskRound
	resolved bool
	// trailing: the holder's own follow-up for steps the throttle held back
	// (armTrailing). It is not a report: it carries no seq, is not a sign of
	// life for the rotation guard, and is never published.
	trailing bool
}

// progressSeqTrailing is a trailing nudge's seq. Below every real seq, so
// progressFor does not count it and merge lets any real report's seq win.
const progressSeqTrailing int32 = -1

// merge folds a later nudge for the same run into this one. Two nudges for one
// run say what the later of them says — a read takes every row past what is
// already shown — so the lane keeps one, and a run that reports faster than
// its bubble can be written never builds a queue.
func (n progressNudge) merge(later progressNudge) progressNudge {
	if later.seq > n.seq {
		n.seq = later.seq
	}
	n.local = n.local || later.local
	n.trailing = n.trailing || later.trailing
	if !n.resolved && later.resolved {
		n.target, n.resolved = later.target, true
	}
	return n
}

// progressLane runs nudges one run at a time, off the goroutine that delivered
// them: the daemon's HTTP request for a local report, the relay's shard reader
// for a relayed one. A run's nudges are worked through in order by one
// goroutine that exists while there is something to do, so a slow socket holds
// up its own run's bubble and nobody else's.
//
// It never refuses and never blocks: each run holds at most one milestone and
// one transcript nudge, merged as they arrive.
type progressLane struct {
	mu      sync.Mutex
	pending map[string]*progressPending // by task id, present while its drainer runs
	run     func(progressNudge)
}

type progressPending struct {
	milestone, transcript *progressNudge
}

func newProgressLane(run func(progressNudge)) *progressLane {
	return &progressLane{pending: map[string]*progressPending{}, run: run}
}

func (l *progressLane) offer(n progressNudge) {
	if n.taskID == "" {
		return
	}
	l.mu.Lock()
	p, running := l.pending[n.taskID]
	if !running {
		p = &progressPending{}
		l.pending[n.taskID] = p
	}
	slot := &p.transcript
	if n.milestone {
		slot = &p.milestone
	}
	if *slot == nil {
		*slot = &n
	} else {
		merged := (*slot).merge(n)
		*slot = &merged
	}
	l.mu.Unlock()
	if !running {
		go l.drain(n.taskID)
	}
}

// drain works one run's nudges until there are none, milestone first: the
// starting line belongs above the first tool call.
func (l *progressLane) drain(taskID string) {
	for {
		l.mu.Lock()
		p := l.pending[taskID]
		next := p.milestone
		if next != nil {
			p.milestone = nil
		} else if next = p.transcript; next != nil {
			p.transcript = nil
		}
		if next == nil {
			delete(l.pending, taskID)
			l.mu.Unlock()
			return
		}
		l.mu.Unlock()
		l.run(*next)
	}
}

// idle reports whether no nudge is queued or running.
func (l *progressLane) idle() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.pending) == 0
}

// progressListening is the subscribers' one cheap gate. With no relay a report
// can only be shown here, so a replica with no bubble open pays nothing; with
// one, the bubble may be on another replica and every report is a candidate.
func (m *TypingIndicatorManager) progressListening() bool {
	return m.streams != nil && (m.relay != nil || m.streams.depth() > 0)
}

// handleTaskMessage queues the run's transcript for its bubble — the tool calls
// and the reasoning between them, which are the whole middle of a run.
//
// The message is only classified here. Tool results and the answer being
// written are most of the event's volume and are dropped without anything
// queued; everything else is done by the lane.
func (m *TypingIndicatorManager) handleTaskMessage(e events.Event) {
	if !m.progressListening() {
		return
	}
	seq, ok := progressSeqOf(e.Payload)
	if !ok {
		return
	}
	m.progress.offer(progressNudge{taskID: taskIDFromEvent(e), seq: seq, local: true})
}

// handleTaskProgress queues the daemon's first milestone: the agent is being
// launched, which is the only thing to say before its first tool call. Only the
// step number is read. The summary is the daemon's English for operators
// ("Launching claude"), so the bubble says it in its own language, and the
// closing milestone lands a moment before the answer that replaces it, so it
// is not shown at all.
func (m *TypingIndicatorManager) handleTaskProgress(e events.Event) {
	if !m.progressListening() {
		return
	}
	if milestoneStep(e.Payload) != 1 {
		return
	}
	m.progress.offer(progressNudge{taskID: taskIDFromEvent(e), milestone: true, local: true})
}

// acceptRelayedProgress is the progressSink half: a report another replica
// could not place. Called on the relay's shard reader, so it only queues.
func (m *TypingIndicatorManager) acceptRelayedProgress(f relayFrame) {
	sessionID, err := util.ParseUUID(f.SessionID)
	if err != nil || !sessionID.Valid || f.TaskID == "" {
		return
	}
	m.progress.offer(progressNudge{
		taskID:    f.TaskID,
		seq:       f.Seq,
		milestone: f.Milestone,
		target:    taskRound{session: sessionID, round: f.RoundID, installation: f.InstallationID},
		resolved:  true,
	})
}

// processProgress is the lane's work for one nudge.
func (m *TypingIndicatorManager) processProgress(n progressNudge) {
	if !n.resolved {
		t, ok := m.progressTarget(n.taskID)
		if !ok {
			return
		}
		n.target, n.resolved = t, true
	}
	ctx, cancel := context.WithTimeout(context.Background(), progressWriteTimeout)
	defer cancel()
	if m.showProgress(ctx, n) {
		return
	}
	if n.local {
		m.publishProgress(n)
	}
}

// publishProgress hands a report this replica has no round for to the others.
// The frame's id is the run and the seq, so it shares nothing with the
// endings' claim keys, which are keyed on the run alone.
func (m *TypingIndicatorManager) publishProgress(n progressNudge) {
	if m.relay == nil || n.target.installation == "" {
		return
	}
	m.relay.publish(relayFrame{
		Kind:           relayKindProgress,
		InstallationID: n.target.installation,
		SessionID:      util.UUIDToString(n.target.session),
		TaskID:         n.taskID,
		RoundID:        n.target.round,
		Seq:            n.seq,
		Milestone:      n.milestone,
	}, progressEventID(n))
}

func progressEventID(n progressNudge) string {
	if n.milestone {
		return "wecom:progress:" + n.taskID + ":start"
	}
	return "wecom:progress:" + n.taskID + ":" + strconv.FormatInt(int64(n.seq), 10)
}

// progressTarget resolves a run to the chat session and round it belongs to,
// and establishes that it is a run asked over WeCom at all.
//
// Neither task:progress nor task:message carries a chat session, so the rows
// are the only answer, and one run posts dozens of reports. Hence the cache,
// which also remembers the runs that are not WeCom's so an issue run, an
// autopilot run or a question typed in Multica costs one read and then
// nothing.
//
// The delivery row comes first because it is the cheaper "no": it exists only
// for a run asked over a channel, and it names the bot. A run with no row was
// typed in Multica, and although it can hold a WeCom round for a moment
// (task:queued binds it; the origin gate gives it back at the end), its steps
// are not the room's question's steps. The round comes off the task row: see
// task_sessions.go for why that column is what an auto-retry clone is resolved
// through.
func (m *TypingIndicatorManager) progressTarget(taskID string) (taskRound, bool) {
	if m.tasks == nil || m.deliveries == nil || taskID == "" {
		return taskRound{}, false
	}
	if cached, hit := m.taskSessions.get(taskID); hit {
		return cached, cached.session.Valid
	}
	id, err := util.ParseUUID(taskID)
	if err != nil || !id.Valid {
		return taskRound{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), taskLookupTimeout)
	defer cancel()
	delivery, err := m.deliveries.GetChannelTaskDelivery(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		m.taskSessions.put(taskID, taskRound{})
		return taskRound{}, false
	case err != nil:
		// A failure is not an answer, but re-asking is worse than waiting: the
		// rest of this run's reports would each put another round trip on a
		// database that has just shown it is in no state to serve them.
		// Remembered for seconds, not minutes.
		m.taskSessions.putFailure(taskID)
		return taskRound{}, false
	case delivery.ChannelType != channelTypeWecom:
		m.taskSessions.put(taskID, taskRound{})
		return taskRound{}, false
	}
	task, err := m.tasks.GetAgentTask(ctx, id)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// A row that is not there has no chat session and never will — a run
		// cancelled and deleted while its transcript was still flushing.
		m.taskSessions.put(taskID, taskRound{})
		return taskRound{}, false
	case err != nil:
		m.taskSessions.putFailure(taskID)
		return taskRound{}, false
	}
	round := taskRound{session: task.ChatSessionID, installation: util.UUIDToString(delivery.InstallationID)}
	if task.ChatInputTaskID.Valid {
		round.round = util.UUIDToString(task.ChatInputTaskID)
	}
	m.taskSessions.put(taskID, round)
	return round, round.session.Valid
}

// showProgress puts a report into the bubble of the round it belongs to, and
// reports whether that round is on this replica. True means the report is
// settled here, shown or deliberately not; false means it is somebody else's.
//
// The round is looked up by the run that reported and then by the round it
// belongs to, which differs only for an auto-retry clone — FailTask gives the
// clone a fresh id and the round is still filed under the id the flush bound,
// which the clone inherited as chat_input_task_id. The same two names
// roundTaker tries when an ending arrives.
//
// Content is a full replacement, not a delta, so a frame carries the whole
// list and no caller has to know what the bubble says now. A report inside the
// feed's refresh interval reads nothing: the next one past it reads its rows
// too. And a refresh that merely fails leaves the spinner exactly as it was,
// for the answer to finish properly.
func (m *TypingIndicatorManager) showProgress(ctx context.Context, n progressNudge) bool {
	sessionID := n.target.session
	if m.streams == nil || !sessionID.Valid {
		return false
	}
	roundKey := n.taskID
	slot, here := m.streams.progressFor(sessionID, roundKey, n.taskID, n.seq)
	if !here && n.target.round != "" && n.target.round != n.taskID {
		roundKey = n.target.round
		slot, here = m.streams.progressFor(sessionID, roundKey, n.taskID, n.seq)
	}
	if !here {
		return false
	}
	if !slot.writable || slot.handle.Level == progressLevelNone || m.senders == nil {
		// No bubble to write into yet or any more, or a group, or a
		// colleague's own chat: the bubble stays as the opening frame painted
		// it and the answer seals it. Nothing in between is theirs to read,
		// and stopping here keeps a bubble that shows nothing from spending a
		// read and a socket write per tool call for the length of the run.
		return true
	}
	if !n.milestone && !n.trailing && n.seq > 0 && n.seq <= slot.folded {
		return true // a copy of a report already shown
	}
	h, feed, c := slot.handle, slot.feed, copyFor(slot.handle.Locale)
	added := false
	started := n.milestone && !slot.started && slot.folded == 0
	if started {
		// Once, and only before the transcript: a milestone that arrives after
		// the first tool call is not news any more.
		added = feed.fold(progressStep{kind: progressStarting}, c, h.Level)
	}
	through := slot.folded
	due := feed.due()
	if !n.milestone && due {
		taskID, err := util.ParseUUID(n.taskID)
		if err != nil || !taskID.Valid {
			return true
		}
		rows, err := m.tasks.ListTaskProgressSince(ctx, db.ListTaskProgressSinceParams{TaskID: taskID, AfterSeq: slot.folded})
		if err != nil {
			m.log.DebugContext(ctx, "wecom typing: could not read the run's progress",
				"task_id", n.taskID, "error", err)
		}
		for _, row := range rows {
			if step, ok := stepFromMessage(messageFromRow(row)); ok && feed.fold(step, c, h.Level) {
				added = true
			}
			if row.Seq > through {
				through = row.Seq
			}
		}
	}
	if through > slot.folded || started {
		m.streams.markFolded(sessionID, roundKey, n.taskID, through, started)
	}
	if !added && !feed.pending() {
		if !n.milestone && !due {
			// Rows this report pointed at are still unread.
			m.armTrailing(feed, n)
		}
		return true
	}
	if content := feed.flush(c, h.CreatedAt); content != "" {
		m.writeProgress(ctx, sessionID, h, content)
		return true
	}
	m.armTrailing(feed, n)
	return true
}

// armTrailing schedules one more pass over this round once the throttle
// window ends, for whatever the window held back. It goes through the lane
// like any report, so it is ordered with the run's other nudges, and it is not
// local: if the round has ended by then it finds nothing and goes nowhere.
func (m *TypingIndicatorManager) armTrailing(feed *progressFeed, n progressNudge) {
	if m.trailAfter == nil || m.progress == nil {
		return
	}
	wait, ok := feed.armTrail()
	if !ok {
		return
	}
	t := n
	t.seq, t.milestone, t.local, t.trailing = progressSeqTrailing, false, false, true
	m.trailAfter(wait, func() {
		feed.trailFired()
		m.progress.offer(t)
	})
}

// messageFromRow is taskMessageFields for a transcript row: the same four
// fields, so a step read back off the table is classified exactly as one
// carried on the event would be.
func messageFromRow(row db.ListTaskProgressSinceRow) taskMessage {
	msg := taskMessage{msgType: row.Type, tool: row.Tool.String, content: row.Content.String}
	if len(row.Input) > 0 {
		_ = json.Unmarshal(row.Input, &msg.input)
	}
	return msg
}

// writeProgress writes one refresh into the bubble. The installation is asked
// again with the writer in hand (guardReply), like every other frame of a
// reply: a revocation mid-run stops the steps at the next frame.
//
// The one thing a refresh does end is a bubble the server has disowned; see
// markUnusable for why that stop condition is not optional.
func (m *TypingIndicatorManager) writeProgress(ctx context.Context, sessionID pgtype.UUID, h streamHandle, content string) {
	err := m.senders.stream(guardReply(ctx, m.deliveries, h.InstallationID), h, content, false)
	switch {
	case err == nil:
	case errors.Is(err, errStreamBusy), errors.Is(err, errStreamSuperseded):
		// The previous frame is still in flight, or the answer got there
		// first. Skipping is the design in both cases.
	case errors.Is(err, errInstallationGone):
		m.log.InfoContext(ctx, "wecom typing: progress not shown, installation revoked",
			"chat_session_id", util.UUIDToString(sessionID),
			"installation_id", util.UUIDToString(h.InstallationID))
	case streamUnusable(err):
		if !m.streams.markUnusable(sessionID, h.StreamID) {
			// A refresh that raced into the same refusal got here first and
			// has already said it.
			return
		}
		m.log.WarnContext(ctx, "wecom typing: bubble disowned by the server, answering as a new message",
			"chat_session_id", util.UUIDToString(sessionID),
			"installation_id", util.UUIDToString(h.InstallationID), "error", err)
		// Nothing can close this bubble now — the stream takes no further
		// frame — so the spinner turns for good and the user is owed a word
		// about where the rest of the round is going to appear instead. Said
		// once: the mark is what makes this the only refresh that gets here.
		if sendErr := m.senders.sendTextCtx(guardReply(ctx, m.deliveries, h.InstallationID),
			h.InstallationID, h.ChatID, h.ChatType, copyFor(h.Locale).StreamStuck); sendErr != nil {
			m.log.WarnContext(ctx, "wecom typing: could not say the bubble is stuck",
				"chat_session_id", util.UUIDToString(sessionID), "error", sendErr)
		}
	default:
		m.log.DebugContext(ctx, "wecom typing: progress refresh failed",
			"chat_session_id", util.UUIDToString(sessionID), "error", err)
	}
}

// progressSeqOf reads what the subscriber needs off a task:message payload: its
// seq, and whether it is a kind the bubble shows at all (stepFromMessage).
// Typed in-process or in its map form after a serialization round trip (where
// a JSON number arrives as float64).
func progressSeqOf(payload any) (int32, bool) {
	var (
		msgType string
		seq     int
	)
	switch p := payload.(type) {
	case protocol.TaskMessagePayload:
		msgType, seq = p.Type, p.Seq
	case *protocol.TaskMessagePayload:
		if p == nil {
			return 0, false
		}
		msgType, seq = p.Type, p.Seq
	case map[string]any:
		msgType, _ = p["type"].(string)
		if f, ok := p["seq"].(float64); ok {
			seq = int(f)
		}
	default:
		return 0, false
	}
	// The kind alone decides it, and stepFromMessage is where that is
	// decided. ListTaskProgressSince names the same kinds in SQL;
	// TestTheTranscriptQueryReadsWhatTheBubbleShows holds the two together.
	if _, ok := stepFromMessage(taskMessage{msgType: msgType}); !ok {
		return 0, false
	}
	return int32(seq), true
}

// milestoneStep reads the step number off a task:progress payload, typed
// in-process or in its map form after a serialization round trip (where a JSON
// number arrives as float64). Zero when there is none.
func milestoneStep(payload any) int {
	switch p := payload.(type) {
	case protocol.TaskProgressPayload:
		return p.Step
	case map[string]any:
		if n, ok := p["step"].(float64); ok {
			return int(n)
		}
	}
	return 0
}

var (
	_ progressSink     = (*TypingIndicatorManager)(nil)
	_ progressAttacher = (*RelayOutbound)(nil)
)
