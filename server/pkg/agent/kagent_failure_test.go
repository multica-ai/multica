//go:build !windows

package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

func TestKagentLostSendAcknowledgmentBlocksReplay(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.failSend = true
	if _, ok := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background())); ok {
		t.Fatal("lost send acknowledged cleanup")
	}
	for _, id := range []string{"attempt", "another-attempt"} {
		_, err := f.backend(id).Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"})
		if !errors.Is(err, kagent.ErrUnknownOutcome) {
			t.Fatalf("unsafe replay allowed: %v", err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 1 {
		t.Fatal("ambiguous send replayed")
	}
}

func TestKagentKnownTaskRecoversByObservationWithoutResending(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.failGet = true
	if _, ok := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background())); ok {
		t.Fatal("lost poll acknowledged cleanup")
	}
	_, err := f.backend("new-attempt").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"})
	if !errors.Is(err, kagent.ErrUnknownOutcome) {
		t.Fatalf("new work before recovery: %v", err)
	}
	f.mu.Lock()
	f.failGet = false
	f.mu.Unlock()
	result, ok := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background()))
	if !ok || result.Status != "failed" || result.Output != "" {
		t.Fatalf("recovery failed: %+v", result)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 1 {
		t.Fatal("recovery resubmitted input")
	}
}

func TestKagentCancellationRequiresRemoteTerminalAcknowledgment(t *testing.T) {
	for _, state := range []string{kagent.StateCanceled, kagent.StateWorking} {
		t.Run(state, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			f.nextState = kagent.StateWorking
			f.cancelState = state
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := executeSliceKagent(t, f.backend("attempt"), ctx)
			<-s.Messages // remote identity has been received and persisted
			cancel()
			result, ok := finishSliceKagent(t, s)
			if state == kagent.StateCanceled {
				if !ok || result.Status != "cancelled" {
					t.Fatalf("confirmed cancellation missing: %+v", result)
				}
			} else if ok {
				t.Fatal("unconfirmed cancellation acknowledged cleanup")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.cancels != 1 {
				t.Fatalf("cancel requests=%d", f.cancels)
			}
		})
	}
}

func TestKagentActiveLeaseRejectsConcurrentTurnAndDelete(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.nextState = kagent.StateWorking
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := executeSliceKagent(t, f.backend("attempt"), ctx)
	<-s.Messages
	_, err := f.backend("another").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"})
	if !errors.Is(err, kagent.ErrBusy) {
		t.Fatalf("overlapping turn: %v", err)
	}
	if err = f.lifecycle.Delete(context.Background(), sliceAgentID, true); !errors.Is(err, kagent.ErrBusy) {
		t.Fatalf("deleted active agent: %v", err)
	}
	cancel()
	finishSliceKagent(t, s)
}

func TestKagentRejectsUnsupportedCapabilitiesBeforeDispatch(t *testing.T) {
	f := newSliceHTTPFixture(t)
	for _, opts := range []ExecOptions{
		{}, {SystemPrompt: "brief", Cwd: "/local/checkout"}, {SystemPrompt: "brief", TaskToken: "do-not-send"},
		{SystemPrompt: "brief", McpConfig: []byte(`{}`)}, {SystemPrompt: "brief", PiSkillPaths: []string{"/local/skill"}},
		{SystemPrompt: "brief", Model: "override"}, {SystemPrompt: "brief", EnableTaskSupplement: true},
		{SystemPrompt: "brief", ThreadName: "unsupported-title"}, {SystemPrompt: "brief", ClaudeSettingsPath: "/local/settings"},
		{SystemPrompt: "brief", OpenclawMode: "gateway"}, {SystemPrompt: "brief", HandshakeTimeout: time.Second},
		{SystemPrompt: "brief", MulticaWorkspaceID: "other"}, {SystemPrompt: "brief", ResumeSessionID: sliceSessionID},
		{SystemPrompt: "brief", ResumeExpected: true},
	} {
		if _, err := f.backend("attempt").Execute(context.Background(), "question", opts); err == nil {
			t.Fatal("unsupported capability silently accepted")
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 0 {
		t.Fatal("unsupported input dispatched")
	}
}

func TestKagentAttemptInputCannotChange(t *testing.T) {
	f := newSliceHTTPFixture(t)
	finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background()))
	if _, err := f.backend("attempt").Execute(context.Background(), "changed", ExecOptions{SystemPrompt: "inline runtime brief"}); err == nil {
		t.Fatal("attempt identity reused for new input")
	}
}

func TestKagentUnsupportedHumanFlowsCancelBeforeReturningFailure(t *testing.T) {
	for _, state := range []string{kagent.StateInputRequired, kagent.StateAuthRequired} {
		t.Run(state, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			f.nextState = state
			result, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background()))
			if !known || result.Status != "failed" || result.Error == "" {
				t.Fatalf("unsupported flow hidden: %+v", result)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.cancels != 1 {
				t.Fatal("paused remote work was not cancelled")
			}
		})
	}
}
