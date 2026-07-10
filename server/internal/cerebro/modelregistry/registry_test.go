package modelregistry

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/multica-ai/multica/server/pkg/pricing"
)

func testSnapshot() Snapshot {
	return Snapshot{
		FallbackModel: "claude-opus-4-1",
		Models: map[string]ModelEntry{
			"claude-opus-4-1": {Label: "Claude Opus 4.1", Provider: "anthropic", ContextWindow: 200_000, InputUSDPerMtok: 15, OutputUSDPerMtok: 75, CacheReadUSDPerMtok: 1.5, CacheWriteUSDPerMtok: 18.75},
			"claude-sonnet-5": {Label: "Claude Sonnet 5", Provider: "anthropic", ContextWindow: 1_000_000, InputUSDPerMtok: 3, OutputUSDPerMtok: 15, CacheReadUSDPerMtok: 0.3, CacheWriteUSDPerMtok: 3.75},
			"gpt-5.5":         {Label: "GPT-5.5", Provider: "openai", InputUSDPerMtok: 5, OutputUSDPerMtok: 30, CacheReadUSDPerMtok: 0.5, CacheWriteUSDPerMtok: 5},
			"gemini-2.5-pro":  {Label: "Gemini 2.5 Pro", Provider: "google", ContextWindow: 1_000_000, InputUSDPerMtok: 1.25, OutputUSDPerMtok: 10, CacheReadUSDPerMtok: 0.3125},
		},
	}
}

func TestEncodeDecodeSnapshot_RoundTrip(t *testing.T) {
	snap := testSnapshot()
	got := DecodeSnapshot(EncodeSnapshot(snap))
	if got.FallbackModel != snap.FallbackModel || len(got.Models) != len(snap.Models) {
		t.Fatalf("round trip lost data: %+v", got)
	}
	if got.Models["claude-sonnet-5"] != snap.Models["claude-sonnet-5"] {
		t.Errorf("entry drift after round trip: %+v", got.Models["claude-sonnet-5"])
	}
	// Nil-safety.
	if s := DecodeSnapshot(nil); s.Models == nil {
		t.Error("DecodeSnapshot(nil) must return a non-nil Models map")
	}
}

func TestValidateSnapshot(t *testing.T) {
	if err := ValidateSnapshot(testSnapshot()); err != nil {
		t.Fatalf("valid snapshot rejected: %v", err)
	}
	bad := testSnapshot()
	bad.FallbackModel = "not-in-table"
	if err := ValidateSnapshot(bad); err == nil {
		t.Error("fallback not in table must be rejected")
	}
	bad = testSnapshot()
	bad.FallbackModel = ""
	if err := ValidateSnapshot(bad); err == nil {
		t.Error("empty fallback must be rejected")
	}
	bad = testSnapshot()
	e := bad.Models["gpt-5.5"]
	e.InputUSDPerMtok = -1
	bad.Models["gpt-5.5"] = e
	if err := ValidateSnapshot(bad); err == nil {
		t.Error("negative price must be rejected")
	}
	bad = testSnapshot()
	bad.Models["Claude-Upper"] = ModelEntry{}
	if err := ValidateSnapshot(bad); err == nil {
		t.Error("uppercase model id must be rejected")
	}
	if err := ValidateSnapshot(Snapshot{FallbackModel: "x"}); err == nil {
		t.Error("empty model table must be rejected")
	}
}

func TestRenderAndDiffSnapshots(t *testing.T) {
	base := testSnapshot()
	render := RenderSnapshot(base)
	if !strings.HasPrefix(render, "fallback_model: claude-opus-4-1\n") {
		t.Errorf("render must start with fallback line: %q", render[:60])
	}
	// Deterministic: same snapshot renders identically (sorted model order).
	if RenderSnapshot(base) != render {
		t.Error("render is not deterministic")
	}
	if d := DiffSnapshots(base, base); d != "" {
		t.Errorf("identical snapshots must produce empty diff, got %q", d)
	}
	changed := testSnapshot()
	e := changed.Models["claude-sonnet-5"]
	e.InputUSDPerMtok = 4
	changed.Models["claude-sonnet-5"] = e
	d := DiffSnapshots(base, changed)
	if !strings.Contains(d, "-claude-sonnet-5:") || !strings.Contains(d, "+claude-sonnet-5:") {
		t.Errorf("diff must show the changed model line: %q", d)
	}
}

