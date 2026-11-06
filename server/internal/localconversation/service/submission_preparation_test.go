package service

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/localconversation/store/sqlitedb"
)

func TestSubmitAfterStopDuringPreparation(t *testing.T) {
	for _, tc := range []struct {
		name       string
		settled    bool
		pauseAgain bool
	}{
		{name: "submit_before_stop_settles"},
		{name: "submit_after_stop_settles", settled: true},
		{name: "later_pause_is_preserved", pauseAgain: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
			f.create(t, "conv", "claude")
			entered, block := make(chan struct{}), make(chan struct{})
			var enter, unblock sync.Once
			release := func() { unblock.Do(func() { close(block) }) }
			defer release()
			f.beforeLaunch = func() { enter.Do(func() { close(entered) }); <-block }
			ctx := context.Background()
			if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("first", "first"), TurnOrigin{}); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(5 * time.Second):
				t.Fatal("provider preparation did not start")
			}
			if _, err := f.svc.ControlQueue(ctx, f.scopeID, "conv", "stop", "", "stop-first"); err != nil {
				t.Fatal(err)
			}
			waitStopped := func() {
				t.Helper()
				eventually(t, "preparing execution released", func() bool {
					busy, err := f.svc.executionBusy(ctx, "conv")
					return err == nil && !busy
				})
				queue, err := f.svc.Queue(ctx, f.scopeID, "conv")
				if err != nil || !queue.Paused || queue.Error != "" || len(f.sends(t)) != 0 {
					t.Fatalf("user stop became a failure or resumed: %+v %v, sends=%v", queue, err, f.sends(t))
				}
			}
			if tc.settled {
				release()
				waitStopped()
			}
			next, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("second", "second"), TurnOrigin{})
			if err != nil || next.Queue.Paused || next.Queue.Error != "" {
				t.Fatalf("new send did not resume: %+v %v", next, err)
			}
			if tc.pauseAgain {
				if _, err := f.svc.UpdateQueue(ctx, f.scopeID, "conv", "pause", ""); err != nil {
					t.Fatal(err)
				}
			}
			release()
			texts := []string{"first", "second"}
			if tc.pauseAgain {
				waitStopped()
				replay, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("second", "second"), TurnOrigin{})
				if err != nil || !replay.Queue.Paused || replay.Queue.Error != "" {
					t.Fatalf("duplicate resumed the later pause: %+v %v", replay, err)
				}
				if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("third", "third"), TurnOrigin{}); err != nil {
					t.Fatal(err)
				}
				texts = append(texts, "third")
			}
			for i := range texts {
				eventually(t, "FIFO input", func() bool { return fmt.Sprint(f.sends(t)) == fmt.Sprint(texts[:i+1]) })
				f.allAdapters()[0].emit(t, turnDone)
			}
			eventually(t, "settled queue", func() bool {
				busy, err := f.svc.executionBusy(ctx, "conv")
				return err == nil && !busy
			})
			queue, err := f.svc.Queue(ctx, f.scopeID, "conv")
			if err != nil || queue.Paused || queue.Error != "" || len(queue.Items) != 0 {
				t.Fatalf("queue did not finish: %+v %v", queue, err)
			}
		})
	}
}

func TestProviderCancellationIsStillAPreparationFailure(t *testing.T) {
	f := newServiceFixture(t, filepath.Join(t.TempDir(), "c.sqlite"))
	f.create(t, "conv", "claude")
	f.svc.opts.Claude = func(context.Context, sqlitedb.Conversation) (ClaudeSession, StartClaudeAdapter, error) {
		return ClaudeSession{}, nil, context.Canceled
	}
	ctx := context.Background()
	if _, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("failed", "failed"), TurnOrigin{}); err != nil {
		t.Fatal(err)
	}
	eventually(t, "provider cancellation failure", func() bool {
		queue, err := f.svc.Queue(ctx, f.scopeID, "conv")
		return err == nil && queue.Paused && queue.Error == context.Canceled.Error()
	})
	next, err := f.svc.Submit(ctx, f.scopeID, "conv", queued("next", "next"), TurnOrigin{})
	if err != nil || !next.Queue.Paused || next.Queue.Error != context.Canceled.Error() || next.Submission.State != "queued" {
		t.Fatalf("new send bypassed a real preparation failure: %+v %v", next, err)
	}
}
