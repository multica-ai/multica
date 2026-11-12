package agent

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type hermesGuidanceWriter func([]byte) (int, error)

func (f hermesGuidanceWriter) Write(data []byte) (int, error) { return f(data) }

func TestHermesGuidanceIncompleteTerminalIsUncertain(t *testing.T) {
	s := &hermesGuidanceSession{enabled: true, identity: hermesGuidanceReceipt{Instance: "i", Session: "s", Turn: "t"}}
	c := &hermesClient{pending: make(map[int]*pendingRPC)}
	s.client = c
	c.stdin = hermesGuidanceWriter(func(data []byte) (int, error) {
		var request struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(data, &request); err != nil {
			t.Fatal(err)
		}
		s.finish(json.RawMessage(`{"_meta":{"ai.nous/active-turn-guidance":{"receipts":[{"runtimeInstanceId":"i","sessionId":"s","turnId":"t","messageId":"m","disposition":"accepted_active","state":"pending_incorporation"}]}}}`))
		response, _ := json.Marshal(map[string]any{"id": request.ID, "result": map[string]string{"runtimeInstanceId": "i", "sessionId": "s", "turnId": "t", "messageId": "m", "disposition": "accepted_active", "state": "pending_incorporation"}})
		c.handleLine(string(response))
		return len(data), nil
	})
	if err := s.sendID(t.Context(), "m", "literal"); !errors.Is(err, ErrSupplementDeliveryUnknown) {
		t.Fatalf("incomplete terminal reported delivered: %v", err)
	}
}

func TestHermesGuidanceNegotiationAndCorrelation(t *testing.T) {
	for _, contract := range []string{`{}`, `{"version":1}`, `{"version":2,"statusLookup":true,"literalText":true,"terminalReceipts":true}`} {
		if hermesHasGuidance(json.RawMessage(`{"agentCapabilities":{"_meta":{"ai.nous/active-turn-guidance":` + contract + `}}}`)) {
			t.Fatal("insufficient capability enabled guidance")
		}
	}
	if !hermesHasGuidance(json.RawMessage(`{"agentCapabilities":{"_meta":{"ai.nous/active-turn-guidance":{"version":1,"statusLookup":true,"literalText":true,"terminalReceipts":true}}}}`)) {
		t.Fatal("complete contract rejected")
	}
	s := &hermesGuidanceSession{enabled: true, promptID: "p", sessionID: "s"}
	s.notification("_ai.nous/guidanceTurn", json.RawMessage(`{"runtimeInstanceId":"i","sessionId":"s","turnId":"wrong","promptId":"other","state":"started"}`))
	if s.ready() {
		t.Fatal("unrelated primary enabled guidance")
	}
	s.notification("_ai.nous/guidanceTurn", json.RawMessage(`{"runtimeInstanceId":"i","sessionId":"s","turnId":"t","promptId":"p","state":"started"}`))
	if !s.ready() {
		t.Fatal("primary identity not retained")
	}
	target := s.identity
	for _, state := range []string{"pending_incorporation", "incorporated", "unconsumed_at_turn_end", "cancelled_before_incorporation", "future_state"} {
		receipt := target
		receipt.Message = "m"
		receipt.Disposition = "accepted_active"
		receipt.State = state
		done, err := s.outcome(receipt, target, "m")
		if (state == "incorporated") != (done && err == nil) {
			t.Fatalf("unexpected outcome %s: %v %v", state, done, err)
		}
		if state == "pending_incorporation" && done {
			t.Fatal("admission mistaken for incorporation")
		}
		receipt.Instance = "restarted"
		if _, err := s.outcome(receipt, target, "m"); !errors.Is(err, ErrSupplementDeliveryUnknown) {
			t.Fatal("uncorrelated receipt accepted")
		}
	}
	s.finish(nil)
	s.notification("_ai.nous/guidanceTurn", json.RawMessage(`{"runtimeInstanceId":"i","sessionId":"s","turnId":"late","promptId":"p","state":"started"}`))
	if s.ready() {
		t.Fatal("late event revived ended turn")
	}
}

