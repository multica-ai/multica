package slack

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/slack-go/slack"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// fakeReactor records reaction add/remove calls so a test can assert the
// indicator's lifecycle without a live Slack API client.
type fakeReactor struct {
	addName string
	added   []slack.ItemRef
	removed []slack.ItemRef
	addErr  error
}

func (f *fakeReactor) AddReactionContext(_ context.Context, name string, item slack.ItemRef) error {
	f.addName = name
	f.added = append(f.added, item)
	return f.addErr
}

func (f *fakeReactor) RemoveReactionContext(_ context.Context, _ string, item slack.ItemRef) error {
	f.removed = append(f.removed, item)
	return nil
}

func newTestTyping(q TypingIndicatorQueries, fr *fakeReactor) *TypingIndicatorManager {
	m := NewTypingIndicatorManager(q, nil, nil)
	m.newAPI = func(credentials) reactionAPI { return fr }
	return m
}

// freshTS / staleTS build Slack ts strings ("<seconds>.<micros>") relative to
// now so the max-age guard can be exercised deterministically.
func freshTS() string { return fmt.Sprintf("%d.000100", time.Now().Unix()) }
func staleTS() string {
	return fmt.Sprintf("%d.000100", time.Now().Add(-5*time.Minute).Unix())
}

func TestTypingIndicator_AddThenClear(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		binding: db.ChannelChatSessionBinding{InstallationID: uid(1)},
		inst:    db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)

	ts := freshTS()
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", ts)

	if len(fr.added) != 1 || fr.added[0].Channel != "C1" || fr.added[0].Timestamp != ts {
		t.Fatalf("add reaction = %+v, want one on C1/%s", fr.added, ts)
	}
	if fr.addName != typingEmoji {
		t.Errorf("emoji = %q, want %q", fr.addName, typingEmoji)
	}

	m.Clear(context.Background(), sessionID)
	if len(fr.removed) != 1 || fr.removed[0].Channel != "C1" || fr.removed[0].Timestamp != ts {
		t.Fatalf("remove reaction = %+v, want one on C1/%s", fr.removed, ts)
	}

	// State is dropped on clear, so a second clear is a no-op.
	m.Clear(context.Background(), sessionID)
	if len(fr.removed) != 1 {
		t.Errorf("second clear must be a no-op, removed %d times", len(fr.removed))
	}
}

func TestTypingIndicator_SkipsStaleAndEmpty(t *testing.T) {
	cases := []struct {
		name      string
		channelID string
		ts        string
	}{
		{"stale message (replayed reconnect)", "C1", staleTS()},
		{"empty ts", "C1", ""},
		{"empty channel", "", freshTS()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fr := &fakeReactor{}
			m := newTestTyping(&fakeOutboundQueries{}, fr)
			m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, uid(7), tc.channelID, tc.ts)
			if len(fr.added) != 0 {
				t.Errorf("%s: must not add a reaction, added %d", tc.name, len(fr.added))
			}
			// Nothing recorded → a clear has nothing to remove.
			m.Clear(context.Background(), uid(7))
			if len(fr.removed) != 0 {
				t.Errorf("%s: clear must be a no-op, removed %d", tc.name, len(fr.removed))
			}
		})
	}
}

func TestTypingIndicator_ClearsOnTaskFailed(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		binding: db.ChannelChatSessionBinding{InstallationID: uid(1)},
		inst:    db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", freshTS())

	// EventTaskFailed carries the session id only in the broadcast payload map,
	// not on the envelope — the clear handler must read it from there.
	m.handleEvent(events.Event{
		Type:    protocol.EventTaskFailed,
		Payload: map[string]any{"chat_session_id": util.UUIDToString(sessionID)},
	})
	if len(fr.removed) != 1 {
		t.Fatalf("task-failed event must clear the reaction, removed %d", len(fr.removed))
	}
}

func TestTypingIndicator_IgnoresNonChatEvent(t *testing.T) {
	fr := &fakeReactor{}
	m := newTestTyping(&fakeOutboundQueries{}, fr)
	// An issue/autopilot event with no chat session must not even reach a binding
	// lookup, let alone a reaction removal.
	m.handleEvent(events.Event{Type: protocol.EventTaskFailed, Payload: map[string]any{"task_id": "t1"}})
	if len(fr.removed) != 0 {
		t.Errorf("non-chat event must be ignored, removed %d", len(fr.removed))
	}
}