func TestPublishAndLookup(t *testing.T) {
	Publish(testSnapshot(), "9.9.9")
	defer Publish(Snapshot{}, "") // reset for other tests

	cases := []struct {
		in   string
		want string
	}{
		{"claude-sonnet-5", "claude-sonnet-5"},
		{"  Claude-Sonnet-5  ", "claude-sonnet-5"},                // case + whitespace
		{"anthropic/claude-sonnet-5", "claude-sonnet-5"},          // provider/ prefix
		{"anthropic:claude-sonnet-5", "claude-sonnet-5"},          // provider: prefix
		{"claude-sonnet-5-20260101", "claude-sonnet-5"},           // dated snapshot
		{"claude-sonnet-5-latest", "claude-sonnet-5"},             // -latest tag
		{"gpt-5-5", "gpt-5.5"},                                    // hyphen variant via alias rule
		{"openrouter/gpt-5.5", "gpt-5.5"},                         // prefixed alias
		{"gemini-2.5-pro-preview-06-05", "gemini-2.5-pro"},        // alias regex contains-match
		{"anthropic/claude-sonnet-5-20260101", "claude-sonnet-5"}, // prefix + date
	}
	for _, c := range cases {
		id, _, ok := Lookup(c.in)
		if !ok || id != c.want {
			t.Errorf("Lookup(%q) = (%q, ok=%v), want %q", c.in, id, ok, c.want)
		}
	}
	if _, _, ok := Lookup("model-nobody-has-heard-of"); ok {
		t.Error("unknown model must not resolve")
	}
	if _, _, ok := Lookup(""); ok {
		t.Error("empty model must not resolve")
	}

	// Context windows: curated vs uncurated (0) vs unknown.
	if w, ok := ContextWindow("claude-sonnet-5"); !ok || w != 1_000_000 {
		t.Errorf("ContextWindow(sonnet-5) = %d ok=%v", w, ok)
	}
	if _, ok := ContextWindow("gpt-5.5"); ok {
		t.Error("uncurated window (0) must report ok=false")
	}
	if _, ok := ContextWindow("unknown"); ok {
		t.Error("unknown model must report ok=false")
	}

	// Version plumbed through.
	if _, v, ok := Current(); !ok || v != "9.9.9" {
		t.Errorf("Current() version = %q ok=%v", v, ok)
	}
}

