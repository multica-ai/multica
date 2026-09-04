package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// normalizeCompletionResult owns the /complete selection rules: legacy vs v1,
// strict rejection, and dual-write conflict. It is a pure function, so this is
// the canonical layer for that matrix — the DB-backed handler tests cover
// wiring and persistence, not these cases again.

func TestNormalizeCompletionResultLegacy(t *testing.T) {
	// No `result` key at all: a pre-v1 daemon. Its answer must survive, lifted
	// into the canonical shape.
	req := &TaskCompleteRequest{Output: "legacy answer"}
	got, err := normalizeCompletionResult(req)
	if err != nil {
		t.Fatalf("error = %v, want nil: a legacy payload is valid input", err)
	}
	if got.Summary != "legacy answer" {
		t.Errorf("Summary = %q, want %q", got.Summary, "legacy answer")
	}
	if got.Version != protocol.CompletionResultVersion1 {
		t.Errorf("Version = %d, want normalization to v1", got.Version)
	}
	if got.ArtifactIDs == nil {
		t.Error("ArtifactIDs is nil, want an empty slice")
	}
}

func TestNormalizeCompletionResultLegacyEmptyOutput(t *testing.T) {
	// An empty legacy answer is a legal terminal state (tool-only turn).
	got, err := normalizeCompletionResult(&TaskCompleteRequest{Output: ""})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got.Summary != "" {
		t.Errorf("Summary = %q, want empty", got.Summary)
	}
}

func TestNormalizeCompletionResultV1(t *testing.T) {
	req := &TaskCompleteRequest{
		Result: json.RawMessage(`{"version":1,"summary":"v1 answer","artifact_ids":["a1"]}`),
		Output: "v1 answer",
	}
	got, err := normalizeCompletionResult(req)
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got.Summary != "v1 answer" {
		t.Errorf("Summary = %q, want %q", got.Summary, "v1 answer")
	}
	if len(got.ArtifactIDs) != 1 || got.ArtifactIDs[0] != "a1" {
		t.Errorf("ArtifactIDs = %v, want [a1]", got.ArtifactIDs)
	}
}

func TestNormalizeCompletionResultV1EmptySummaryWithEmptyOutput(t *testing.T) {
	// The tool-only chat turn, dual-written. Both sides empty is agreement,
	// not a conflict — this case would break every such turn if mishandled.
	got, err := normalizeCompletionResult(&TaskCompleteRequest{
		Result: json.RawMessage(`{"version":1,"summary":""}`),
		Output: "",
	})
	if err != nil {
		t.Fatalf("error = %v, want nil: an empty summary is a legal completion", err)
	}
	if got.Summary != "" {
		t.Errorf("Summary = %q, want empty", got.Summary)
	}
}

func TestNormalizeCompletionResultRejectsMalformedV1(t *testing.T) {
	// Each of these declares v1 and breaks it. All must 400 rather than fall
	// back to legacy: persisting a mis-read payload under a v1 label is
	// undetectable downstream.
	tests := []struct {
		name   string
		result string
		output string
	}{
		{"missing summary", `{"version":1}`, "answer"},
		{"summary null", `{"version":1,"summary":null}`, ""},
		{"summary wrong type", `{"version":1,"summary":7}`, ""},
		{"unknown version", `{"version":2,"summary":"a"}`, "a"},
		{"artifact_ids not an array", `{"version":1,"summary":"a","artifact_ids":"x"}`, "a"},
		{"artifact_ids bad element", `{"version":1,"summary":"a","artifact_ids":[5]}`, "a"},
		{"artifact_ids with NUL", "{\"version\":1,\"summary\":\"a\",\"artifact_ids\":[\"x\\u0000y\"]}", "a"},
		{"malformed json", `{"version":1,`, "a"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := normalizeCompletionResult(&TaskCompleteRequest{
				Result: json.RawMessage(tc.result),
				Output: tc.output,
			})
			if err == nil {
				t.Fatal("error = nil, want a rejection")
			}
		})
	}
}

func TestNormalizeCompletionResultRejectsDualWriteConflict(t *testing.T) {
	// Different server versions would otherwise persist different answers for
	// the same run: one reads `result`, an older one reads `output`.
	_, err := normalizeCompletionResult(&TaskCompleteRequest{
		Result: json.RawMessage(`{"version":1,"summary":"from v1"}`),
		Output: "from legacy",
	})
	if err == nil {
		t.Fatal("error = nil, want rejection of a divergent dual write")
	}
	if !strings.Contains(err.Error(), "disagree") {
		t.Errorf("error = %v, want it to name the disagreement", err)
	}
}

func TestNormalizeCompletionResultAllowsAbsentOutputWithV1(t *testing.T) {
	// A future daemon may stop dual-writing once no old servers remain. An
	// absent output is not a conflict.
	got, err := normalizeCompletionResult(&TaskCompleteRequest{
		Result: json.RawMessage(`{"version":1,"summary":"only v1"}`),
	})
	if err != nil {
		t.Fatalf("error = %v, want nil", err)
	}
	if got.Summary != "only v1" {
		t.Errorf("Summary = %q, want %q", got.Summary, "only v1")
	}
}

func TestNormalizeCompletionResultSanitizesSummary(t *testing.T) {
	// A NUL in prose must be stripped, not rejected: it would abort the JSONB
	// insert (GH #7098) and strand the task in 'running'. The comparison
	// against the already-sanitized Output must still see them as equal.
	req := &TaskCompleteRequest{
		Result: json.RawMessage("{\"version\":1,\"summary\":\"done\\u0000 text\"}"),
		Output: "done text", // as sanitizeTaskCompleteRequest would have left it
	}
	got, err := normalizeCompletionResult(req)
	if err != nil {
		t.Fatalf("error = %v, want nil: a sanitized NUL is not a real disagreement", err)
	}
	if strings.ContainsRune(got.Summary, 0) {
		t.Error("summary still contains a NUL; JSONB will reject the insert")
	}
	if got.Summary != "done text" {
		t.Errorf("Summary = %q, want %q", got.Summary, "done text")
	}
}
