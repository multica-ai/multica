package evals

import (
	"encoding/json"
	"testing"
)

func TestParseTarget(t *testing.T) {
	spec, err := ParseTarget(json.RawMessage(`{"kind":"PROMPT","ref":" agent-x ","system":" be terse ","model":"claude"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if spec.Kind != "prompt" {
		t.Fatalf("kind not lowercased: %q", spec.Kind)
	}
	if spec.Ref != "agent-x" || spec.System != "be terse" || spec.Model != "claude" {
		t.Fatalf("fields not trimmed: %+v", spec)
	}
}

func TestParseTargetRejectsMissing(t *testing.T) {
	if _, err := ParseTarget(nil); err == nil {
		t.Fatal("expected error for empty target")
	}
	if _, err := ParseTarget(json.RawMessage(`{"ref":"x"}`)); err == nil {
		t.Fatal("expected error for missing kind")
	}
	if _, err := ParseTarget(json.RawMessage(`not json`)); err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}