func TestPublish_FeedsPricingTable(t *testing.T) {
	Publish(testSnapshot(), "2.0.0")
	defer Publish(Snapshot{}, "")

	// USD/Mtok × 100 = cents/Mtok: sonnet-5 at $3/$15 → 1M in + 1M out =
	// 300 + 1500 = 1800 cents.
	got := pricing.ComputeCents("claude-sonnet-5", pricing.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	if got != 1800 {
		t.Errorf("ComputeCents via published table = %d, want 1800", got)
	}
	// Unknown model must use the registry fallback (Opus 4.1 worst case).
	got = pricing.ComputeCents("mystery", pricing.Usage{InputTokens: 1_000_000, OutputTokens: 1_000_000})
	if got != 9000 {
		t.Errorf("fallback ComputeCents = %d, want 9000", got)
	}
	// Snapshot contract: version is stamped registry-<semver>.
	if snap := pricing.Snapshot(); snap.Version != "registry-2.0.0" || snap.FallbackModel != "claude-opus-4-1" {
		t.Errorf("pricing snapshot = version %q fallback %q", snap.Version, snap.FallbackModel)
	}
}

// TestSeedMigration_ParsesAndCoversLegacyTables guards the 9120 seed: the
// JSON literal must parse, validate, and contain every model the four
// replaced in-code tables priced or curated — at the exact values production
// billed with (spot-checked on the rows that drove FIR-2689 and FIR-2661).
func TestSeedMigration_ParsesAndCoversLegacyTables(t *testing.T) {
	raw, err := os.ReadFile("../../../migrations/9120_cerebro_model_registry.up.sql")
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := string(raw)
	const openAnchor = "'1.0.0', '{"
	start := strings.Index(sql, openAnchor)
	end := strings.Index(sql, "}'::jsonb")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("seed JSON literal not found in migration")
	}
	jsonStart := start + len(openAnchor) - 1 // position of the opening brace
	var snap Snapshot
	if err := json.Unmarshal([]byte(sql[jsonStart:end+1]), &snap); err != nil {
		t.Fatalf("seed JSON does not parse: %v", err)
	}
	if err := ValidateSnapshot(snap); err != nil {
		t.Fatalf("seed snapshot invalid: %v", err)
	}

	// Every model the replaced tables covered must be present.
	legacy := []string{
		// pkg/pricing (cost_cents source of truth)
		"claude-fable-5", "claude-opus-4-8", "claude-opus-4-7", "claude-opus-4-6",
		"claude-opus-4-5", "claude-opus-4-1", "claude-opus-4", "claude-sonnet-5",
		"claude-sonnet-4-6", "claude-sonnet-4-5", "claude-sonnet-4",
		"claude-haiku-4-5", "claude-haiku-3-5", "gpt-5.5", "gpt-5", "gpt-5-mini",
		"gemini-2.5-pro", "gemini-2.5-flash",
		// internal/metrics extras
		"gpt-5.4", "gpt-5.4-mini", "gpt-5.3-codex", "gpt-5.2-codex",
		"deepseek-v4-pro", "deepseek-v4-flash", "minimax-m2.7",
		"minimax-m2.7-highspeed", "gemini-3-flash", "gemini-3.1-pro",
		// frontend MODEL_PRICING extras
		"gpt-5-codex", "gpt-5-nano", "o3", "o3-mini", "o4-mini", "gpt-4o",
		"gpt-4o-mini", "deepseek-chat", "deepseek-reasoner", "kimi-k2.6",
		"glm-5.1", "glm-5", "glm-5-turbo", "glm-4.7", "glm-4.7-flashx",
		"glm-4.7-flash", "glm-4.6", "glm-4.5", "glm-4.5-x", "glm-4.5-air",
		"glm-4.5-airx", "glm-4.5-flash",
	}
	for _, id := range legacy {
		if _, ok := snap.Models[id]; !ok {
			t.Errorf("seed is missing legacy model %q", id)
		}
	}

	// Billing parity spot checks (values production billed with pre-registry).
	checks := map[string]ModelEntry{
		"claude-sonnet-5": {Label: "Claude Sonnet 5", Provider: "anthropic", ContextWindow: 1_000_000, InputUSDPerMtok: 3, OutputUSDPerMtok: 15, CacheReadUSDPerMtok: 0.3, CacheWriteUSDPerMtok: 3.75},
		"claude-fable-5":  {Label: "Claude Fable 5", Provider: "anthropic", ContextWindow: 1_000_000, InputUSDPerMtok: 10, OutputUSDPerMtok: 50, CacheReadUSDPerMtok: 1, CacheWriteUSDPerMtok: 12.5},
		"claude-opus-4-1": {Label: "Claude Opus 4.1", Provider: "anthropic", ContextWindow: 200_000, InputUSDPerMtok: 15, OutputUSDPerMtok: 75, CacheReadUSDPerMtok: 1.5, CacheWriteUSDPerMtok: 18.75},
		"gpt-5.5":         {Label: "GPT-5.5", Provider: "openai", ContextWindow: 272_000, InputUSDPerMtok: 5, OutputUSDPerMtok: 30, CacheReadUSDPerMtok: 0.5, CacheWriteUSDPerMtok: 5},
	}
	for id, want := range checks {
		got, ok := snap.Models[id]
		if !ok {
			t.Errorf("seed missing %q", id)
			continue
		}
		if got != want {
			t.Errorf("seed %q = %+v, want %+v", id, got, want)
		}
	}
	if snap.FallbackModel != "claude-opus-4-1" {
		t.Errorf("seed fallback = %q, want claude-opus-4-1", snap.FallbackModel)
	}
}
