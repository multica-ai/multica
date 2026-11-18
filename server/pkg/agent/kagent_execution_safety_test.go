//go:build !windows

package agent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

func restartSafetyFixture(t *testing.T, f *sliceHTTPFixture) {
	t.Helper()
	var err error
	f.journal, err = kagent.NewJournal(f.journalDir)
	if err != nil {
		t.Fatal(err)
	}
	f.lifecycle, err = kagent.NewLifecycle(f.reconciler, f.journal)
	if err != nil {
		t.Fatal(err)
	}
}

func safetyBackend(f *sliceHTTPFixture, conversation, attempt string) *kagentBackend {
	b := f.backend(attempt).(*kagentBackend)
	b.cfg.ConversationID = conversation
	return b
}

func assertSafetyDenied(t *testing.T, f *sliceHTTPFixture) {
	t.Helper()
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			b := safetyBackend(f, fmt.Sprintf("other-%d", i), "fresh")
			_, err := b.Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"})
			if !errors.Is(err, kagent.ErrUnknownOutcome) && !errors.Is(err, kagent.ErrBusy) {
				t.Errorf("cross-conversation send allowed: %v", err)
			}
		}(i)
	}
	wg.Wait()
}

func TestKagentSafetyUnknownSendSurvivesRestartAcrossConversations(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.failSend = true
	_, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("lost"), context.Background()))
	if known {
		t.Fatal("unknown send settled")
	}
	restartSafetyFixture(t, f)
	assertSafetyDenied(t, f)
	// Clear and the original claim cannot authorize a blind resend either.
	for _, id := range []string{"lost", "clear"} {
		_, err := f.backend(id).Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"})
		if !errors.Is(err, kagent.ErrUnknownOutcome) {
			t.Fatalf("replay allowed: %v", err)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.sends) != 1 {
		t.Fatalf("sends=%d", len(f.sends))
	}
}

// Block atomic rename at a selected commit boundary, without replacing the
// persistence implementation. Restore the last committed record after the run.
func blockSafetyRecord(t *testing.T, f *sliceHTTPFixture, key string) func() {
	t.Helper()
	hash := sha256.Sum256([]byte(key))
	path := filepath.Join(f.journalDir, hex.EncodeToString(hash[:])+".json")
	if err := os.Rename(path, path+".previous"); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(path+".previous", path); err != nil {
			t.Fatal(err)
		}
	}
}

func TestKagentSafetyPersistenceFailuresRetainFenceAndExactRecovery(t *testing.T) {
	for _, stage := range []string{"identity-agent", "identity-conversation", "terminal-conversation", "terminal-agent"} {
		t.Run(stage, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			var restore func()
			f.beforeResponse = func(method string) {
				want := "SendMessage"
				if strings.HasPrefix(stage, "terminal") {
					want = "GetTask"
				}
				if method != want || restore != nil {
					return
				}
				key := kagent.ConversationKey("workspace", sliceAgentID, "conversation")
				if strings.HasSuffix(stage, "agent") {
					key = "agent/" + sliceAgentID
				}
				restore = blockSafetyRecord(t, f, key)
			}
			result, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background()))
			if known || result.Status != "cleanup_unconfirmed" || restore == nil {
				t.Fatalf("blocked commit acknowledged: %+v", result)
			}
			restore()
			f.beforeResponse = nil
			restartSafetyFixture(t, f)
			assertSafetyDenied(t, f)
			if stage == "identity-agent" {
				if _, err := f.backend("attempt").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"}); !errors.Is(err, kagent.ErrUnknownOutcome) {
					t.Fatalf("unrecorded task resubmitted: %v", err)
				}
			} else {
				result, known = finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background()))
				wantStatus := "completed"
				if stage == "terminal-agent" {
					wantStatus = "failed"
				}
				if !known || result.Status != wantStatus {
					t.Fatalf("exact recovery failed: %+v", result)
				}
				// Recovery released the fence, allowing another conversation.
				finishSliceKagent(t, executeSliceKagent(t, safetyBackend(f, "independent", "next"), context.Background()))
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			want := 2
			if stage == "identity-agent" {
				want = 1
			}
			if len(f.sends) != want {
				t.Fatalf("sends=%d want=%d", len(f.sends), want)
			}
		})
	}
}

type safetyTransport struct {
	base     http.RoundTripper
	sentinel string
}

