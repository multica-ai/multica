package sessions

// Context-window sizes per model — the denominator for "how full is the
// context window". Windows come from the database-backed model registry
// (server/internal/cerebro/modelregistry, FIR-2698), which replaced the
// curated in-code table that previously lived here. The `[1m]` long-context
// tag handling stays local: it is capability detection on the reported model
// string, not registry data.
//
// Source of truth note: the Firtal gateway's /v1/models discovery does NOT
// return a context window today (it sends only id/owned_by/display_name), so
// we cannot read it from the provider yet. Until the runtime reports the live
// window per run, the registry is the authority — editable under Settings
// without a deploy.

import (
	"regexp"

	"github.com/multica-ai/multica/server/internal/cerebro/modelregistry"
)

// defaultContextWindow is the conservative fallback for a model the registry
// has not curated: the standard 200k Claude/OpenAI window. Better to
// under-state "fullness" against a smaller window than to over-promise
// headroom.
const defaultContextWindow int64 = 200_000

// oneMillionMarker matches Anthropic's "[1m]" long-context tag, e.g.
// `claude-opus-4-8[1m]`. The tag declares a 1M-token window regardless of family,
// so it overrides the base family row — this keeps the indicator correct for any
// model whose base row is still a smaller window (older Opus, Haiku) when it runs
// with the long-context variant. The gateway emits the bracketed tag verbatim in
// task_usage, so we match it here.
var oneMillionMarker = regexp.MustCompile(`\[1m\]`)

// capabilityTag strips a trailing bracketed capability tag (e.g. `[1m]`) so a
// bracketed model still resolves to its base family row when the tag is not the
// window-defining `[1m]` handled above.
var capabilityTag = regexp.MustCompile(`\[[^\]]*\]$`)

// contextWindowForModel returns the model's documented context window from
// the registry, or the conservative default when the model is not curated.
// The registry lookup already normalizes casing, provider prefixes, dated
// snapshots and alias spellings; this wrapper adds the `[1m]` long-context
// override and strips other bracketed capability tags first.
func contextWindowForModel(model string) int64 {
	// The [1m] long-context beta tag declares a 1M window regardless of family.
	if oneMillionMarker.MatchString(model) {
		return 1_000_000
	}
	stripped := capabilityTag.ReplaceAllString(model, "")
	if w, ok := modelregistry.ContextWindow(stripped); ok {
		return w
	}
	return defaultContextWindow
}
