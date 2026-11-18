//go:build !windows

package agent

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/pkg/kagent"
)

type rereviewTransport struct {
	base   http.RoundTripper
	mutate func(string, map[string]any)
}

func (s rereviewTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	var method string
	if strings.HasPrefix(req.URL.Path, "/agents/") {
		raw, _ := io.ReadAll(req.Body)
		req.Body = io.NopCloser(strings.NewReader(string(raw)))
		var v struct{ Method string }
		json.Unmarshal(raw, &v)
		method = v.Method
	}
	resp, err := s.base.RoundTrip(req)
	if err != nil || method == "" {
		return resp, err
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	task, _ := body["result"].(map[string]any)
	if nested, ok := task["task"].(map[string]any); ok {
		task = nested
	}
	if task != nil {
		s.mutate(method, task)
	}
	raw, _ := json.Marshal(body)
	resp.Body = io.NopCloser(strings.NewReader(string(raw)))
	return resp, nil
}

func installRereviewTransport(t *testing.T, f *sliceHTTPFixture, mutate func(string, map[string]any)) {
	t.Helper()
	var err error
	f.client, err = kagent.NewA2AClient(kagent.HTTPConfig{Endpoint: "https://kagent.test", Client: &http.Client{Transport: rereviewTransport{base: kagentHandlerTransport{http.HandlerFunc(f.serve)}, mutate: mutate}}, Token: func(context.Context) (string, error) { return "test-caller-token", nil }})
	if err != nil {
		t.Fatal(err)
	}
}

func assertRereviewNoSentinel(t *testing.T, f *sliceHTTPFixture, s *Session) Result {
	t.Helper()
	var transcript []Message
	for m := range s.Messages {
		transcript = append(transcript, m)
	}
	result := <-s.Result
	raw, _ := json.Marshal(struct {
		Result     Result
		Transcript []Message
	}{result, transcript})
	if strings.Contains(string(raw), "rereview-sentinel") || strings.Contains(f.logs.String(), "rereview-sentinel") {
		t.Fatalf("diagnostic leaked: %s", raw)
	}
	entries, err := os.ReadDir(f.journalDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".json") || e.IsDir() {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(f.journalDir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "rereview-sentinel") {
			t.Fatal("diagnostic persisted")
		}
	}
	return result
}

func TestKagentRereviewUnsupportedIdentityCommitFaults(t *testing.T) {
	for _, response := range []string{"SendMessage", "GetTask"} {
		for _, target := range []string{"agent", "conversation"} {
			t.Run(response+"/"+target, func(t *testing.T) {
				f := newSliceHTTPFixture(t)
				var restore func()
				installRereviewTransport(t, f, func(method string, task map[string]any) {
					if method == response && restore == nil {
						task["status"].(map[string]any)["state"] = kagent.StateAuthRequired
						key := "agent/" + sliceAgentID
						if target == "conversation" {
							key = kagent.ConversationKey("workspace", sliceAgentID, "conversation")
						}
						restore = blockSafetyRecord(t, f, key)
					}
					task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": "rereview-sentinel"}}}}
				})
				result := assertRereviewNoSentinel(t, f, executeSliceKagent(t, f.backend("fault"), context.Background()))
				if result.Status != "cleanup_unconfirmed" || restore == nil {
					t.Fatalf("fault acknowledged: %+v", result)
				}
				restore()
				restartSafetyFixture(t, f)
				// Recovery now sees completed diagnostics. It must retain either the
				// committed unsupported disposition or the interrupted-observation guard.
				installRereviewTransport(t, f, func(_ string, task map[string]any) {
					task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": "rereview-sentinel"}}}}
				})
				if response == "SendMessage" && target == "agent" {
					if _, err := f.backend("fault").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"}); !errors.Is(err, kagent.ErrUnknownOutcome) {
						t.Fatalf("unknown send recovered: %v", err)
					}
					return
				}
				result = assertRereviewNoSentinel(t, f, executeSliceKagent(t, f.backend("fault"), context.Background()))
				if result.Status != "failed" || result.Error == "" {
					t.Fatalf("unsupported recovery succeeded: %+v", result)
				}
				result = assertRereviewNoSentinel(t, f, executeSliceKagent(t, f.backend("fault"), context.Background()))
				if result.Status != "failed" {
					t.Fatal("settled unsupported replay succeeded")
				}
			})
		}
	}
}

func TestKagentRereviewUnsupportedAlwaysFails(t *testing.T) {
	for _, flow := range []string{kagent.StateAuthRequired, kagent.StateInputRequired} {
		for _, final := range []string{kagent.StateCompleted, kagent.StateFailed, kagent.StateRejected, kagent.StateCanceled} {
			t.Run(flow+"/"+final, func(t *testing.T) {
				f := newSliceHTTPFixture(t)
				f.nextState, f.cancelState = flow, final
				installRereviewTransport(t, f, func(_ string, task map[string]any) {
					task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": "rereview-sentinel"}}}}
				})
				result := assertRereviewNoSentinel(t, f, executeSliceKagent(t, f.backend("flow"), context.Background()))
				if result.Status != "failed" || !strings.Contains(result.Error, "unsupported") {
					t.Fatalf("wrong disposition: %+v", result)
				}
			})
		}
	}
}

