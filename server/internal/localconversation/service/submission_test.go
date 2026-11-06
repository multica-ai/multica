package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
)

func TestSubmitReservesBeforeSlowPreparationAndQueuesOtherDevices(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "claude")
	block := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(block) }) }
	defer release()
	f.beforeLaunch = func() { <-block }
	ctx := context.Background()
	result, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("first", "A"), TurnOrigin{PeerID: "phone", ClientKind: "mobile"})
	if err != nil || result.Submission.State != "starting" || result.Submission.Attempt == nil {
		t.Fatalf("first=%+v, %v", result, err)
	}
	if result.SnapshotSeq == 0 || result.QueueRevision == 0 {
		t.Fatalf("missing durable revision: %+v", result)
	}
	next, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("second", "B"), TurnOrigin{PeerID: "computer"})
	if err != nil || next.Submission.State != "queued" {
		t.Fatalf("second=%+v, %v", next, err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv", RequestID: "direct", Text: "C"}); !errors.Is(err, ErrTurnRunning) {
		t.Fatalf("direct send bypassed reservation: %v", err)
	}
	replay, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("first", "A"), TurnOrigin{})
	if err != nil || replay.Submission.Attempt.AttemptID != result.Submission.Attempt.AttemptID {
		t.Fatalf("duplicate changed attempt: %+v %v", replay, err)
	}
	if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("first", "different"), TurnOrigin{}); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("conflict=%v", err)
	}
	release()
	eventually(t, "first input", func() bool { return len(f.sends(t)) == 1 })
	f.adapters[0].emit(t, turnDone)
	eventually(t, "second input", func() bool { return len(f.sends(t)) == 2 })
	states, err := f.svc.Submissions(ctx, f.scopeID, "conv", []string{"first", "second"})
	if err != nil || len(states.Submissions) != 2 {
		t.Fatalf("statuses=%+v %v", states, err)
	}
	for _, s := range states.Submissions {
		if s.State != "attached" || len(s.Attempt.TurnIDs) != 1 {
			t.Fatalf("not attached: %+v", s)
		}
	}
}

func TestSubmitResumesUserPausedQueue(t *testing.T) {
	for _, pending := range []bool{false, true} {
		t.Run(fmt.Sprintf("pending=%t", pending), func(t *testing.T) {
			f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
			f.create(t, "conv", "claude")
			ctx := context.Background()
			if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "pause", ""); err != nil {
				t.Fatal(err)
			}
			if pending {
				if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv", queued("old", "old"), true, TurnOrigin{}); err != nil {
					t.Fatal(err)
				}
			}
			result, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("new", "new"), TurnOrigin{})
			if err != nil || result.Queue.Paused || result.Queue.Error != "" {
				t.Fatalf("submit did not resume: %+v %v", result, err)
			}
			wantState, wantFirst := "starting", "[new]"
			if pending {
				wantState, wantFirst = "queued", "[old]"
			}
			if result.Submission.State != wantState {
				t.Fatalf("state=%s; want %s", result.Submission.State, wantState)
			}
			eventually(t, "first input", func() bool { return fmt.Sprint(f.sends(t)) == wantFirst })
			if pending {
				f.allAdapters()[0].emit(t, turnDone)
				eventually(t, "new input after old input", func() bool { return fmt.Sprint(f.sends(t)) == "[old new]" })
			}
		})
	}
}

func TestSubmitResumesAfterStopWithoutOverlappingTurns(t *testing.T) {
	for _, completed := range []bool{false, true} {
		t.Run(fmt.Sprintf("completed=%t", completed), func(t *testing.T) {
			f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
			f.create(t, "conv", "claude")
			ctx := context.Background()
			if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("first", "first"), TurnOrigin{}); err != nil {
				t.Fatal(err)
			}
			eventually(t, "first attached", func() bool {
				v, err := f.svc.Submissions(ctx, f.scopeID, "conv", []string{"first"})
				return err == nil && len(v.Submissions) == 1 && v.Submissions[0].State == "attached"
			})
			eventually(t, "first input", func() bool { return fmt.Sprint(f.sends(t)) == "[first]" })
			if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "stop", "", "stop-first"); err != nil {
				t.Fatal(err)
			}
			adapter := f.allAdapters()[0]
			if !slices.Contains(adapter.sent(), "interrupt") {
				t.Fatal("stop did not interrupt the running turn")
			}
			finish := func() {
				adapter.emit(t, `{"type":"result","subtype":"error_during_execution","is_error":true,"errors":["interrupted"]}`)
			}
			if completed {
				finish()
				eventually(t, "first stopped", func() bool { busy, err := f.svc.executionBusy(ctx, "conv"); return err == nil && !busy })
			}
			result, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("second", "second"), TurnOrigin{})
			if err != nil || result.Queue.Paused {
				t.Fatalf("submit after stop=%+v %v", result, err)
			}
			if !completed {
				if result.Submission.State != "queued" || fmt.Sprint(f.sends(t)) != "[first]" {
					t.Fatalf("new input overlapped stopping turn: %+v, %v", result, f.sends(t))
				}
				finish()
			}
			eventually(t, "second input", func() bool { return fmt.Sprint(f.sends(t)) == "[first second]" })
		})
	}
}