// When the run trigger enqueues no task (agent offline / archived), no task
// lifecycle event ever publishes, so the engine clears the indicator through the
// notifier's OnSettled instead of the bus.
func TestSlackTypingNotifier_OnSettledClears(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		binding: db.ChannelChatSessionBinding{InstallationID: uid(1)},
		inst:    db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", freshTS())

	(&slackTypingNotifier{mgr: m}).OnSettled(context.Background(), sessionID)
	if len(fr.removed) != 1 || fr.removed[0].Channel != "C1" {
		t.Fatalf("OnSettled must clear the reaction, removed = %+v", fr.removed)
	}
}

// A cancelled run publishes neither chat-done nor task-failed, so the bus
// subscription is the only thing that can ever take the reaction back off.
//
// Driven through Register + Publish rather than by calling handleEvent, which
// is what the sibling tests above do. handleEvent runs identically whether or
// not the manager is subscribed to task:cancelled, so a test written that way
// passes with the fix reverted — it proves the handler works and says nothing
// about whether the event reaches it. Only a real publish can fail for the
// reason the user experiences.
func TestTypingIndicator_ClearsOnTaskCancelled(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		binding: db.ChannelChatSessionBinding{InstallationID: uid(1)},
		inst:    db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)
	bus := events.New()
	m.Register(bus)

	ts := freshTS()
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", ts)

	// The shape a cancel is published with: ids on the envelope
	// and in the payload map, status "cancelled", and no content of any kind.
	bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        util.UUIDToString(uid(9)),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload: map[string]any{
			"task_id":         util.UUIDToString(uid(9)),
			"chat_session_id": util.UUIDToString(sessionID),
			"status":          "cancelled",
		},
	})

	if len(fr.removed) != 1 {
		t.Fatalf("the run was cancelled and the :%s: reaction is still on C1/%s — "+
			"the user is watching a typing indicator spin for an answer nobody is "+
			"producing (removed %d reactions)", typingEmoji, ts, len(fr.removed))
	}
	if fr.removed[0].Channel != "C1" || fr.removed[0].Timestamp != ts {
		t.Errorf("cleared the wrong message: %+v, want C1/%s", fr.removed[0], ts)
	}
}

// Deleting a chat session cancels its queued turns and deletes the Slack binding
// in one transaction, then broadcasts the cancels after that transaction
// commits. By the time this manager sees them the binding row is gone, so a
// clear that resolves its credentials through the binding finds nothing — and
// because the state is taken from the map first, the reaction cannot be cleared
// by anything afterwards either. The reaction has to come off from what the add
// already recorded.
func TestTypingIndicator_ClearsAfterSessionDeleteRemovedTheBinding(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)
	bus := events.New()
	m.Register(bus)

	ts := freshTS()
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", ts)

	// The session and its binding are deleted; the cancel is broadcast after.
	q.binding = db.ChannelChatSessionBinding{}
	q.bindingErr = pgx.ErrNoRows

	bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        util.UUIDToString(uid(9)),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload: map[string]any{
			"task_id":         util.UUIDToString(uid(9)),
			"chat_session_id": util.UUIDToString(sessionID),
			"status":          "cancelled",
		},
	})

	if len(fr.removed) != 1 {
		t.Fatalf("the session was deleted and the :%s: reaction is still on C1/%s — "+
			"deleting a conversation left a processing badge on the message that "+
			"started it (removed %d reactions)", typingEmoji, ts, len(fr.removed))
	}
	if fr.removed[0].Channel != "C1" || fr.removed[0].Timestamp != ts {
		t.Errorf("cleared the wrong message: %+v, want C1/%s", fr.removed[0], ts)
	}
}

