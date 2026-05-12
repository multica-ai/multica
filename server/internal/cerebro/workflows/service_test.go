package workflows

import (
	"encoding/json"
	"testing"
)

func TestTriggerMatches_StatusChangedNoFilter(t *testing.T) {
	wf := workflow{triggerType: TriggerStatusChanged, triggerConfig: nil}
	te := TriggerEvent{Type: TriggerStatusChanged, FromStatus: "todo", ToStatus: "in_review"}
	if !triggerMatches(wf, te) {
		t.Fatal("workflow with no trigger_config must match any status transition")
	}
}

func TestTriggerMatches_FromToFilter(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigStatusChanged{FromStatus: "todo", ToStatus: "in_review"})
	wf := workflow{triggerType: TriggerStatusChanged, triggerConfig: cfg}

	cases := []struct {
		from, to string
		want     bool
	}{
		{"todo", "in_review", true},
		{"in_progress", "in_review", false}, // wrong from
		{"todo", "done", false},             // wrong to
		{"in_progress", "done", false},
	}
	for _, c := range cases {
		got := triggerMatches(wf, TriggerEvent{Type: TriggerStatusChanged, FromStatus: c.from, ToStatus: c.to})
		if got != c.want {
			t.Errorf("triggerMatches(%s→%s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestTriggerMatches_OnlyToFilter(t *testing.T) {
	cfg, _ := json.Marshal(TriggerConfigStatusChanged{ToStatus: "done"})
	wf := workflow{triggerType: TriggerStatusChanged, triggerConfig: cfg}
	if !triggerMatches(wf, TriggerEvent{Type: TriggerStatusChanged, FromStatus: "todo", ToStatus: "done"}) {
		t.Fatal("workflow with only to_status set should match any → done")
	}
	if triggerMatches(wf, TriggerEvent{Type: TriggerStatusChanged, FromStatus: "todo", ToStatus: "blocked"}) {
		t.Fatal("workflow with to_status=done must not match → blocked")
	}
}

func TestRenderTitle_SubstitutesTitle(t *testing.T) {
	raw := map[string]any{
		"issue": map[string]any{"title": "Login bug", "priority": "high"},
	}
	got := renderTitle("Follow-up: {{title}} ({{priority}})", raw)
	want := "Follow-up: Login bug (high)"
	if got != want {
		t.Errorf("renderTitle = %q, want %q", got, want)
	}
}

func TestRenderTitle_HandlesMissingPayload(t *testing.T) {
	if got := renderTitle("static", nil); got != "static" {
		t.Errorf("renderTitle with nil raw = %q, want %q", got, "static")
	}
	if got := renderTitle("static", map[string]any{}); got != "static" {
		t.Errorf("renderTitle with empty raw = %q, want %q", got, "static")
	}
}

func TestRetryBackoffsMatchSpec(t *testing.T) {
	// Pin the spec-locked backoffs (1 / 5 / 15 min). A future tweak to the
	// schedule must update this test alongside the runtime.
	if len(retryBackoffs) != 3 {
		t.Fatalf("retryBackoffs length = %d, want 3", len(retryBackoffs))
	}
	want := []int{1, 5, 15}
	for i, d := range retryBackoffs {
		if int(d.Minutes()) != want[i] {
			t.Errorf("retryBackoffs[%d] = %v, want %d minutes", i, d, want[i])
		}
	}
	if maxAttempts != len(retryBackoffs)+1 {
		t.Fatalf("maxAttempts must be retryBackoffs+1 to leave room for the initial run")
	}
}

func TestEnvFlagEnabled(t *testing.T) {
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"yes":   true,
		"on":    true,
		"":      false,
		"0":     false,
		"false": false,
		"maybe": false,
	}
	for in, want := range cases {
		t.Setenv("TEST_FLAG", in)
		if got := envFlagEnabled("TEST_FLAG"); got != want {
			t.Errorf("envFlagEnabled(%q) = %v, want %v", in, got, want)
		}
	}
}
