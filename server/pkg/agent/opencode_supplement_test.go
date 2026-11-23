package agent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"
)

const opencodeSteeringHelperEnv = "MULTICA_OPENCODE_STEERING_HELPER"

func runFakeOpencodeSteeringHelper(mode string) {
	if len(os.Args) > 1 && os.Args[1] == "session" {
		fmt.Println(`{"messages":[]}`)
		return
	}
	var promptID int
	respond := func(id int, result any) {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				SessionID string                        `json:"sessionId"`
				Prompt    []struct{ Type, Text string } `json:"prompt"`
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		switch request.Method {
		case "initialize":
			respond(request.ID, map[string]any{"protocolVersion": 1, "_meta": map[string]any{"steering": map[string]any{"supported": mode != "unsupported"}}})
		case "session/new", "session/load":
			id := request.Params.SessionID
			if id == "" {
				id = "ses_new"
			}
			respond(request.ID, map[string]any{"sessionId": id})
		case "session/prompt":
			promptID = request.ID
			if mode == "unsupported" || mode == "disabled" {
				fmt.Printf("{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":%q,\"update\":{\"sessionUpdate\":\"agent_message_chunk\",\"content\":{\"type\":\"text\",\"text\":\"original answer\"}}}}\n", request.Params.SessionID)
				respond(promptID, map[string]any{"stopReason": "end_turn"})
			}
		case "_session/steering":
			if len(request.Params.Prompt) != 1 || request.Params.Prompt[0].Type != "text" {
				os.Exit(3)
			}
			fmt.Printf("{\"jsonrpc\":\"2.0\",\"method\":\"session/update\",\"params\":{\"sessionId\":%q,\"update\":{\"sessionUpdate\":\"agent_message_chunk\",\"content\":{\"type\":\"text\",\"text\":%q}}}}\n", request.Params.SessionID, request.Params.Prompt[0].Text)
			respond(request.ID, map[string]any{"outcome": "injected"})
			respond(promptID, map[string]any{"stopReason": "end_turn"})
		case "session/cancel":
			respond(promptID, map[string]any{"stopReason": "cancelled"})
		default:
			os.Exit(4)
		}
	}
}

func TestOpencodeSupplementLifecycle(t *testing.T) {
	for _, tc := range []struct {
		name, mode, resume string
		enabled            bool
	}{
		{"new", "supported", "", true},
		{"resume", "supported", "ses_resumed", true},
		{"capability absent", "unsupported", "", true},
		{"not negotiated", "disabled", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			exe, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			backend, err := New("opencode", Config{ExecutablePath: exe, BuiltinRuntime: true, CLIVersion: "2.2.7", Logger: slog.Default(), Env: map[string]string{opencodeSteeringHelperEnv: tc.mode, opencodeTransportEnv: "acp"}})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			session, err := backend.Execute(ctx, "original", ExecOptions{Cwd: t.TempDir(), ResumeSessionID: tc.resume, EnableTaskSupplement: tc.enabled})
			if err != nil {
				t.Fatal(err)
			}
			if !tc.enabled {
				if session.Supplement != nil || session.SupplementReady != nil {
					t.Fatal("unnegotiated hooks exposed")
				}
			} else if tc.mode == "supported" {
				if session.Supplement == nil || session.SupplementReady == nil {
					t.Fatal("missing steering hooks")
				}
				for !session.SupplementReady() {
					select {
					case <-ctx.Done():
						t.Fatal("steering never became ready")
					case <-time.After(time.Millisecond):
					}
				}
				if err := session.Supplement(ctx, "Use the revised requirements."); err != nil {
					t.Fatal(err)
				}
			}
			for range session.Messages {
			}
			result := <-session.Result
			if result.Status != "completed" {
				t.Fatalf("result = %+v", result)
			}
			if tc.mode == "supported" {
				if result.Status != "completed" || result.Output != "Use the revised requirements." {
					t.Fatalf("result = %+v", result)
				}
				wantSession := tc.resume
				if wantSession == "" {
					wantSession = "ses_new"
				}
				if result.SessionID != wantSession {
					t.Fatalf("session = %q, want %q", result.SessionID, wantSession)
				}
			}
			if tc.enabled {
				if session.SupplementReady() {
					t.Fatal("steering ready after turn ended")
				}
				if err := session.Supplement(ctx, "too late"); !errors.Is(err, context.Canceled) {
					t.Fatalf("late steer = %v", err)
				}
			}
		})
	}
}

