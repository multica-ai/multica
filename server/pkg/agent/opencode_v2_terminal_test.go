package agent

import (
	"log/slog"
	"strings"
	"testing"
)

// Stream shapes observed from real OpenCode 2.x runs (GH #8892, #9456, and the
// captures in #8897 / #9437). 2.x stops consuming step events once the session
// settles, so the final step_finish is routinely missing even though the turn
// completed and the CLI exits 0. On 2.x the process outcome is the terminal
// signal; these tests pin the stream half of that contract.
const (
	v2TermStepStart        = `{"type":"step_start","sessionID":"ses_v2","part":{"type":"step-start"}}`
	v2TermText             = `{"type":"text","sessionID":"ses_v2","part":{"type":"text","text":"DONE","time":{"start":1,"end":2}}}`
	v2TermToolUse          = `{"type":"tool_use","sessionID":"ses_v2","part":{"type":"tool","tool":"write","callID":"call_1","state":{"status":"completed","input":{"path":"probe.txt"},"output":"ok"}}}`
	v2TermStepFinishTools  = `{"type":"step_finish","sessionID":"ses_v2","part":{"type":"step-finish","reason":"tool-calls","cost":0,"tokens":{"input":10585,"output":69,"reasoning":0,"cache":{"read":0,"write":0}}}}`
	v2TermStepFinishVoid   = `{"type":"step_finish","sessionID":"ses_v2","part":{"type":"step-finish","reason":"unknown","cost":0,"tokens":{"input":0,"output":0,"reasoning":0,"cache":{"read":0,"write":0}}}}`
	v2TermErrorInvalidOut  = `{"type":"error","sessionID":"ses_v2","error":{"type":"provider.invalid-output","message":"stream ended without finish_reason"}}`
	v2TermErrorTypeOnly    = `{"type":"error","sessionID":"ses_v2","error":{"type":"provider.invalid-output"}}`
	v1TermErrorNameAndData = `{"type":"error","sessionID":"ses_v1","error":{"name":"APIError","data":{"message":"rate limited"}}}`
)

func v2TermConfig() Config {
	return Config{Logger: slog.Default(), BuiltinRuntime: true, CLIVersion: "opencode v2.1.3", Env: map[string]string{opencodeTransportEnv: "run"}}
}

func runV2TermStream(t *testing.T, cfg Config, lines ...string) eventResult {
	t.Helper()
	b := &opencodeBackend{cfg: cfg}
	ch := make(chan Message, 256)
	defer close(ch)
	return b.processEvents(strings.NewReader(strings.Join(lines, "\n")), ch)
}

// TestOpencodeV2MissingFinalStepFinishCompletes covers every real 2.x shape
// that ends without a closing step_finish. None of them may fail on their own.
func TestOpencodeV2MissingFinalStepFinishCompletes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		lines     []string
		wantInput int64
	}{
		{"text-only step left open", []string{v2TermStepStart, v2TermText}, 0},
		{"bare text without step_start", []string{v2TermText}, 0},
		{"tool step closed then text without new step_start",
			[]string{v2TermStepStart, v2TermToolUse, v2TermStepFinishTools, v2TermText}, 10585},
		{"tool step closed then final text step left open",
			[]string{v2TermStepStart, v2TermToolUse, v2TermStepFinishTools, v2TermStepStart, v2TermText}, 10585},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := runV2TermStream(t, v2TermConfig(), tc.lines...)
			if res.status != "completed" {
				t.Fatalf("status = %q (err %q), want completed", res.status, res.errMsg)
			}
			if res.output != "DONE" {
				t.Fatalf("output = %q, want the streamed reply preserved", res.output)
			}
			if res.noTerminalSignal {
				t.Fatal("noTerminalSignal = true, want false: on 2.x the process exit is the terminal signal")
			}
			// Usage from step_finish events that did arrive is still counted, once.
			if res.usage.InputTokens != tc.wantInput {
				t.Fatalf("input tokens = %d, want %d", res.usage.InputTokens, tc.wantInput)
			}
		})
	}
}

// TestOpencodeV2StillFailsClosed pins what the 2.x relaxation must not cover.
func TestOpencodeV2StillFailsClosed(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		lines   []string
		wantErr string
	}{
		{"error event", []string{v2TermStepStart, v2TermText, v2TermErrorInvalidOut},
			"provider.invalid-output: stream ended without finish_reason"},
		{"error event with type only", []string{v2TermStepStart, v2TermErrorTypeOnly},
			"provider.invalid-output"},
		{"closed void step", []string{v2TermStepStart, v2TermStepFinishVoid}, "empty step"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := runV2TermStream(t, v2TermConfig(), tc.lines...)
			if res.status != "failed" {
				t.Fatalf("status = %q, want failed", res.status)
			}
			if !strings.Contains(res.errMsg, tc.wantErr) {
				t.Fatalf("errMsg = %q, want it to contain %q", res.errMsg, tc.wantErr)
			}
			if res.sawTerminalSignal {
				t.Fatal("sawTerminalSignal = true on a failed run")
			}
		})
	}
}