// The reply path asks TaskInputIsChannelIngested whether an answer belongs on
// Slack, and a task cancelled for owning an empty input batch answers no — it
// has no user rows at all, so nothing in it is stamped channel-ingested. That
// is the run of #6611, and the run whose badge most needs removing.
//
// The clear must not consult it. Nothing is posted for a cancellation, so there
// is no answer to misroute and no question for the gate to decide; the badge is
// this process's own and Clear only touches sessions it put one on. The fake is
// set to the answers that gate would give — a task owning a batch with no
// channel-ingested message — so adding the gate to this path breaks this test.
func TestTypingIndicator_ClearsOnCancelOfATaskThatReadsAsAWebRun(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		binding:             db.ChannelChatSessionBinding{InstallationID: uid(1)},
		inst:                db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
		task:                db.AgentTaskQueue{ChatInputTaskID: uid(9)},
		taskChannelIngested: false,
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)
	bus := events.New()
	m.Register(bus)

	ts := freshTS()
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", ts)

	bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        util.UUIDToString(uid(9)),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload: map[string]any{
			"task_id":         util.UUIDToString(uid(9)),
			"chat_session_id": util.UUIDToString(sessionID),
			"status":          "cancelled",
		},
	})

	if len(fr.removed) != 1 {
		t.Fatalf("the cancelled run owned an empty input batch, so the origin query "+
			"calls it a web run, and the :%s: reaction stayed on C1/%s — the badge is "+
			"stuck on exactly the cancel it was added for (removed %d reactions)",
			typingEmoji, ts, len(fr.removed))
	}
	if fr.removed[0].Channel != "C1" || fr.removed[0].Timestamp != ts {
		t.Errorf("cleared the wrong message: %+v, want C1/%s", fr.removed[0], ts)
	}
}

// A runtime teardown deletes the installation inside the transaction that
// cancels its tasks, so by the time the cancel reaches Clear the row the
// credentials come from is gone. The reaction is still on the message and the
// state has already been taken off the map, so the snapshot taken at add time
// is the only thing left that can remove it.
//
// The reviewer's repro, kept as the test.
func TestTypingIndicator_TeardownDeletedInstallationStillClears(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)

	ts := freshTS()
	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", ts)
	if len(fr.added) != 1 {
		t.Fatalf("setup: the reaction should be on the message; added = %d", len(fr.added))
	}

	// The teardown transaction has committed: the installation is gone.
	q.instErr = pgx.ErrNoRows

	bus := events.New()
	m.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        util.UUIDToString(uid(9)),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload: map[string]any{
			"task_id":         util.UUIDToString(uid(9)),
			"chat_session_id": util.UUIDToString(sessionID),
			"status":          "cancelled",
		},
	})

	if len(fr.removed) != 1 {
		t.Fatalf("the runtime was torn down and its tasks cancelled, but the reaction is still on C1/%s "+
			"with nothing left to take it off (removes = %d)", ts, len(fr.removed))
	}
}

// The snapshot must not shadow a live row: a credential rotation between add and
// clear is picked up by the lookup, and a snapshot used unconditionally would
// clear through the app's old secret.
func TestTypingIndicator_ALiveInstallationIsPreferredOverTheSnapshot(t *testing.T) {
	sessionID := uid(7)
	q := &fakeOutboundQueries{
		inst: db.ChannelInstallation{ID: uid(1), Status: "active", Config: slackInstallConfigJSON()},
	}
	fr := &fakeReactor{}
	m := newTestTyping(q, fr)

	m.Add(context.Background(), db.ChannelInstallation{ID: uid(1), Config: slackInstallConfigJSON()}, sessionID, "C1", freshTS())

	// A lookup error that is NOT "the row is gone" must still refuse: it says
	// nothing about whether the installation exists.
	q.instErr = errors.New("connection reset")

	bus := events.New()
	m.Register(bus)
	bus.Publish(events.Event{
		Type:          protocol.EventTaskCancelled,
		TaskID:        util.UUIDToString(uid(9)),
		ChatSessionID: util.UUIDToString(sessionID),
		Payload: map[string]any{
			"task_id":         util.UUIDToString(uid(9)),
			"chat_session_id": util.UUIDToString(sessionID),
			"status":          "cancelled",
		},
	})

	if len(fr.removed) != 0 {
		t.Fatalf("a transient lookup failure fell back to the snapshot; removes = %d", len(fr.removed))
	}
}
