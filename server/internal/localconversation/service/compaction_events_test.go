package service

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAutomaticCompactionIsStoredWithoutSettlingTheTurn(t *testing.T) {
	t.Parallel()
	for _, provider := range []string{"codex", "claude"} {
		t.Run(provider, func(t *testing.T) {
			var f *runnerFixture
			var finish func()
			if provider == "codex" {
				var r *CodexRunner
				var server *fakeCodex
				f, r, server = newCodexFixture(t, CodexSession{}, nil)
				t.Cleanup(func() { _ = r.Close(context.Background()) })
				codexSend(t, r, "t1")
				server.notify(t, "item/started", `{"threadId":"th-1","turnId":"tu-1","item":{"id":"compaction","type":"contextCompaction"}}`)
				server.notify(t, "item/completed", `{"threadId":"th-1","turnId":"tu-1","item":{"id":"compaction","type":"contextCompaction"}}`)
				finish = func() {
					server.notify(t, "turn/completed", `{"threadId":"th-1","turn":{"id":"tu-1","status":"completed"}}`)
				}
			} else {
				f = newRunnerFixture(t, NewLimiter(MaxRunningTurns), nil)
				t.Cleanup(func() { _ = f.runner.Close(context.Background()) })
				send(t, f, "t1")
				f.adapter(0).emit(t,
					`{"type":"system","subtype":"status","status":"compacting","uuid":"start-1"}`,
					`{"type":"system","subtype":"compact_boundary","uuid":"boundary-1"}`)
				finish = func() { f.adapter(0).emit(t, turnDone) }
			}
			eventually(t, "compaction items to be committed", func() bool {
				events, _ := f.store.TurnEvents(context.Background(), "conv-1", "t1")
				count := 0
				for _, e := range events {
					var payload struct{ Kind string }
					_ = json.Unmarshal([]byte(e.Payload), &payload)
					if payload.Kind == "compaction" {
						count++
					}
				}
				return count == 2
			})
			turn, err := f.store.Turn(context.Background(), "conv-1", "t1")
			if err != nil || turn.Status != "running" {
				t.Fatalf("compaction settled its turn: %+v, %v", turn, err)
			}
			finish()
			f.turnEvents(t, "t1")
		})
	}
}
