package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// The window is in no stream event, but the CLI answers `get_usage` with the
// one it is actually using. Verified against Claude Code 2.1.330: the control
// response carries `session.model_usage["claude-opus-5-5"].contextWindow`.
func TestClaudeTakesTheWindowFromTheUsageAnswer(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"a"}],"usage":{"input_tokens":150000}}}`),
		json.RawMessage(`{"type":"control_response","response":{"subtype":"success","request_id":"multica-context-window",` +
			`"response":{"session":{"model_usage":{"claude-opus-5-5":{"contextWindow":1000000}}}}}}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-2","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"b"}],"usage":{"input_tokens":900000}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got := drainSessionContext(t, session)
	if len(got) != 2 {
		t.Fatalf("expected one observation per response, got %+v", got)
	}
	// The first measurement waited for the answer, so it is stated once and
	// against the real window — the derived 200K never reaches a reader.
	if got[0].UsedTokens != 150_000 || got[0].LimitTokens != 1_000_000 {
		t.Fatalf("first observation = %+v, want 150000 of 1000000", got[0])
	}
	// And a 900K prompt that would have disproved 200K now simply fits.
	if got[1].UsedTokens != 900_000 || got[1].LimitTokens != 1_000_000 {
		t.Fatalf("second observation = %+v, want 900000 of 1000000", got[1])
	}
}

func TestClaudeUsageProbeReadsOnlyItsOwnAnswer(t *testing.T) {
	t.Parallel()

	probe := newClaudeUsageProbe()
	answer := func(raw string) int64 {
		return probe.windowFrom(claudeSDKMessage{Response: json.RawMessage(raw)})
	}

	if got := answer(`{"subtype":"success","request_id":"multica-context-window",` +
		`"response":{"session":{"model_usage":{"claude-opus-5-5":{"contextWindow":1000000}}}}}`); got != 1_000_000 {
		t.Fatalf("window = %d, want 1000000", got)
	}
	// Another request's answer — the initializer's handshake, for one — is
	// not ours to read.
	if got := answer(`{"subtype":"success","request_id":"init-1",` +
		`"response":{"session":{"model_usage":{"claude-opus-5-5":{"contextWindow":1000000}}}}}`); got != 0 {
		t.Fatalf("window from another request = %d, want 0", got)
	}
	// A CLI too old for the request says so; that is "no window", not a crash.
	if got := answer(`{"subtype":"error","request_id":"multica-context-window","error":"get_usage is not supported"}`); got != 0 {
		t.Fatalf("window from an error answer = %d, want 0", got)
	}
	// A session with no model record yet answers empty.
	if got := answer(`{"subtype":"success","request_id":"multica-context-window","response":{"session":{"model_usage":{}}}}`); got != 0 {
		t.Fatalf("window from an empty session = %d, want 0", got)
	}
	// Two models in one session: every prompt so far fit in the larger one.
	if got := answer(`{"subtype":"success","request_id":"multica-context-window",` +
		`"response":{"session":{"model_usage":{"claude-haiku-5-5":{"contextWindow":200000},` +
		`"claude-opus-5-5":{"contextWindow":1000000}}}}}`); got != 1_000_000 {
		t.Fatalf("window across models = %d, want the largest", got)
	}
}

func TestClaudeUsageProbeAsksWithBothFlags(t *testing.T) {
	t.Parallel()

	var written strings.Builder
	newClaudeUsageProbe().ask(&written, nil)

	// detached + skip_behaviors are what keep the question out of the turn's
	// way: the answer is produced when it is sent and never queues behind the
	// prompt.
	if strings.TrimSpace(written.String()) != claudeUsageProbeRequest() {
		t.Fatalf("probe wrote %q, want %q", written.String(), claudeUsageProbeRequest())
	}
}