func TestSubmitReplayPreservesPauseAndRemovedIdentity(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "claude")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv", RequestID: "running", Text: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("new", "new"), TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	paused, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "pause", "")
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("new", "new"), TurnOrigin{})
	if err != nil || result.Submission.State != "queued" || !result.Queue.Paused || result.QueueRevision != paused.Revision {
		t.Fatalf("replay changed pause=%+v %v", result, err)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "remove", "new"); err != nil {
		t.Fatal(err)
	}
	result, err = f.svc.Submit(ctx, f.scopeID, "conv", queued("new", "new"), TurnOrigin{})
	if err != nil || result.Submission.State != "removed" || string(result.Submission.Item) != "{}" || !result.Queue.Paused {
		t.Fatalf("removed retry=%+v %v", result, err)
	}
	if fmt.Sprint(f.sends(t)) != "[running]" {
		t.Fatal("replayed input ran")
	}
}

func TestSubmitPreparingFailureKeepsOriginalDisplayAndCanRetry(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "unsupported")
	ctx := context.Background()
	raw := json.RawMessage(`{"requestId":"r","content":"A","display":{"id":"r","clientSubmitId":"r","content":[{"type":"text","text":"Keep this"}],"createdAtUnixMs":1}}`)
	if _, err := f.svc.Submit(ctx, f.scopeID, "conv", raw, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "preparation failure", func() bool {
		v, e := f.svc.Queue(ctx, f.scopeID, "conv")
		return e == nil && v.Paused && len(v.Items) == 1
	})
	v, err := f.svc.Queue(ctx, f.scopeID, "conv")
	if err != nil {
		t.Fatal(err)
	}
	if string(v.Items[0]) != string(raw) {
		t.Fatalf("display changed: %s", v.Items[0])
	}
	states, _ := f.svc.Submissions(ctx, f.scopeID, "conv", []string{"r"})
	if states.Submissions[0].State != "queued" {
		t.Fatalf("state=%+v", states)
	}
	next, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("next", "next"), TurnOrigin{})
	if err != nil || !next.Queue.Paused || next.Queue.Error != v.Error || next.Submission.State != "queued" {
		t.Fatalf("new submit retried preparation failure: %+v %v", next, err)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "resume", ""); err != nil {
		t.Fatal(err)
	}
	eventually(t, "next failed attempt", func() bool {
		states, e := f.svc.Submissions(ctx, f.scopeID, "conv", []string{"r"})
		return e == nil && states.Submissions[0].Revision > v.Revision
	})
}

func TestSubmissionSurvivesRestartWithoutResending(t *testing.T) {
	path := filepath.Join(t.TempDir(), "c.sqlite")
	f := newServiceFixture(t, path)
	f.create(t, "conv", "claude")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv", RequestID: "running", Text: "running"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv", queued("waiting", "body"), false, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Close(ctx); err != nil {
		t.Fatal(err)
	}
	again := newServiceFixture(t, path)
	states, err := again.svc.Submissions(ctx, again.scopeID, "conv", []string{"waiting"})
	if err != nil || len(states.Submissions) != 1 || states.Submissions[0].State != "queued" {
		t.Fatalf("restored=%+v %v", states, err)
	}
	time.Sleep(20 * time.Millisecond)
	if len(again.sends(t)) != 0 {
		t.Fatal("restart replayed queue")
	}
	next, err := again.svc.Submit(ctx, again.scopeID, "conv", queued("new", "new"), TurnOrigin{})
	if err != nil || !next.Queue.Paused || next.Queue.Error != queuePausedByRestart || next.Submission.State != "queued" {
		t.Fatalf("new submit resumed a restart hold: %+v %v", next, err)
	}
	again.svc.dispatchQueue("conv")
	if len(again.sends(t)) != 0 {
		t.Fatal("new submit replayed restored messages")
	}
}