func TestOpencodeSteeringCapability(t *testing.T) {
	for _, tc := range []struct {
		raw  string
		want bool
	}{
		{`{"_meta":{"steering":{"supported":true}}}`, true},
		{`{"_meta":{"steering":{"supported":false}}}`, false},
		{`{"agentCapabilities":{"_meta":{"steering":{"supported":true}}}}`, false},
		{`{}`, false}, {`null`, false}, {`invalid`, false},
	} {
		if got := opencodeSteeringSupported(json.RawMessage(tc.raw)); got != tc.want {
			t.Errorf("%s: got %v, want %v", tc.raw, got, tc.want)
		}
	}
}

func TestOpencodeSupplementOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name, response string
		rpcErr         error
		wantErr        bool
	}{
		{"injected", `{"outcome":"injected"}`, nil, false},
		{"idle", `{"outcome":"promptRequired","reason":"noRunningTurn"}`, nil, true},
		{"unknown", `{"outcome":"queued"}`, nil, true},
		{"empty", `{}`, nil, true},
		{"invalid", `invalid`, nil, true},
		{"rejected", "", errors.New("method not found"), true},
		{"timeout", "", context.DeadlineExceeded, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := &opencodeSupplementSession{ctx: context.Background(), supported: true, sessionID: "ses_target"}
			s.request = func(ctx context.Context, method string, params any) (json.RawMessage, error) {
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded RPC")
				}
				data, _ := json.Marshal(params)
				if method != "_session/steering" || string(data) != `{"prompt":[{"text":"guidance","type":"text"}],"sessionId":"ses_target"}` {
					t.Fatalf("request = %s %s", method, data)
				}
				return json.RawMessage(tc.response), tc.rpcErr
			}
			s.sent.Store(true)
			err := s.send(context.Background(), "guidance")
			if (err != nil) != tc.wantErr {
				t.Fatalf("send = %v", err)
			}
			if tc.rpcErr != nil && !errors.Is(err, tc.rpcErr) {
				t.Fatalf("lost RPC error: %v", err)
			}
			if tc.name == "idle" && (!errors.Is(err, context.Canceled) || s.ready()) {
				t.Fatalf("idle steer = %v, ready=%v", err, s.ready())
			}
		})
	}
}

func TestOpenCodeSupportsTaskSupplement(t *testing.T) {
	t.Setenv(opencodeTransportEnv, "")
	for _, tc := range []struct {
		name string
		cfg  Config
		opts ExecOptions
		want bool
	}{
		{"ACP", Config{BuiltinRuntime: true, CLIVersion: "2.2.7"}, ExecOptions{}, true},
		{"old", Config{BuiltinRuntime: true, CLIVersion: "2.2.5"}, ExecOptions{}, false},
		{"custom", Config{CLIVersion: "2.2.7"}, ExecOptions{}, false},
		{"prefix", Config{BuiltinRuntime: true, CLIVersion: "2.2.7", LaunchPrefix: []string{"wrapper"}}, ExecOptions{}, false},
		{"args", Config{BuiltinRuntime: true, CLIVersion: "2.2.7"}, ExecOptions{CustomArgs: []string{"--agent", "plan"}}, false},
		{"run", Config{BuiltinRuntime: true, CLIVersion: "2.2.7", Env: map[string]string{opencodeTransportEnv: "run"}}, ExecOptions{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := OpenCodeSupportsTaskSupplement(tc.cfg, tc.opts); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestOpencodeSupplementReadiness(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := &opencodeSupplementSession{ctx: ctx, supported: true, sessionID: "ses"}
	if s.ready() {
		t.Fatal("ready before prompt write")
	}
	s.ended.Store(true)
	s.sent.Store(true)
	if s.ready() {
		t.Fatal("write callback revived ended turn")
	}
	s.ended.Store(false)
	if !s.ready() {
		t.Fatal("active turn not ready")
	}
	s.supported = false
	if s.ready() {
		t.Fatal("ready without advertised capability")
	}
	s.supported = true
	cancel()
	if s.ready() {
		t.Fatal("cancelled turn ready")
	}
	if err := s.send(context.Background(), "late"); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("cancelled send = %v", err)
	}
}

func TestOpencodeSupplementCancellation(t *testing.T) {
	for _, source := range []string{"caller", "run"} {
		t.Run(source, func(t *testing.T) {
			runCtx, cancelRun := context.WithCancel(context.Background())
			defer cancelRun()
			ctx, cancelCaller := context.WithCancel(context.Background())
			defer cancelCaller()
			s := &opencodeSupplementSession{ctx: runCtx, supported: true, sessionID: "ses"}
			s.sent.Store(true)
			s.request = func(ctx context.Context, _ string, _ any) (json.RawMessage, error) {
				if source == "caller" {
					cancelCaller()
				} else {
					cancelRun()
				}
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Second):
					t.Fatal("steering did not observe cancellation")
					return nil, nil
				}
			}
			if err := s.send(ctx, "guidance"); !errors.Is(err, context.Canceled) {
				t.Fatalf("send = %v", err)
			}
		})
	}
}
