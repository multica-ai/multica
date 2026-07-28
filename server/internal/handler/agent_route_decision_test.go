package handler

import "testing"

func TestRouteDecisionForResponseRedactsOwnerCapacityEvidence(t *testing.T) {
	raw := []byte(`{
		"schema_version": 1,
		"capacities": [{
			"provider": "claude",
			"remaining_permille": 700,
			"reserve_permille": 200,
			"reserved_inflight_permille": 50
		}],
		"selected": {
			"candidate_id": "claude-best",
			"provider": "claude",
			"expected_use_permille": 40,
			"projected_remaining_permille": 610,
			"projected_headroom_permille": 410
		},
		"fallbacks": [{
			"candidate_id": "codex-fallback",
			"projected_remaining_permille": 500,
			"projected_headroom_permille": 300
		}]
	}`)

	got, ok := routeDecisionForResponse(raw).(map[string]any)
	if !ok {
		t.Fatal("route decision did not decode to an object")
	}
	if _, exists := got["capacities"]; exists {
		t.Fatal("generic response exposed owner-plan capacities")
	}
	selected, ok := got["selected"].(map[string]any)
	if !ok {
		t.Fatal("selected route evidence was removed")
	}
	for _, key := range []string{"projected_remaining_permille", "projected_headroom_permille"} {
		if _, exists := selected[key]; exists {
			t.Fatalf("selected route exposed %s", key)
		}
	}
	if selected["candidate_id"] != "claude-best" || selected["provider"] != "claude" {
		t.Fatalf("non-capacity route evidence was not preserved: %+v", selected)
	}
	if selected["expected_use_permille"] != float64(40) {
		t.Fatalf("candidate forecast was not preserved: %+v", selected)
	}
	fallbacks, ok := got["fallbacks"].([]any)
	if !ok || len(fallbacks) != 1 {
		t.Fatalf("fallback evidence was not preserved: %+v", got["fallbacks"])
	}
	fallback, ok := fallbacks[0].(map[string]any)
	if !ok {
		t.Fatal("fallback route evidence did not decode to an object")
	}
	if _, exists := fallback["projected_remaining_permille"]; exists {
		t.Fatal("fallback exposed projected remaining capacity")
	}
}

func TestRouteDecisionForResponseRejectsMalformedJSON(t *testing.T) {
	if got := routeDecisionForResponse([]byte(`{"capacities":`)); got != nil {
		t.Fatalf("malformed route decision = %#v, want nil", got)
	}
}