// TestOpencodeV1AndCustomKeepStepBracketing guards that only built-in 2.x
// runtimes get the relaxation: 1.x and custom runtimes still fail the same
// shapes closed (#5233).
func TestOpencodeV1AndCustomKeepStepBracketing(t *testing.T) {
	t.Parallel()

	cfgs := map[string]Config{
		"builtin 1.x":     {Logger: slog.Default(), BuiltinRuntime: true, CLIVersion: "1.18.32"},
		"custom 2.x":      {Logger: slog.Default(), BuiltinRuntime: false, CLIVersion: "opencode v2.1.3"},
		"unknown version": {Logger: slog.Default(), BuiltinRuntime: true},
	}
	shapes := map[string][]string{
		"open step":             {v2TermStepStart, v2TermText},
		"continuation not seen": {v2TermStepStart, v2TermToolUse, v2TermStepFinishTools, v2TermText},
	}
	for cfgName, cfg := range cfgs {
		for shapeName, lines := range shapes {
			cfg, lines := cfg, lines
			t.Run(cfgName+"/"+shapeName, func(t *testing.T) {
				t.Parallel()
				res := runV2TermStream(t, cfg, lines...)
				if res.status != "failed" || !res.noTerminalSignal {
					t.Fatalf("status = %q noTerminalSignal = %v, want failed closed", res.status, res.noTerminalSignal)
				}
				if !strings.Contains(res.errMsg, "terminal signal") {
					t.Fatalf("errMsg = %q, want the terminal-signal diagnosis", res.errMsg)
				}
			})
		}
	}
}

// TestOpencodeErrorMessageShapes pins decoding of both majors' error payloads.
func TestOpencodeErrorMessageShapes(t *testing.T) {
	t.Parallel()

	res := runV2TermStream(t, Config{Logger: slog.Default()}, v1TermErrorNameAndData)
	if res.status != "failed" || res.errMsg != "rate limited" {
		t.Fatalf("1.x error: status = %q errMsg = %q, want failed / rate limited", res.status, res.errMsg)
	}
}

// TestOpencodeV2RecoveredVoidStepCompletes: a void step the run recovers from
// must not fail a 2.x run just because the recovering step never reports its
// step_finish (review on #9463). Mirrors the 1.x
// TestOpencodeProcessEventsEmptyStepMidRunRecovers for the 2.x stream shape.
func TestOpencodeV2RecoveredVoidStepCompletes(t *testing.T) {
	t.Parallel()

	cases := map[string][]string{
		"new step_start then text": {v2TermStepStart, v2TermStepFinishVoid, v2TermStepStart, v2TermText},
		"bare text after void":     {v2TermStepStart, v2TermStepFinishVoid, v2TermText},
		"tool after void":          {v2TermStepStart, v2TermStepFinishVoid, v2TermStepStart, v2TermToolUse},
	}
	for name, lines := range cases {
		lines := lines
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			res := runV2TermStream(t, v2TermConfig(), lines...)
			if res.status != "completed" {
				t.Fatalf("status = %q (err %q), want completed after recovery", res.status, res.errMsg)
			}
		})
	}

	// A void step that is still the last thing the stream saw keeps failing,
	// even after an open step_start with nothing in it.
	res := runV2TermStream(t, v2TermConfig(), v2TermStepStart, v2TermStepFinishVoid, v2TermStepStart)
	if res.status != "failed" || !strings.Contains(res.errMsg, "empty step") {
		t.Fatalf("void then empty open step: status = %q errMsg = %q, want empty-step failure", res.status, res.errMsg)
	}
}

// TestOpencodeV1VoidStepThenBareTextStillFails pins that the recovery rule is
// 2.x-only: on 1.x a recovering step must still close with step_finish.
func TestOpencodeV1VoidStepThenBareTextStillFails(t *testing.T) {
	t.Parallel()

	cfg := Config{Logger: slog.Default(), BuiltinRuntime: true, CLIVersion: "1.18.32"}
	res := runV2TermStream(t, cfg, v2TermStepStart, v2TermStepFinishVoid, v2TermText)
	if res.status != "failed" || !strings.Contains(res.errMsg, "empty step") {
		t.Fatalf("status = %q errMsg = %q, want 1.x empty-step failure unchanged", res.status, res.errMsg)
	}
}

// TestOpencodeV2NeverClaimsStreamLevelCompletion: on 2.x an intermediate
// step_finish is not proof the run finished, so sawTerminalSignal stays false
// and Execute keeps failing a broken prompt write.
func TestOpencodeV2NeverClaimsStreamLevelCompletion(t *testing.T) {
	t.Parallel()

	res := runV2TermStream(t, v2TermConfig(), v2TermStepStart, v2TermToolUse, v2TermStepFinishTools, v2TermText)
	if res.status != "completed" || res.sawTerminalSignal {
		t.Fatalf("status = %q sawTerminalSignal = %v, want completed without stream-level proof", res.status, res.sawTerminalSignal)
	}
}
