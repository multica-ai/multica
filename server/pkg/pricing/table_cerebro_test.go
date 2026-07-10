// CEREBRO (FIR-2698): test fixture + tests for the injected registry table.
// TestMain installs a fixture table mirroring the registry seed so the
// pre-existing ComputeCents expectations keep guarding the same math.
package pricing

import (
	"os"
	"testing"
)

// testTable mirrors the model-registry seed rows the pre-registry hardcoded
// table carried (migration 9120). Fixture data, not a price source.
var testTable = map[string]Pricing{
	"claude-fable-5":    {InputCentsPerMtok: 1000, OutputCentsPerMtok: 5000, CacheReadCentsPerMtok: 100, CacheWriteCentsPerMtok: 1250},
	"claude-opus-4-8":   {InputCentsPerMtok: 500, OutputCentsPerMtok: 2500, CacheReadCentsPerMtok: 50, CacheWriteCentsPerMtok: 625},
	"claude-opus-4-7":   {InputCentsPerMtok: 500, OutputCentsPerMtok: 2500, CacheReadCentsPerMtok: 50, CacheWriteCentsPerMtok: 625},
	"claude-opus-4-1":   {InputCentsPerMtok: 1500, OutputCentsPerMtok: 7500, CacheReadCentsPerMtok: 150, CacheWriteCentsPerMtok: 1875},
	"claude-sonnet-4-6": {InputCentsPerMtok: 300, OutputCentsPerMtok: 1500, CacheReadCentsPerMtok: 30, CacheWriteCentsPerMtok: 375},
	"claude-haiku-4-5":  {InputCentsPerMtok: 100, OutputCentsPerMtok: 500, CacheReadCentsPerMtok: 10, CacheWriteCentsPerMtok: 125},
	"gpt-5.5":           {InputCentsPerMtok: 500, OutputCentsPerMtok: 3000, CacheReadCentsPerMtok: 50, CacheWriteCentsPerMtok: 500},
	"gpt-5":             {InputCentsPerMtok: 125, OutputCentsPerMtok: 1000, CacheReadCentsPerMtok: 12.5, CacheWriteCentsPerMtok: 0},
}

func TestMain(m *testing.M) {
	SetTable(testTable, "claude-opus-4-1", "registry-test")
	os.Exit(m.Run())
}

func TestSetTable_ReplacesAndCopies(t *testing.T) {
	// SetTable must copy the input map so later caller mutations don't leak
	// into the live table.
	src := map[string]Pricing{"m": {InputCentsPerMtok: 100}}
	SetTable(src, "m", "v-copy")
	src["m"] = Pricing{InputCentsPerMtok: 999999}
	if p, ok := tableGet("m"); !ok || p.InputCentsPerMtok != 100 {
		t.Errorf("table should hold the value at SetTable time, got %+v ok=%v", p, ok)
	}
	// Restore the fixture for other tests.
	SetTable(testTable, "claude-opus-4-1", "registry-test")
}

func TestSnapshot_ReflectsInjectedTable(t *testing.T) {
	snap := Snapshot()
	if snap.Version != "registry-test" {
		t.Errorf("snapshot version = %q, want registry-test", snap.Version)
	}
	if snap.FallbackModel != "claude-opus-4-1" {
		t.Errorf("snapshot fallback = %q", snap.FallbackModel)
	}
	if len(snap.Models) != len(testTable) {
		t.Errorf("snapshot has %d models, want %d", len(snap.Models), len(testTable))
	}
	got := snap.Models["claude-opus-4-7"]
	if got.InputCentsPerMtok != 500 || got.OutputCentsPerMtok != 2500 {
		t.Errorf("opus 4-7 rate wrong in snapshot: %+v", got)
	}
	// The returned map must be a copy.
	snap.Models["claude-opus-4-7"] = ModelRate{}
	if p, _ := tableGet("claude-opus-4-7"); p.InputCentsPerMtok != 500 {
		t.Error("mutating the snapshot map leaked into the live table")
	}
}

func TestFallback_BeforeTableLoad(t *testing.T) {
	// With no table loaded, unknown models must price at the static
	// worst-case constant — fail-expensive, never zero.
	SetTable(nil, "", "")
	defer SetTable(testTable, "claude-opus-4-1", "registry-test")

	usage := Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000}
	got := ComputeCents("anything", usage)
	want := int64(failSafePricing.InputCentsPerMtok + failSafePricing.OutputCentsPerMtok)
	if got != want {
		t.Errorf("pre-load fallback = %d cents, want %d (static worst case)", got, want)
	}
	if snap := Snapshot(); snap.Version != failSafeVersion {
		t.Errorf("pre-load snapshot version = %q, want %q", snap.Version, failSafeVersion)
	}
}
