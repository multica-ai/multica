package capabilities

import (
	"encoding/json"
	"testing"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// FIR-3212 slice 2.

func TestExecOptionsForKnownProviderIsStable(t *testing.T) {
	rows, ok := ExecOptionsFor("claude")
	if !ok {
		t.Fatal("claude must be described by the ExecOptions matrix")
	}
	if len(rows) != len(agent.ExecOptionFields()) {
		t.Fatalf("claude returned %d rows, want one per field (%d)", len(rows), len(agent.ExecOptionFields()))
	}

	// claude is the reference row: it honours every field, so anything less
	// means the bridge is dropping information on the way to the wire.
	for _, row := range rows {
		if !row.Effective {
			t.Errorf("claude field %q reported as not effective (handling=%q)", row.Field, row.Handling)
		}
	}

	// The UI iterates these, so the JSON shape is part of the contract.
	blob, err := json.Marshal(rows)
	if err != nil {
		t.Fatalf("rows must marshal: %v", err)
	}
	if len(blob) == 0 || string(blob) == "null" {
		t.Fatal("rows marshalled to null; the UI would crash iterating it")
	}
}

// The whole point of the ok flag: unknown must read as "cannot say".
func TestExecOptionsForUnknownProviderReportsNotOK(t *testing.T) {
	rows, ok := ExecOptionsFor("no-such-runtime")
	if ok {
		t.Error("an unknown provider must report ok=false")
	}
	if rows != nil {
		t.Errorf("an unknown provider must return no rows, got %v", rows)
	}
}

// The operator-facing query: which settings look live but do nothing? codex
// silently drops the tool deny-policy (it has no field for it in the app-server
// protocol), which is exactly the class of thing this must surface.
func TestSilentlyIgnoredBySurfacesTheDenyPolicyGap(t *testing.T) {
	ignored := SilentlyIgnoredBy("codex")
	found := false
	for _, field := range ignored {
		if field == string(agent.FieldDisallowedTools) {
			found = true
		}
	}
	if !found {
		t.Errorf("codex silently drops the tool deny-policy; SilentlyIgnoredBy returned %v", ignored)
	}

	// claude honours everything, so it must report a clean bill.
	if got := SilentlyIgnoredBy("claude"); len(got) != 0 {
		t.Errorf("claude honours every field; want no silent ignores, got %v", got)
	}
}

// The drift alarm KnownProviders() was written for. This asserts the alarm
// FIRES — the gap is real today, and the test documents it rather than hiding it.
func TestUnmappedProvidersReportsTheCurationGap(t *testing.T) {
	unmapped := UnmappedProviders()
	if len(unmapped) == 0 {
		t.Skip("every backend is now curated; the drift alarm has nothing to report")
	}

	// Everything reported must genuinely be a runnable backend with no curated
	// Set — otherwise the alarm is crying wolf.
	for _, provider := range unmapped {
		if !agent.IsSupportedType(provider) {
			t.Errorf("UnmappedProviders reported %q, which is not a runnable backend", provider)
		}
		if set := For(provider); set.DiscoveryMethod != "unmapped" {
			t.Errorf("provider %q reported unmapped but For() says discovery_method=%q",
				provider, set.DiscoveryMethod)
		}
	}

	// And nothing curated may be reported.
	for _, provider := range KnownProviders() {
		for _, u := range unmapped {
			if provider == u {
				t.Errorf("curated provider %q must not be reported as unmapped", provider)
			}
		}
	}
}

func TestCoverageCountsMatchTheBackendFleet(t *testing.T) {
	cov := Coverage()

	if cov.Total != len(agent.ExecOptionsMatrixProviders()) {
		t.Errorf("Coverage.Total = %d, want %d", cov.Total, len(agent.ExecOptionsMatrixProviders()))
	}
	if cov.Curated+len(cov.Unmapped) != cov.Total {
		t.Errorf("Coverage does not add up: curated=%d + unmapped=%d != total=%d",
			cov.Curated, len(cov.Unmapped), cov.Total)
	}
	if cov.Curated <= 0 {
		t.Errorf("Coverage.Curated = %d; the registry curates at least claude", cov.Curated)
	}
}