func (s safetyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := s.base.RoundTrip(req)
	if err != nil || !strings.HasPrefix(req.URL.Path, "/agents/") {
		return resp, err
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	result, _ := body["result"].(map[string]any)
	task := result
	if nested, ok := result["task"].(map[string]any); ok {
		task = nested
	}
	if task != nil {
		status, _ := task["status"].(map[string]any)
		status["message"] = map[string]any{"parts": []any{map[string]any{"text": s.sentinel}}}
		task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": s.sentinel}}}}
		task["metadata"] = map[string]any{"providerException": s.sentinel}
	}
	raw, _ := json.Marshal(body)
	resp.Body = io.NopCloser(strings.NewReader(string(raw)))
	return resp, nil
}

func TestKagentSafetyRemoteDiagnosticsNeverReachResultTranscriptOrJournal(t *testing.T) {
	const sentinel = "unrecognized-opaque-value-X9q7Z2p8"
	for _, state := range []string{kagent.StateFailed, kagent.StateRejected, kagent.StateCanceled, kagent.StateInputRequired, kagent.StateAuthRequired, kagent.StateCompleted} {
		t.Run(state, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			f.nextState = state
			if state == kagent.StateInputRequired || state == kagent.StateAuthRequired {
				f.cancelState = kagent.StateCompleted
			}
			config := kagent.HTTPConfig{Endpoint: "https://kagent.test", Client: &http.Client{Transport: safetyTransport{base: kagentHandlerTransport{http.HandlerFunc(f.serve)}, sentinel: sentinel}}, Token: func(context.Context) (string, error) { return "test-caller-token", nil }}
			var err error
			f.client, err = kagent.NewA2AClient(config)
			if err != nil {
				t.Fatal(err)
			}
			s := executeSliceKagent(t, f.backend("attempt"), context.Background())
			var transcript []Message
			for m := range s.Messages {
				transcript = append(transcript, m)
			}
			result := <-s.Result
			raw, _ := json.Marshal(struct {
				Result     Result
				Transcript []Message
			}{result, transcript})
			if state == kagent.StateCompleted {
				if result.Output != sentinel {
					t.Fatal("completed intentional output lost")
				}
			} else if strings.Contains(string(raw), sentinel) || result.Output != "" {
				t.Fatalf("diagnostic leaked: %s", raw)
			}
			if state == kagent.StateInputRequired || state == kagent.StateAuthRequired {
				f.nextState = kagent.StateCompleted
				replay, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("attempt"), context.Background()))
				if !known || replay.Output != "" {
					t.Fatal("unsupported-flow diagnostics exposed on settled replay")
				}
			}
			if strings.Contains(f.logs.String(), sentinel) {
				t.Fatal("remote diagnostic logged")
			}
			entries, err := os.ReadDir(f.journalDir)
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range entries {
				if !strings.HasSuffix(e.Name(), ".json") {
					continue
				}
				data, err := os.ReadFile(filepath.Join(f.journalDir, e.Name()))
				if err != nil {
					t.Fatal(err)
				}
				if state != kagent.StateCompleted && strings.Contains(string(data), sentinel) {
					t.Fatal("remote diagnostic persisted")
				}
			}
		})
	}
}

// The child exits inside the HTTP transport, bypassing every deferred close and
// goroutine cleanup. Only the filesystem journal and OS-released locks survive.
func TestKagentSafetyCrashHelper(t *testing.T) {
	dir := os.Getenv("MULTICA_KAGENT_CRASH_JOURNAL")
	if dir == "" {
		return
	}
	f := newSliceHTTPFixture(t)
	f.journalDir = dir
	restartSafetyFixture(t, f)
	raw, err := os.ReadFile(filepath.Join(filepath.Dir(dir), "remote.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &f.agent); err != nil {
		t.Fatal(err)
	}
	stage := os.Getenv("MULTICA_KAGENT_CRASH_STAGE")
	if strings.HasPrefix(stage, "Unsupported") {
		f.nextState = kagent.StateInputRequired
		stage = strings.TrimPrefix(stage, "Unsupported")
	}
	f.beforeResponse = func(method string) {
		if method != stage {
			return
		}
		raw, err := json.Marshal(f.tasks)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(filepath.Dir(dir), "tasks.json"), raw, 0600); err != nil {
			t.Fatal(err)
		}
		os.Exit(73)
	}
	executeSliceKagent(t, f.backend("crashed"), context.Background())
	select {}
}