func TestQueueControlsAreDurableAndDuplicateStopCannotStopANewerTurn(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "claude")
	ctx := context.Background()
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv", RequestID: "first", Text: "first"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "stop", "", "stop-operation"); err != nil {
		t.Fatal(err)
	}
	f.adapters[0].emit(t, turnDone)
	eventually(t, "first completion", func() bool { busy, _ := f.svc.executionBusy(ctx, "conv"); return !busy })
	if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "resume", "", "resume-operation"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.Send(ctx, SendCommand{ScopeID: f.scopeID, ConversationID: "conv", RequestID: "second", Text: "second"}); err != nil {
		t.Fatal(err)
	}
	view, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "stop", "", "stop-operation")
	if err != nil || view.Paused {
		t.Fatalf("replayed old stop: %+v %v", view, err)
	}
	if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "pause", "", "stop-operation"); !errors.Is(err, ErrRequestConflict) {
		t.Fatalf("operation conflict=%v", err)
	}
	if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "remove", "late-request", "remove-operation"); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("late-request", "late"), TurnOrigin{})
	if err != nil || result.Submission.State != "removed" {
		t.Fatalf("removed before arrival=%+v %v", result, err)
	}
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv", queued("late-request", "late"), true, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	view, _ = f.svc.Queue(ctx, f.scopeID, "conv")
	if len(view.Items) != 0 {
		t.Fatalf("legacy resurrected removed item: %+v", view)
	}
}

func TestUnknownSubmissionStaysVisibleAndExplicitRemovalReleasesIt(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "claude")
	f.reply = func(_ *fakeAdapter, typ string) (json.RawMessage, error) {
		if typ == "send" {
			return nil, &claude.AdapterError{Code: "closed", Message: "unconfirmed", Dispatch: "outcome_unknown"}
		}
		return json.RawMessage(`{}`), nil
	}
	ctx := context.Background()
	if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("r", "keep original"), TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "unknown delivery", func() bool {
		v, e := f.svc.Submissions(ctx, f.scopeID, "conv", []string{"r"})
		return e == nil && len(v.Submissions) == 1 && v.Submissions[0].State == "recovery_required"
	})
	duplicate, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("r", "keep original"), TurnOrigin{})
	if err != nil || len(duplicate.Queue.Items) != 1 || duplicate.Queue.Error == "" {
		t.Fatalf("duplicate lost recovery queue: %+v %v", duplicate, err)
	}
	view, err := f.svc.Queue(ctx, f.scopeID, "conv")
	if err != nil || len(view.Items) != 1 {
		t.Fatalf("recovery item invisible: %+v %v", view, err)
	}
	if _, err := f.svc.Store().Execution(ctx, "conv"); err != nil {
		t.Fatalf("unknown result released execution: %v", err)
	}
	next, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("next", "next"), TurnOrigin{})
	if err != nil || next.Submission.State != "queued" {
		t.Fatalf("new submit bypassed unknown outcome: %+v %v", next, err)
	}
	f.svc.dispatchQueue("conv")
	slot, err := f.svc.Store().Execution(ctx, "conv")
	if err != nil || slot.RequestID != "r" || len(f.sends(t)) != 1 {
		t.Fatalf("new submit released unknown execution: %+v %v, sends=%v", slot, err, f.sends(t))
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "remove", "next"); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "remove", "r", "remove-unknown"); err != nil {
		t.Fatal(err)
	}
	states, err := f.svc.Submissions(ctx, f.scopeID, "conv", []string{"r"})
	if err != nil || states.Submissions[0].State != "removed" {
		t.Fatalf("resolve=%+v %v", states, err)
	}
	if _, err := f.svc.Store().Execution(ctx, "conv"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("resolved slot=%v", err)
	}
}

func TestFailedSubmissionRestoredAtFrontHasOneQueueIdentity(t *testing.T) {
	ids := requestIDsAfter(store.QueueState{Items: []store.QueueItem{{RequestID: "later"}}}, store.QueueChange{Append: &store.QueueItem{RequestID: "retry"}, Front: "retry"})
	if len(ids) != 2 || ids[0] != "retry" || ids[1] != "later" {
		t.Fatalf("queue identities=%v", ids)
	}
}
