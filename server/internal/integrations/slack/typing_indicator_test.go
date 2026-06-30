package slack

import (
	"context"
	"fmt"
	"testing"
	"time"

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
	m.Add(context.Background(), db.ChannelInstallation{Config: slackInstallConfigJSON()}, sessionID, "C1", ts)

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
			m.Add(context.Background(), db.ChannelInstallation{Config: slackInstallConfigJSON()}, uid(7), tc.channelID, tc.ts)
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
	m.Add(context.Background(), db.ChannelInstallation{Config: slackInstallConfigJSON()}, sessionID, "C1", freshTS())

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
	m.Add(context.Background(), db.ChannelInstallation{Config: slackInstallConfigJSON()}, sessionID, "C1", freshTS())

	(&slackTypingNotifier{mgr: m}).OnSettled(context.Background(), sessionID)
	if len(fr.removed) != 1 || fr.removed[0].Channel != "C1" {
		t.Fatalf("OnSettled must clear the reaction, removed = %+v", fr.removed)
	}
}