// The answer carries a window only once the session has a per-model usage
// record, and the first response can land before that record exists. One ask
// would leave such a run on a derived window for good, so the probe retries —
// bounded, so a CLI that never answers costs a few frames, not one per
// response.
func TestClaudeUsageProbeRetriesUntilAnswered(t *testing.T) {
	t.Parallel()

	var written strings.Builder
	probe := newClaudeUsageProbe()
	for range claudeUsageProbeMaxAsks + 2 {
		probe.ask(&written, nil)
	}
	if got := strings.Count(written.String(), "control_request"); got != claudeUsageProbeMaxAsks {
		t.Fatalf("probe wrote %d requests, want the %d-ask bound", got, claudeUsageProbeMaxAsks)
	}

	// An empty answer is not an answer: the retries stay available.
	probe = newClaudeUsageProbe()
	written.Reset()
	probe.ask(&written, nil)
	probe.windowFrom(claudeSDKMessage{Response: json.RawMessage(
		`{"subtype":"success","request_id":"multica-context-window","response":{"session":{"model_usage":{}}}}`)})
	probe.ask(&written, nil)
	if got := strings.Count(written.String(), "control_request"); got != 2 {
		t.Fatalf("after an empty answer the probe wrote %d requests, want 2", got)
	}

	// Once a window arrives there is nothing left to ask.
	probe.windowFrom(claudeSDKMessage{Response: json.RawMessage(
		`{"subtype":"success","request_id":"multica-context-window",` +
			`"response":{"session":{"model_usage":{"claude-opus-5-5":{"contextWindow":1000000}}}}}`)})
	written.Reset()
	probe.ask(&written, nil)
	if written.Len() != 0 {
		t.Fatalf("probe asked again after an answer: %q", written.String())
	}
}

// The CLI cannot answer before the response that triggers the question, so a
// run's first measurement has no knowable window yet. A chat turn is one
// response: stating it against the family default would make the whole turn
// read as 200K on a run that is really on 1M. It is held instead,
// and the answer — a fifth of a second later — releases it.
func TestClaudeHoldsTheFirstFigureUntilTheWindowIsKnown(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"a"}],"usage":{"input_tokens":120000}}}`),
		json.RawMessage(`{"type":"control_response","response":{"subtype":"success","request_id":"multica-context-window",` +
			`"response":{"session":{"model_usage":{"claude-opus-5-5":{"contextWindow":1000000}}}}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got := drainSessionContext(t, session)
	if len(got) != 1 {
		t.Fatalf("expected the figure to be stated exactly once, got %+v", got)
	}
	// One response, one observation — against the real window, with no 200K
	// reading in front of it.
	if got[0].UsedTokens != 120_000 || got[0].LimitTokens != 1_000_000 {
		t.Fatalf("observation = %+v, want 120000 of 1000000", got[0])
	}
	if got[0].Model != "claude-opus-5-5" {
		t.Fatalf("model = %q, want the model that measured it", got[0].Model)
	}
}

// A resumed session's first prompt can already exceed the family default. The
// measurement is then dropped for want of a window a prompt that size fits
// in, and the run showed nothing at all — both layers of an issue run.
// The answer makes that measurement reportable.
func TestClaudeReportsADroppedMeasurementOnceTheWindowArrives(t *testing.T) {
	t.Parallel()

	events := []json.RawMessage{
		json.RawMessage(`{"type":"system","subtype":"init","session_id":"s-1","model":"claude-opus-5-5"}`),
		json.RawMessage(`{"type":"assistant","message":{"id":"m-1","role":"assistant","model":"claude-opus-5-5",` +
			`"content":[{"type":"text","text":"a"}],"usage":{"input_tokens":917900}}}`),
		json.RawMessage(`{"type":"control_response","response":{"subtype":"success","request_id":"multica-context-window",` +
			`"response":{"session":{"model_usage":{"claude-opus-5-5":{"contextWindow":1000000}}}}}}`),
	}
	terminal := json.RawMessage(`{"type":"result","subtype":"success","result":"done"}`)
	backend := claudeUsageFixtureBackend(t, events, terminal, false)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	session, err := backend.Execute(ctx, "fixture", ExecOptions{Timeout: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	got := drainSessionContext(t, session)
	if len(got) != 1 {
		t.Fatalf("expected one observation, got %+v", got)
	}
	if got[0].UsedTokens != 917_900 || got[0].LimitTokens != 1_000_000 {
		t.Fatalf("observation = %+v, want 917900 of 1000000", got[0])
	}
}
