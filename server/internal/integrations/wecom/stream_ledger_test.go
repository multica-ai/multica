package wecom

// stream_ledger_test.go — the ledger's invariants, held shut from the outside.
//
// Four review rounds have found four different ways to break the same promise,
// and every one of them was a scenario nobody had written a test for. So these
// tests are not scenarios. They enumerate every terminal path a round has and
// assert the properties the ledger claims for all of them at once, which is the
// only shape that covers the fifth way before somebody finds it.

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// ---- the reconnect window ----

// TestAPromiseIsNotKeptUntilTheWordsAreAccepted is the round-4 regression,
// reproduced exactly as the review describes it: guard-close the round, clear
// the registered sender, deliver an empty completion (which returns
// errNoLiveConnection), reconnect, replay the completion.
//
// The guard told the user "还在处理，完成后我再单独回复你". The empty completion
// is the separate reply it promised — there is nothing to add, so the words are
// the no-reply copy — and a send refused because the WebSocket happened to be
// mid-reconnect must not count as having said them. WeCom redelivers, the
// sweeper repeats, and either way the next attempt is the last chance the
// promise has; a ledger that recorded the first attempt as delivered spends it
// on nothing and the asker waits for a reply that has already been filed as
// sent.
func TestAPromiseIsNotKeptUntilTheWordsAreAccepted(t *testing.T) {
	t.Parallel()
	rig := newBubbleRig(t)
	rig.ran(t, "REQ-1", 1, "task-1")

	// Nine minutes pass with the run still going: the guard takes the bubble
	// and leaves the promise behind.
	rig.guardClosed(t, 1)

	// The socket drops. Nothing else about the process changes — the store is
	// built at boot, not per connection.
	rig.senders.clear(rig.instID, rig.conn.sender)

	// The run finishes with nothing to say, into a registry with no sender.
	if err := rig.answerErr(t, "", "task-1"); err == nil {
		t.Fatal("delivering into an empty sender registry returned no error; " +
			"the reconnect window this test is about did not happen")
	}

	// The Supervisor reconnects and the same completion is redelivered.
	conn := rig.reconnect()
	if err := rig.answerErr(t, "", "task-1"); err != nil {
		t.Fatalf("replayed completion after the reconnect: %v", err)
	}

	got := pushedTexts(t, conn)
	if len(got) != 1 {
		t.Fatalf("the guard promised a separate reply and the user got %d message(s), want 1 — "+
			"the promise was recorded as kept by a send that was refused, so the replay found "+
			"nothing owed and stayed silent: %q", len(got), got)
	}
	if got[0] != streamCopyNoReply {
		t.Fatalf("the promised reply said %q, want %q", got[0], streamCopyNoReply)
	}
}

// answerErr is answer without the fatal: the reconnect-window tests are about
// what a refused send does to the ledger, so the error is the subject.
func (r *bubbleRig) answerErr(t *testing.T, content, taskName string) error {
	t.Helper()
	return r.out.processEvent(context.Background(), events.Event{
		ChatSessionID: bubbleSession,
		TaskID:        taskUUID(t, taskName),
		Payload:       protocol.ChatDonePayload{Content: content},
	})
}

// ---- the invariants, over every terminal path ----

// terminalPath is one way a round can end. Between them these are all of them:
// the two subscribers the manager registers, the chat-done subscriber, and each
// of those against a round that still has its bubble and a round the guard has
// already closed on a promise.
type terminalPath struct {
	name string
	// afterTheGuard runs the nine-minute guard on this round first, so the
	// ending arrives to a promise rather than a bubble.
	afterTheGuard bool
	// fire publishes the ending for the round bound to task-1, the way its
	// real publisher does.
	fire func(t *testing.T, rig *bubbleRig)
	// want is what the person in the chat has to end up reading.
	want string
}

