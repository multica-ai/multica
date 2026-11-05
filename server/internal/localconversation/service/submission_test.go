package service

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/multica-ai/multica/server/internal/localconversation/provider/claude"
	"github.com/multica-ai/multica/server/internal/localconversation/store"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

func TestSubmitPreservesPauseAndRemovedIdentity(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "claude")
	ctx := context.Background()
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv", queued("old", "old"), true, TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("new", "new"), TurnOrigin{})
	if err != nil || result.Submission.State != "queued" || !result.Queue.Paused {
		t.Fatalf("paused submit=%+v %v", result, err)
	}
	if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "remove", "new"); err != nil {
		t.Fatal(err)
	}
	result, err = f.svc.Submit(ctx, f.scopeID, "conv", queued("new", "new"), TurnOrigin{})
	if err != nil || result.Submission.State != "removed" || string(result.Submission.Item) != "{}" {
		t.Fatalf("removed retry=%+v %v", result, err)
	}
	if len(f.sends(t)) != 0 {
		t.Fatal("paused input ran")
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
	if _, err := f.svc.Enqueue(ctx, f.scopeID, "conv", queued("waiting", "body"), true, TurnOrigin{}); err != nil {
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
