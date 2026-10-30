package agent

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"math"
	"strings"
	"testing"
)

func TestReasonixPermissionPolicy(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		params       string
		wantID       string
		wantGrant    bool
		wantOK       bool
		wantQuestion bool
	}{
		{
			name: "ordinary one-shot approval is allowed",
			params: `{"toolCall":{"toolCallId":"gate-1","_meta":{"reasonix.io":{"tool":"write_file"}}},` +
				`"options":[{"optionId":"allow_once","kind":"allow_once"},{"optionId":"reject_once","kind":"reject_once"}]}`,
			wantID: "allow_once", wantGrant: true, wantOK: true,
		},
		{
			name: "question selects cancel and remains visible",
			params: `{"toolCall":{"toolCallId":"ask-a-q1","title":"Choose a deployment"},` +
				`"options":[{"optionId":"q1:1","kind":"allow_once"},{"optionId":"q1:cancel","kind":"reject_once"}]}`,
			wantID: "q1:cancel", wantOK: true, wantQuestion: true,
		},
		{
			name: "fresh approval is rejected",
			params: `{"toolCall":{"toolCallId":"gate-2","_meta":{"reasonix.io":{"tool":"bash","fresh":true}}},` +
				`"options":[{"optionId":"allow_once","kind":"allow_once"},{"optionId":"reject_once","kind":"reject_once"}]}`,
			wantID: "reject_once", wantOK: true,
		},
		{
			name: "protected approval is rejected even without fresh metadata",
			params: `{"toolCall":{"toolCallId":"gate-3","_meta":{"reasonix.io":{"tool":"config_write"}}},` +
				`"options":[{"optionId":"allow_once","kind":"allow_once"},{"optionId":"reject_once","kind":"reject_once"}]}`,
			wantID: "reject_once", wantOK: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, grant, ok, question := selectReasonixPermissionOption(json.RawMessage(tt.params))
			if id != tt.wantID || grant != tt.wantGrant || ok != tt.wantOK {
				t.Fatalf("decision = (%q, %v, %v), want (%q, %v, %v)", id, grant, ok, tt.wantID, tt.wantGrant, tt.wantOK)
			}
			if (question != "") != tt.wantQuestion {
				t.Fatalf("question = %q, want present=%v", question, tt.wantQuestion)
			}
		})
	}
}

func TestReasonixPermissionMetadataWarningDetection(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		params string
		want   bool
	}{
		{
			name:   "ordinary request without metadata warns",
			params: `{"toolCall":{"toolCallId":"gate-1"},"options":[{"optionId":"allow_once","kind":"allow_once"}]}`,
			want:   true,
		},
		{
			name:   "ordinary request with metadata does not warn",
			params: `{"toolCall":{"toolCallId":"gate-1","_meta":{"reasonix.io":{"tool":"write_file"}}},"options":[]}`,
		},
		{
			name:   "question without metadata does not warn",
			params: `{"toolCall":{"toolCallId":"ask-a-q1"},"options":[{"optionId":"q1:cancel","kind":"reject_once"}]}`,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := reasonixPermissionMetadataMissing(json.RawMessage(tt.params)); got != tt.want {
				t.Fatalf("reasonixPermissionMetadataMissing() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestReasonixCapabilityGapWarning(t *testing.T) {
	t.Parallel()
	compatible := json.RawMessage(`{"agentCapabilities":{"_meta":{"_reasonix.io/session/status":{"schemaVersion":1},"_reasonix.io/session/status_update":{"schemaVersion":1}}}}`)
	statusVersion, updateVersion := reasonixStatusCapabilitySchemas(compatible)
	if statusVersion != 1 || updateVersion != 1 {
		t.Fatalf("capability versions = (%d, %d), want (1, 1)", statusVersion, updateVersion)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	warnReasonixCapabilityGaps(logger, compatible)
	if logs.Len() != 0 {
		t.Fatalf("compatible capabilities logged warning: %s", logs.String())
	}
	warnReasonixCapabilityGaps(logger, json.RawMessage(`{"agentCapabilities":{}}`))
	if !strings.Contains(logs.String(), "usage and cost reporting may be incomplete") {
		t.Fatalf("missing capabilities warning not logged: %s", logs.String())
	}
}

func TestReasonixStatusUsageTracker(t *testing.T) {
	t.Parallel()
	tracker := &reasonixStatusUsageTracker{}
	cost := 0.0123456789
	params := json.RawMessage(`{"schemaVersion":1,"sequence":7,"status":{"model":"deepseek-reasoner","usage":{"turn":{` +
		`"promptTokens":120,"completionTokens":30,"cacheHitTokens":80,"cacheMissTokens":40,` +
		`"estimatedCost":0.0123456789,"currency":"USD"}}}}`)
	tracker.observe("_reasonix.io/session/status_update", params)

	usage, model := tracker.snapshot()
	if model != "deepseek-reasoner" {
		t.Fatalf("model = %q", model)
	}
	if usage.InputTokens != 40 || usage.CacheReadTokens != 80 || usage.OutputTokens != 30 {
		t.Fatalf("usage = %+v", usage)
	}
	wantCost := int64(math.Round(cost * CostUSDTicksPerUSD))
	if usage.CostUSDTicks != wantCost {
		t.Fatalf("cost ticks = %d, want %d", usage.CostUSDTicks, wantCost)
	}

	// Older notifications cannot overwrite the newest turn snapshot.
	tracker.observe("_reasonix.io/session/status_update", json.RawMessage(`{"schemaVersion":1,"sequence":6,"status":{"model":"old","usage":{"turn":{"cacheMissTokens":1}}}}`))
	after, afterModel := tracker.snapshot()
	if after != usage || afterModel != model {
		t.Fatalf("older snapshot overwrote latest: %+v %q", after, afterModel)
	}
}

func TestReasonixResumeAndSetupErrors(t *testing.T) {
	t.Parallel()
	unknown := &acpRPCError{Method: "session/resume", Code: -32602, Message: "session/resume: unknown session dead"}
	if !isACPSessionNotFound(unknown) {
		t.Fatal("Reasonix unknown-session error was not classified")
	}
	lease := &acpRPCError{Method: "session/resume", Code: -32600, Message: "session is in use; close the other Reasonix window"}
	if !isReasonixSessionLeaseConflict(lease) {
		t.Fatal("Reasonix lease conflict was not classified")
	}
	if isReasonixSessionLeaseConflict(errors.New("close the other Reasonix window")) {
		t.Fatal("unstructured error was misclassified as a lease conflict")
	}
	if got := reasonixSessionNewError(errors.New("no default_model configured")); !strings.Contains(got, "reasonix setup") {
		t.Fatalf("setup hint missing from %q", got)
	}
}