func terminalPaths() []terminalPath {
	answer := func(content string) func(*testing.T, *bubbleRig) {
		return func(t *testing.T, rig *bubbleRig) {
			t.Helper()
			_ = rig.answerErr(t, content, "task-1")
		}
	}
	failed := func(t *testing.T, rig *bubbleRig) { t.Helper(); rig.failed(t, "task-1", false) }
	cancelled := func(t *testing.T, rig *bubbleRig) { t.Helper(); rig.cancelled(t, "task-1") }

	return []terminalPath{
		{name: "an answer", fire: answer("the answer"), want: "the answer"},
		{name: "an empty answer", fire: answer(""), want: streamCopyNoReply},
		{name: "a failure", fire: failed, want: streamCopyFailed},
		{name: "a cancellation", fire: cancelled, want: streamCopyCancelled},
		{name: "an answer after the guard", afterTheGuard: true, fire: answer("the answer"), want: "the answer"},
		{name: "an empty answer after the guard", afterTheGuard: true, fire: answer(""), want: streamCopyNoReply},
		{name: "a failure after the guard", afterTheGuard: true, fire: failed, want: streamCopyFailed},
		{name: "a cancellation after the guard", afterTheGuard: true, fire: cancelled, want: streamCopyCancelled},
	}
}

// controlRound is a SECOND run of the same session, set up alongside the one
// under test and never touched by it. The two shapes are the two ways a run can
// be reached once its ending arrives, and each is what a different earlier
// defect took something from.
type controlRound struct {
	name string
	// setUp arranges the control round as task-2.
	setUp func(t *testing.T, rig *bubbleRig)
}

func controlRounds() []controlRound {
	return []controlRound{
		{
			// A promise outstanding, sitting at the HEAD of the owed list — the
			// entry a claim that spends the head instead of its own takes.
			name: "a round still owed the guard's promise",
			setUp: func(t *testing.T, rig *bubbleRig) {
				t.Helper()
				rig.ran(t, "REQ-2", 2, "task-2")
				rig.guardClosed(t, 2)
			},
		},
		{
			// No bubble, no promise, nothing on file: this run's ending has to
			// find its chat in the binding row. It is the one a dedup keyed by
			// "this session has a note" silences, because by the time it
			// arrives the round under test has left a note of its own.
			name:  "a round this process holds nothing for",
			setUp: func(t *testing.T, rig *bubbleRig) { t.Helper() },
		},
	}
}

// setUpTwoRounds gives a session the round under test, bound to task-1, plus a
// control round on task-2 that nothing the tested path does is allowed to
// touch: not spend its promise, not file an ending in its name, not leave a
// note that silences it.
func setUpTwoRounds(t *testing.T, p terminalPath, c controlRound) *bubbleRig {
	t.Helper()
	rig := newBoundRoomRig(t)
	rig.askedInTheRoom(t, "task-1")
	rig.askedInTheRoom(t, "task-2")
	rig.ran(t, "REQ-1", 1, "task-1")
	c.setUp(t, rig)
	if p.afterTheGuard {
		rig.guardClosed(t, 1)
	}
	return rig
}