func TestKagentRereviewCancelUnsupportedThenCompletedFallbackFails(t *testing.T) {
	f := newSliceHTTPFixture(t)
	f.nextState, f.cancelState = kagent.StateWorking, kagent.StateInputRequired
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	canceled := false
	installRereviewTransport(t, f, func(method string, task map[string]any) {
		if method == "CancelTask" {
			canceled = true
		}
		if method == "GetTask" {
			if canceled {
				task["status"].(map[string]any)["state"] = kagent.StateCompleted
			} else {
				cancel()
			}
		}
		task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": "rereview-sentinel"}}}}
	})
	result := assertRereviewNoSentinel(t, f, executeSliceKagent(t, f.backend("fallback"), ctx))
	if result.Status != "failed" || !strings.Contains(result.Error, "unsupported") {
		t.Fatalf("fallback lost unsupported flow: %+v", result)
	}
}

func TestKagentRereviewOutputReplayBound(t *testing.T) {
	f := newSliceHTTPFixture(t)
	installRereviewTransport(t, f, func(_ string, task map[string]any) {
		task["artifacts"] = []any{map[string]any{"parts": []any{map[string]any{"text": strings.Repeat("x", (32<<10)+1)}}}}
	})
	first, _ := finishSliceKagent(t, executeSliceKagent(t, f.backend("large"), context.Background()))
	if first.Status != "failed" || first.Output != "" {
		t.Fatalf("oversized output retained: %+v", first)
	}
	f.failGet = true
	replay, _ := finishSliceKagent(t, executeSliceKagent(t, f.backend("large"), context.Background()))
	if replay.Status != first.Status || replay.Error != first.Error || replay.Output != "" {
		t.Fatalf("bounded disposition changed: %+v", replay)
	}
}

func TestKagentRereviewFinalizedReplayDoesNotContactTaskService(t *testing.T) {
	for _, mode := range []string{"completed", "timeout", "legacy", "pending-clear"} {
		t.Run(mode, func(t *testing.T) {
			f := newSliceHTTPFixture(t)
			b := f.backend("settled")
			ctx := context.Background()
			if mode == "timeout" {
				f.nextState = kagent.StateWorking
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			first, _ := finishSliceKagent(t, executeSliceKagent(t, b, ctx))
			restartSafetyFixture(t, f)
			if mode == "pending-clear" {
				r, err := f.journal.Lock(kagent.ConversationKey("workspace", sliceAgentID, "conversation"))
				if err != nil {
					t.Fatal(err)
				}
				var state kagentConversation
				if _, err := r.Load(&state); err != nil {
					t.Fatal(err)
				}
				r.Close()
				a := state.Attempts["settled"]
				lease, err := f.lifecycle.Acquire(context.Background(), sliceAgentID)
				if err != nil {
					t.Fatal(err)
				}
				if err := lease.SaveExecution(&kagent.ExecutionIntent{
					Version: 1, ConversationKey: kagent.ConversationKey("workspace", sliceAgentID, "conversation"), AttemptID: "settled",
					MessageID: a.MessageID, InputHash: a.InputHash, Generation: a.Generation, TaskID: a.TaskID,
					SessionID: state.SessionID, Endpoint: f.client.Identity(),
				}); err != nil {
					t.Fatal(err)
				}
				lease.Close()
			}
			if mode == "legacy" {
				r, err := f.journal.Lock(kagent.ConversationKey("workspace", sliceAgentID, "conversation"))
				if err != nil {
					t.Fatal(err)
				}
				var s kagentConversation
				if _, err := r.Load(&s); err != nil {
					t.Fatal(err)
				}
				a := s.Attempts["settled"]
				a.Outcome = nil
				s.Attempts["settled"] = a
				if err := r.Save(s); err != nil {
					t.Fatal(err)
				}
				r.Close()
			}
			calls := 0
			var err error
			f.client, err = kagent.NewA2AClient(kagent.HTTPConfig{
				Endpoint: "https://kagent.test",
				Client: &http.Client{Transport: kagentHandlerTransport{http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
					calls++
					w.WriteHeader(http.StatusServiceUnavailable)
				})}},
			})
			if err != nil {
				t.Fatal(err)
			}
			f.failGet = true
			if mode == "legacy" {
				if _, err := f.backend("settled").Execute(context.Background(), "harmless question", ExecOptions{SystemPrompt: "inline runtime brief"}); !errors.Is(err, kagent.ErrUnknownOutcome) {
					t.Fatalf("legacy settled result accepted: %v", err)
				}
			} else {
				replay, _ := finishSliceKagent(t, executeSliceKagent(t, f.backend("settled"), context.Background()))
				if replay.Status != first.Status || replay.Output != first.Output || replay.Error != first.Error || (mode == "timeout" && replay.Status != "timeout") {
					t.Fatalf("replay changed result: first=%+v replay=%+v", first, replay)
				}
			}
			if calls != 0 {
				t.Fatal("remote replay")
			}
			lease, err := f.lifecycle.Acquire(context.Background(), sliceAgentID)
			if err != nil {
				t.Fatal(err)
			}
			if lease.Execution() != nil {
				t.Fatal("settled replay refenced agent")
			}
			lease.Close()
			f.failGet = false
			f.nextState = kagent.StateCompleted
			installRereviewTransport(t, f, func(string, map[string]any) {})
			finishSliceKagent(t, executeSliceKagent(t, safetyBackend(f, "other", "new"), context.Background()))
		})
	}
}