// Requires the explicitly supplied local source fixture, never an installed agent.
// It runs the real Go backend and ACP SDK/server with a controlled engine.
func TestHermesGuidanceLocalACPTransport(t *testing.T) {
	for _, scenario := range []string{"normal", "lost_reply", "unconsumed", "final_receipt"} {
		t.Run(scenario, func(t *testing.T) { testHermesGuidanceLocalACPTransport(t, scenario) })
	}
}

func TestHermesGuidanceGatewayACPTransport(t *testing.T) {
	fixture, python := os.Getenv("MULTICA_HERMES_GUIDANCE_GATEWAY_FIXTURE"), os.Getenv("MULTICA_HERMES_GATEWAY_TEST_PYTHON")
	if fixture == "" || python == "" {
		t.Skip("explicit local Hermes gateway source fixture required")
	}
	t.Setenv("MULTICA_HERMES_GUIDANCE_FIXTURE", fixture)
	t.Setenv("MULTICA_HERMES_TEST_PYTHON", python)
	TestHermesGuidanceLocalACPTransport(t)
}

func testHermesGuidanceLocalACPTransport(t *testing.T, scenario string) {
	fixture := os.Getenv("MULTICA_HERMES_GUIDANCE_FIXTURE")
	python := os.Getenv("MULTICA_HERMES_TEST_PYTHON")
	if fixture == "" || python == "" {
		t.Skip("explicit local Hermes source fixture required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
	defer cancel()
	dir := t.TempDir()
	executable := filepath.Join(dir, "controlled-hermes")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }
	writeTestExecutable(t, executable, []byte("#!/bin/sh\nexec "+quote(python)+" "+quote(fixture)+"\n"))
	trace := filepath.Join(dir, "trace.json")
	backend := &hermesBackend{cfg: Config{ExecutablePath: executable, Logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug})), Env: map[string]string{"HERMES_HOME": filepath.Join(dir, "home"), "GUIDANCE_TRACE": trace, "GUIDANCE_CASE": scenario}}}
	session, err := backend.Execute(ctx, "Wait for one instruction", ExecOptions{Cwd: dir, EnableTaskSupplement: true})
	if err != nil {
		t.Fatal(err)
	}
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for range session.Messages {
		}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for !session.SupplementReady() {
		select {
		case <-ticker.C:
		case result := <-session.Result:
			t.Fatalf("primary failed during setup: %+v", result)
		case <-ctx.Done():
			t.Fatal("no correlated turn start")
		}
	}
	select {
	case r := <-session.Result:
		t.Fatalf("primary completed before guidance: %+v", r)
	default:
	}
	const marker = "/reset LITERAL_GUIDANCE_54  "
	err = session.SupplementWithID(ctx, "durable-message-54", marker)
	if scenario == "unconsumed" {
		if err == nil || errors.Is(err, ErrSupplementDeliveryUnknown) {
			t.Fatalf("unconsumed must be a definite non-delivery: %v", err)
		}
		<-session.Result
		<-drained
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-session.Result:
		if result.Status != "completed" || !strings.Contains(result.Output, marker) {
			t.Fatalf("result = %+v", result)
		}
		model, input, output := "controlled", int64(17), int64(5)
		if strings.HasSuffix(fixture, "guidance_gateway_fixture.py") {
			model, input, output = "custom:controlled-gateway", 27, 8
		}
		usage := result.Usage[model]
		if usage.InputTokens != input || usage.OutputTokens != output {
			t.Fatalf("primary usage contaminated: %+v", result.Usage)
		}
	case <-ctx.Done():
		t.Fatal("primary did not finish")
	}
	<-drained
	data, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	var observed struct {
		Kind       string           `json:"kind"`
		OwnerPID   int              `json:"ownerPid"`
		ModelCalls int              `json:"modelCalls"`
		PID        int              `json:"pid"`
		Runs       int              `json:"runs"`
		Steers     int              `json:"steers"`
		Turn       string           `json:"turn"`
		Queued     []string         `json:"queued"`
		Messages   []map[string]any `json:"messages"`
	}
	if err := json.Unmarshal(data, &observed); err != nil {
		t.Fatal(err)
	}
	if observed.PID <= 0 || observed.Runs != 1 || observed.Steers != 1 || observed.Turn == "" || len(observed.Queued) != 0 {
		t.Fatalf("trace = %s", data)
	}
	if strings.HasSuffix(fixture, "guidance_gateway_fixture.py") {
		if observed.Kind != "gateway" || observed.OwnerPID <= 0 || observed.OwnerPID == observed.PID || observed.ModelCalls != 2 || len(observed.Messages) < 4 || observed.Messages[1]["content"] != "Wait for one instruction" || observed.Messages[len(observed.Messages)-1]["content"] != strings.TrimSpace(marker) {
			t.Fatalf("gateway trace = %s", data)
		}
		t.Logf("gateway owner PID %d, ACP viewer PID %d, one admission, turn %s, two model calls, primary usage 27/8", observed.OwnerPID, observed.PID, observed.Turn)
	} else {
		if len(observed.Messages) != 3 || observed.Messages[2]["content"] != marker {
			t.Fatalf("direct trace = %s", data)
		}
		t.Logf("direct PID %d, one engine call, turn %s, primary usage 17/5", observed.PID, observed.Turn)
	}
}