// TestEveryTerminalPathEndsInWordsOrLeavesTheRunOwed is the ledger's contract,
// asserted as a property of all of its paths rather than as a story about one.
//
// Four rounds of review have found four ways to break the same promise, and
// each was fixed with a scenario test that could not have caught the next one:
// a FIFO claim, a delivery path that settled nothing, dedup keyed by session,
// a claim recorded before its send. All four are instances of two properties
// this table checks on every path at once — what the user ends up reading, and
// what a run that was never spoken for is still owed.
func TestEveryTerminalPathEndsInWordsOrLeavesTheRunOwed(t *testing.T) {
	t.Parallel()
	for _, p := range terminalPaths() {
		// I1 and the not-twice rule: the words go out once, and a second
		// publisher of the same ending adds nothing. task:failed has two
		// publishers and a sweeper tick can repeat either, and WeCom redelivers
		// callbacks after a reconnect, so every one of these arrives twice in
		// production sooner or later.
		t.Run(p.name+"/says it once", func(t *testing.T) {
			t.Parallel()
			rig := setUpTwoRounds(t, p, controlRounds()[0])
			before := len(said(t, rig.conn))

			p.fire(t, rig)
			if got := said(t, rig.conn)[before:]; len(got) != 1 || got[0] != p.want {
				t.Fatalf("%s left the user reading %q, want [%q]", p.name, got, p.want)
			}
			p.fire(t, rig)
			if got := said(t, rig.conn)[before:]; len(got) != 1 {
				t.Fatalf("%s republished brought the user to %q, want one message — "+
					"the second publisher of one run's ending told them twice", p.name, got)
			}
		})

		// The other half of the not-twice rule, and the one a replay cannot
		// reach: once a run's ending has been said, no OTHER ending for that
		// same run may speak. A run has several publishers with different
		// copy — an answer, a failure, a cancel — and the promise a
		// guard-closed round left is spent by whichever of them arrives; a
		// path that delivers without settling leaves it on the list for the
		// next one, which then contradicts what the user has just read.
		t.Run(p.name+"/nothing else speaks for the run afterwards", func(t *testing.T) {
			t.Parallel()
			rig := setUpTwoRounds(t, p, controlRounds()[0])
			p.fire(t, rig)
			before := len(said(t, rig.conn))

			rig.failed(t, "task-1", false)
			rig.cancelled(t, "task-1")
			if got := said(t, rig.conn)[before:]; len(got) != 0 {
				t.Fatalf("after %s the same run's other endings added %q — "+
					"the user reads a second, contradicting account of one run", p.name, got)
			}
		})

		// I3: a delivery nothing accepted is not a delivery. The ledger has to
		// come back unchanged, so the run is still owed its ending and the next
		// publisher says it. This is the shape of the reconnect window — the
		// registry is momentarily empty while the Supervisor redials — and it
		// is the only window in which every one of these paths is silent.
		t.Run(p.name+"/a refused delivery is not a delivery", func(t *testing.T) {
			t.Parallel()
			rig := setUpTwoRounds(t, p, controlRounds()[0])
			before := len(said(t, rig.conn))

			rig.senders.clear(rig.instID, rig.conn.sender)
			p.fire(t, rig)
			if got := said(t, rig.conn)[before:]; len(got) != 0 {
				t.Fatalf("%s wrote %q with no live connection", p.name, got)
			}

			conn := rig.reconnect()
			p.fire(t, rig)
			if got := said(t, conn); len(got) != 1 || got[0] != p.want {
				t.Fatalf("after a refused %s the user ended up reading %q, want [%q] — "+
					"the ending was recorded as said by a send nothing accepted, so the "+
					"next publisher of it found the run already spoken for", p.name, got, p.want)
			}
		})

		// I2: matched by the run's own id and by nothing else. A second run of
		// the same session is what every earlier defect took something from —
		// the head of the owed list a FIFO claim spends, the promise a path
		// that settles nothing leaves lying about, the session note a dedup
		// keyed by session reads as this run's own. So each path is run against
		// both shapes a second run can be in, and neither is allowed to lose
		// its own ending to the first round's.
		for _, c := range controlRounds() {
			t.Run(p.name+"/leaves "+c.name+" alone", func(t *testing.T) {
				t.Parallel()
				rig := setUpTwoRounds(t, p, c)
				p.fire(t, rig)
				before := len(said(t, rig.conn))

				rig.failed(t, "task-2", false)
				if got := said(t, rig.conn)[before:]; len(got) != 1 || got[0] != streamCopyFailed {
					t.Fatalf("after %s on the first round, %s was left reading %q for its own "+
						"failure, want [%q] — the first round's ending is what became of it",
						p.name, c.name, got, streamCopyFailed)
				}
			})
		}
	}
}

// said is everything the person in the chat ended up reading on a connection:
// the text of every sealed bubble and every plain message, in write order.
//
// Both, deliberately. A closing frame and a push are the same thing to the
// reader, and they are how the same words reach them depending on whether the
// bubble survived — so a ledger test watching only one of them would call a
// path silent while its words were on the screen, or count one ending twice.
// The opening frame is not in here: it carries no words, only the spinner.
func said(t *testing.T, c *bubbleConn) []string {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	out := []string{}
	for _, f := range c.frames {
		var body map[string]any
		if err := json.Unmarshal(f.Body, &body); err != nil {
			t.Fatalf("decode frame body: %v", err)
		}
		switch f.Cmd {
		case cmdRespondMsg:
			stream, _ := body["stream"].(map[string]any)
			if stream == nil || stream["finish"] != true {
				continue
			}
			s, _ := stream["content"].(string)
			out = append(out, s)
		case cmdSendMsg:
			md, _ := body["markdown"].(map[string]any)
			if md == nil {
				continue
			}
			s, _ := md["content"].(string)
			out = append(out, s)
		}
	}
	return out
}
