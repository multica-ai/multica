package agent

import (
	"context"
	"strings"
)

// Provider-scoped model catalog resolved without touching a CLI (FIR-3287).
//
// ListModels is the discovery path: it shells out, caches, and is meant for the
// daemon answering a UI request. Server-side callers that must decide "can this
// runtime actually run this model?" during an HTTP request or a sweeper tick
// cannot pay that cost, and the daemon's discovery result is never persisted —
// so they need the static half of the catalog on its own.
//
// The list here is intentionally narrower than ListModels' switch. A provider
// belongs in StaticCatalog only when its static list IS the exact set of values
// its `--model` accepts. Deliberately excluded:
//
//   - gemini — geminiStaticModels() is the CLI's alias set plus a few version
//     pins; `gemini -m` also accepts IDs we never baked in.
//   - copilot — account-scoped (Pro vs Pro+ vs Enterprise); its static list is
//     an offline fallback, not the accepted set.
//   - cursor / hermes / kimi / kiro / opencode / pi / openclaw / antigravity /
//     firtal-gateway — catalogs are install- or account-scoped and only
//     knowable via live discovery.
//
// For those, ok=false means "no authoritative list" — never "empty catalog".
// Treating absence of proof as proof of absence would reject a model that
// works, which is a worse failure than the check we skipped.

// StaticCatalog returns the models providerType accepts, without shelling out.
// ok=false means we have no authoritative list for providerType.
func StaticCatalog(providerType string) (models []Model, ok bool) {
	switch providerType {
	case "claude":
		return claudeStaticModels(), true
	case "codex":
		return codexStaticModels(), true
	case openaiEUProvider:
		return openaiEUStaticModels(), true
	case firtalLocalProvider:
		return firtalLocalStaticModels(), true
	default:
		return nil, false
	}
}

// staticCatalogProviders lists every providerType StaticCatalog answers for, in
// the order their models should appear in a merged list.
func staticCatalogProviders() []string {
	return []string{"claude", "codex", openaiEUProvider, firtalLocalProvider}
}

// StaticCatalogSupports reports whether providerType accepts model.
//
// Two cases return true without checking anything, and both are deliberate: an
// empty model means "no override, use the runtime's own default", and a provider
// with no authoritative catalog cannot be proven wrong. Callers use this to
// reject or drop a model, so every uncertain answer has to be permissive.
func StaticCatalogSupports(providerType, model string) bool {
	if model == "" {
		return true
	}
	models, ok := StaticCatalog(providerType)
	if !ok {
		return true
	}
	for _, m := range models {
		if m.ID == model {
			return true
		}
	}
	return false
}

// ModelRunnable reports whether providerType can actually spawn model on this
// machine (FIR-4492). It is the live-discovery counterpart to
// StaticCatalogSupports: where that one has to answer during an HTTP request
// and therefore stays silent about install- or account-scoped providers, this
// one runs on the daemon, where ListModels can ask the CLI itself. That covers
// exactly the providers StaticCatalog excludes — cursor, hermes, kimi, kiro,
// opencode, pi, openclaw, antigravity, firtal-gateway.
//
// It answers false only when discovery succeeded, returned a non-empty catalog,
// and model is absent from it. An empty model, a discovery error and an empty
// catalog all answer true, for the same reason StaticCatalogSupports is
// permissive: absence of proof is not proof of absence, and dropping a model
// the runtime would have accepted is the worse failure.
//
// CEREBRO-PATCH(agent-model-runnable-custom-prefix): Multica pins Hermes/Firtal
// models as `custom:<name>:<model>` while Hermes ACP discovery often advertises
// the same choice as `<name>:<model>`, `custom:<model>`, or bare `<model>`.
// Exact-string mismatch made ModelRunnable report false, runnableTaskModel
// cleared the pin to "", and session/set_model was skipped — resumed Hermes
// tasks then kept a sticky OpenCode base_url with a Firtal model id
// (ModelError: not supported). Match on the underlying model slug too.
func ModelRunnable(ctx context.Context, providerType, executablePath, model string) bool {
	if model == "" {
		return true
	}
	models, err := ListModels(ctx, providerType, executablePath)
	if err != nil || len(models) == 0 {
		return true
	}
	for _, m := range models {
		if modelCatalogIDMatch(m.ID, model) {
			return true
		}
	}
	return false
}

// modelCatalogIDMatch reports whether a discovered catalog model id refers to
// the same underlying model as a Multica agent.model pin.
// CEREBRO-PATCH(agent-model-runnable-custom-prefix): see ModelRunnable.
func modelCatalogIDMatch(catalogID, requested string) bool {
	catalogID = strings.TrimSpace(catalogID)
	requested = strings.TrimSpace(requested)
	if catalogID == "" || requested == "" {
		return false
	}
	if catalogID == requested {
		return true
	}
	return modelSlug(catalogID) == modelSlug(requested)
}

// modelSlug strips Multica/Hermes provider prefixes so catalog and pin forms
// compare equal. `custom:firtal-gateway:deepseek/x` and `deepseek/x` → same.
// CEREBRO-PATCH(agent-model-runnable-custom-prefix).
func modelSlug(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return ""
	}
	if strings.HasPrefix(id, "custom:") {
		rest := strings.TrimPrefix(id, "custom:")
		if i := strings.Index(rest, ":"); i >= 0 {
			return rest[i+1:]
		}
		return rest
	}
	// provider:model (single provider segment). Leave bare ids / org/model paths alone.
	if i := strings.Index(id, ":"); i >= 0 && !strings.Contains(id[:i], "/") {
		return id[i+1:]
	}
	return id
}

// StaticCatalogIDs returns the accepted model IDs for providerType, for building
// an error message that tells the caller what to pick instead. ok mirrors
// StaticCatalog.
func StaticCatalogIDs(providerType string) (ids []string, ok bool) {
	models, ok := StaticCatalog(providerType)
	if !ok {
		return nil, false
	}
	ids = make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	return ids, true
}

// StaticCatalogAllIDs returns every model ID known to any provider in
// StaticCatalog, de-duplicated, in provider order. This is the widest set that
// can be accepted when the target provider is not yet known — an ID outside it
// is one no runtime we can vouch for would run.
func StaticCatalogAllIDs() []string {
	var ids []string
	seen := map[string]bool{}
	for _, provider := range staticCatalogProviders() {
		models, ok := StaticCatalog(provider)
		if !ok {
			continue
		}
		for _, m := range models {
			if seen[m.ID] {
				continue
			}
			seen[m.ID] = true
			ids = append(ids, m.ID)
		}
	}
	return ids
}