func TestKagentSafetyProcessCrashRetainsAgentWideFence(t *testing.T) {
	for _, stage := range []string{"SendMessage", "GetTask", "UnsupportedGetTask", "UnsupportedCancelTask"} {
		t.Run(stage, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			lease, err := f.lifecycle.Acquire(context.Background(), sliceAgentID)
			if err != nil {
				t.Fatal(err)
			}
			lease.Close()
			raw, err := json.Marshal(f.agent)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(filepath.Dir(f.journalDir), "remote.json"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestKagentSafetyCrashHelper$")
			cmd.Env = append(os.Environ(), "MULTICA_KAGENT_CRASH_JOURNAL="+f.journalDir, "MULTICA_KAGENT_CRASH_STAGE="+stage)
			output, err := cmd.CombinedOutput()
			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 73 {
				t.Fatalf("child failed: %v %s", err, output)
			}
			restartSafetyFixture(t, f)
			assertSafetyDenied(t, f)
			lease, err = f.lifecycle.Acquire(context.Background(), sliceAgentID)
			if err != nil {
				t.Fatalf("crash did not release OS lock: %v", err)
			}
			fence := lease.Execution()
			lease.Close()
			if fence == nil || fence.AttemptID != "crashed" || fence.MessageID == "" {
				t.Fatal("crash lost durable intent")
			}
			if stage == "SendMessage" {
				if fence.TaskID != "" {
					t.Fatal("unacknowledged send has task ID")
				}
				if _, err := f.backend("crashed").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"}); !errors.Is(err, kagent.ErrUnknownOutcome) {
					t.Fatalf("crashed send replayed: %v", err)
				}
			} else {
				raw, err := os.ReadFile(filepath.Join(filepath.Dir(f.journalDir), "tasks.json"))
				if err != nil {
					t.Fatal(err)
				}
				if err := json.Unmarshal(raw, &f.tasks); err != nil {
					t.Fatal(err)
				}
				if strings.HasPrefix(stage, "Unsupported") {
					installRereviewTransport(t, f, func(_ string, task map[string]any) {
						task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": "rereview-sentinel"}}}}
					})
				}
				result, known := finishSliceKagent(t, executeSliceKagent(t, f.backend("crashed"), context.Background()))
				if !known || result.Status != "failed" || result.Output != "" || strings.Contains(result.Error, "rereview-sentinel") {
					t.Fatalf("crashed task recovery failed: %+v", result)
				}
			}
			if len(f.sends) != 0 {
				t.Fatal("parent resubmitted crashed task")
			}
		})
	}
}

func TestKagentSafetyOversizedConversationBlocksBeforeDispatch(t *testing.T) {
	f := newSliceHTTPFixture(t)
	lease, err := f.lifecycle.Acquire(context.Background(), sliceAgentID)
	if err != nil {
		t.Fatal(err)
	}
	state := kagentConversation{Version: 2, AgentUID: lease.Desired.ExpectedUID, OwnerToken: lease.Desired.OwnerToken, Endpoint: f.client.Identity(), SessionID: sliceSessionID, Generation: kagent.NewID(), Attempts: map[string]kagentAttempt{}}
	lease.Close()
	old := kagentAttempt{Settled: true, MessageID: kagent.NewID(), InputHash: strings.Repeat("a", 64), Generation: state.Generation, TaskID: "old-task", TerminalState: kagent.StateCompleted}
	state.Attempts["old"] = old
	raw, _ := json.Marshal(state)
	delete(state.Attempts, "old")
	state.Attempts[strings.Repeat("a", (1<<20)-len(raw)-100)] = old
	rec, err := f.journal.Lock(kagent.ConversationKey("workspace", sliceAgentID, "conversation"))
	if err != nil {
		t.Fatal(err)
	}
	if err := rec.Save(state); err != nil {
		t.Fatal(err)
	}
	rec.Close()
	if _, err := f.backend("new").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"}); err == nil {
		t.Fatal("oversized commit accepted")
	}
	restartSafetyFixture(t, f)
	assertSafetyDenied(t, f)
	if len(f.sends) != 0 {
		t.Fatal("send preceded durable conversation commit")
	}
	lease, err = f.lifecycle.Acquire(context.Background(), sliceAgentID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if lease.Execution() == nil || lease.Execution().TaskID != "" {
		t.Fatal("failed pre-dispatch commit lost intent")
	}
}

func TestKagentSafetyLegacyConversationAndEnrollmentEditsPreserveFence(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.failGet = true
	finishSliceKagent(t, executeSliceKagent(t, f.backend("known"), context.Background()))
	lease, err := f.lifecycle.Acquire(context.Background(), sliceAgentID)
	if err != nil {
		t.Fatal(err)
	}
	desired := lease.Desired
	// Reproduce the pre-agent-fence journal: unresolved state only in a chat.
	if err := lease.SaveExecution(nil); err != nil {
		t.Fatal(err)
	}
	lease.Close()
	restartSafetyFixture(t, f)
	assertSafetyDenied(t, f)
	// Only the known, exact old attempt can be adopted and observed.
	f.failGet = false
	f.nextState = kagent.StateWorking
	f.cancelState = kagent.StateWorking
	ctx, cancel := context.WithCancel(context.Background())
	s := executeSliceKagent(t, f.backend("known"), ctx)
	<-s.Messages
	assertSafetyDenied(t, f)
	cancel()
	finishSliceKagent(t, s)
	// Editing enrollment must preserve the adopted unresolved execution intent.
	if err := f.lifecycle.Enroll(desired); err != nil {
		t.Fatal(err)
	}
	restartSafetyFixture(t, f)
	assertSafetyDenied(t, f)
	lease, err = f.lifecycle.Acquire(context.Background(), sliceAgentID)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Close()
	if lease.Execution() == nil || lease.Execution().TaskID == "" {
		t.Fatal("edit erased recovery identity")
	}
}