// Review regression: an unrecognized receipt cannot prove non-delivery.
func TestHermesGuidanceUnknownDisposition(t *testing.T) {
	s := &hermesGuidanceSession{}
	target := hermesGuidanceReceipt{Instance: "i", Session: "s", Turn: "t"}
	for _, disposition := range []string{"", "future_disposition"} {
		t.Run("disposition_"+disposition, func(t *testing.T) {
			r := target
			r.Message, r.Disposition, r.State = "m", disposition, "incorporated"
			_, err := s.outcome(r, target, "m")
			if !errors.Is(err, ErrSupplementDeliveryUnknown) {
				t.Fatalf("expected uncertain delivery, got definitive rejection: %v", err)
			}
		})
	}
}

func TestHermesGuidanceAmbiguousReceiptsAcrossChannels(t *testing.T) {
	for _, channel := range []string{"admission", "lookup", "terminal"} {
		for _, disposition := range []string{"", "future_disposition", "rejected_turn_ended"} {
			t.Run(channel+"/"+disposition, func(t *testing.T) {
				s := &hermesGuidanceSession{enabled: true, identity: hermesGuidanceReceipt{Instance: "i", Session: "s", Turn: "t"}}
				c := &hermesClient{pending: make(map[int]*pendingRPC)}
				s.client = c
				steers := 0
				c.stdin = hermesGuidanceWriter(func(data []byte) (int, error) {
					var request struct {
						ID     int    `json:"id"`
						Method string `json:"method"`
					}
					if err := json.Unmarshal(data, &request); err != nil {
						t.Fatal(err)
					}
					receipt := s.identity
					receipt.Message, receipt.Disposition, receipt.State = "m", "accepted_active", "pending_incorporation"
					if request.Method == "_ai.nous/steer" {
						steers++
					}
					if channel == "admission" || request.Method == "_ai.nous/guidanceStatus" {
						receipt.Disposition, receipt.State = disposition, "incorporated"
					}
					if channel == "terminal" {
						terminal := receipt
						terminal.Disposition, terminal.State = disposition, "incorporated"
						result, _ := json.Marshal(map[string]any{"_meta": map[string]any{hermesGuidanceCapability: map[string]any{"receipts": []hermesGuidanceReceipt{terminal}}}})
						s.finish(result)
					}
					response, _ := json.Marshal(map[string]any{"id": request.ID, "result": receipt})
					c.handleLine(string(response))
					return len(data), nil
				})
				if err := s.sendID(t.Context(), "m", "literal"); !errors.Is(err, ErrSupplementDeliveryUnknown) {
					t.Fatalf("ambiguous %s receipt: %v", channel, err)
				}
				if steers != 1 {
					t.Fatalf("steer replayed %d times", steers)
				}
			})
		}
	}
	for _, state := range []string{"", "incorporated", "rejected"} {
		s := &hermesGuidanceSession{}
		r := hermesGuidanceReceipt{Instance: "i", Session: "s", Turn: "t", Message: "m", Disposition: "rejected_turn_ended", State: state}
		_, err := s.outcome(r, r, "m")
		if (state != "rejected") != errors.Is(err, ErrSupplementDeliveryUnknown) {
			t.Fatalf("rejection/state=%q: %v", state, err)
		}
	}
}
