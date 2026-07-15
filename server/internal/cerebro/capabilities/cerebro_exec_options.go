package capabilities

import (
	"sort"

	"github.com/multica-ai/multica/server/pkg/agent"
)

// ExecOptions dimension + drift alarm (FIR-3212, slice 2).
//
// discovery.go answers "what tools does this runtime expose?". It says nothing
// about whether a runtime acts on the run settings we send it, so an operator
// could set a tool deny-policy on a codex runtime and watch it do nothing, with
// no way to find out. agent.ExecOptionsHandling knows the answer; this file is
// the bridge that gets it onto the capability surface Multica's permission layer
// already reads.
//
// The registry in discovery.go is hand-curated and covers 6 of the 15 backends
// agent.New registers. Its own KnownProviders() was written for a drift alarm
// that was never built — the function has had no production caller since it
// landed. UnmappedProviders below is that alarm.

// ExecOptionSupport is one row of the matrix, flattened for the wire.
type ExecOptionSupport struct {
	// Field is the ExecOptions field name (model, max_turns, …).
	Field string `json:"field"`
	// Handling is honoured | ignored_logged | ignored_silent.
	Handling string `json:"handling"`
	// Effective is the one-bit answer: does setting this change the run?
	// Serialised alongside Handling so a UI can grey out a control without
	// having to know the vocabulary.
	Effective bool `json:"effective"`
}

// ExecOptionsFor returns the ExecOptions support rows for provider, ordered by
// field name so the JSON is stable.
//
// ok=false means the provider is unknown to the matrix — "we cannot say", never
// "it supports nothing". A caller that greys out controls on ok=false would hide
// settings that may work; see StaticCatalog in cerebro_model_catalog.go for the
// same contract.
func ExecOptionsFor(provider string) (rows []ExecOptionSupport, ok bool) {
	if _, known := agent.ExecOptionsSupportFor(provider); !known {
		return nil, false
	}

	rows = make([]ExecOptionSupport, 0, len(agent.ExecOptionFields()))
	for _, field := range agent.ExecOptionFields() {
		handling, known := agent.ExecOptionsHandling(provider, field)
		if !known {
			continue
		}
		rows = append(rows, ExecOptionSupport{
			Field:     string(field),
			Handling:  string(handling),
			Effective: handling.Effective(),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Field < rows[j].Field })
	return rows, true
}

// SilentlyIgnoredBy lists the settings provider drops without a word.
//
// This is the operator-facing half of the matrix: everything here is a control
// that looks live in the UI and does nothing at runtime. Empty is the goal state.
func SilentlyIgnoredBy(provider string) []string {
	rows, ok := ExecOptionsFor(provider)
	if !ok {
		return nil
	}
	var out []string
	for _, row := range rows {
		if row.Handling == string(agent.HandlingIgnoredSilent) {
			out = append(out, row.Field)
		}
	}
	return out
}

// UnmappedProviders returns every backend agent.New can construct that the
// hand-curated providerRegistry has no entry for — the 9 that fall through to
// staticFallback and publish DiscoveryMethod="unmapped".
//
// This is the drift alarm KnownProviders() was written for. It is a query, not a
// panic: an unmapped provider still runs, it just publishes an empty tool
// surface, so this reports rather than blocks.
func UnmappedProviders() []string {
	curated := map[string]bool{}
	for _, name := range KnownProviders() {
		curated[name] = true
	}

	var out []string
	for _, provider := range agent.ExecOptionsMatrixProviders() {
		if !curated[provider] {
			out = append(out, provider)
		}
	}
	sort.Strings(out)
	return out
}

// ProviderCoverage is a one-glance answer to "how much of our runtime fleet do
// we actually describe?" — the number the plan's slice-2 scope question turns on.
type ProviderCoverage struct {
	// Total is every backend agent.New registers.
	Total int `json:"total"`
	// Curated is how many have a hand-written capability Set.
	Curated int `json:"curated"`
	// Unmapped lists the rest, which publish an empty tool surface.
	Unmapped []string `json:"unmapped"`
}

// Coverage reports how much of the backend fleet the static registry describes.
func Coverage() ProviderCoverage {
	unmapped := UnmappedProviders()
	total := len(agent.ExecOptionsMatrixProviders())
	return ProviderCoverage{
		Total:    total,
		Curated:  total - len(unmapped),
		Unmapped: unmapped,
	}
}
