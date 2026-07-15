// Package autopilotmodel — per-autopilot and per-task model override (JEH-1310).
//
// In upstream the model an agent runs on is resolved at agent level only
// (server/internal/daemon/daemon.go: agent.Model → MULTICA_<PROVIDER>_MODEL
// env → CLI default). Cerebro adds a third tier above agent.Model so that
// individual autopilots can ship cheap status pings on Haiku without
// duplicating the agent itself, and so that a future per-mention picker can
// override a single run.
//
// The registry is the *valid choice list* the API accepts and the UI renders.
// It is not maintained here: it is read from agent.StaticCatalog, the same
// per-provider catalog the daemon offers in the model picker. A hand-copied
// list drifts from what the runtimes actually accept, and every model in it is
// a model some run will fail on (FIR-3287).
//
// Model IDs are provider-scoped, not global — an Anthropic ID is meaningless to
// a Codex runtime, which rejects it outright. Prefer ValidateForProvider
// wherever the target agent's runtime is known; plain Validate only answers the
// weaker "is this ID known to any provider at all?".
package autopilotmodel

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Well-known IDs kept as named constants for callers that want a specific tier
// (and for tests). They are shorthand for entries in agent.StaticCatalog, not a
// second source of truth — CatalogCoversNamedModels guards that they resolve.
const (
	ModelHaiku  = "claude-haiku-4-5-20251001"
	ModelSonnet = "claude-sonnet-4-6"
	ModelOpus   = "claude-opus-4-7"
)

// Allowed returns every model ID any catalogued provider accepts, in provider
// order. This is the widest possible answer and the only one available when the
// target runtime is unknown; AllowedForProvider is the one to show a user who
// is picking a model for a specific agent.
func Allowed() []string {
	return agent.StaticCatalogAllIDs()
}

// AllowedForProvider returns the model IDs the given runtime provider accepts.
// ok=false means the provider has no authoritative catalog (its models are only
// knowable by asking the live runtime), so there is nothing to offer or enforce.
func AllowedForProvider(provider string) (ids []string, ok bool) {
	return agent.StaticCatalogIDs(provider)
}

// ErrUnknownModel is returned when a caller supplies a model ID no catalogued
// provider accepts. The empty string is NOT an error — it means "use the agent
// default", same semantics as autopilot.model = NULL.
var ErrUnknownModel = errors.New("model not in registry")

// ErrModelNotOnProvider is returned when a model ID is real but belongs to a
// different provider than the one that would run it — e.g. an Anthropic model
// pinned on a Codex runtime, which fails the run with a 400 from the CLI
// ("model is not supported when using Codex with a ChatGPT account"). This is a
// distinct error from ErrUnknownModel because the fix is different: the caller
// picked a valid model for the wrong runtime, not a typo.
var ErrModelNotOnProvider = errors.New("model not available on this runtime's provider")

// Validate checks model against the union of every provider's catalog. An empty
// string passes (clears the override). Prefer ValidateForProvider when the
// target runtime is known — passing this is necessary but not sufficient, since
// no single runtime accepts the whole union.
func Validate(model string) error {
	if model == "" {
		return nil
	}
	for _, m := range Allowed() {
		if m == model {
			return nil
		}
	}
	return fmt.Errorf("%w: %q (allowed: %v)", ErrUnknownModel, model, Allowed())
}

// ValidateForProvider checks model against the catalog of the runtime provider
// that would actually run it. An empty model passes (no override).
//
// A provider with no authoritative catalog passes anything. It deliberately does
// NOT fall back to the union: providers like hermes and opencode use IDs of a
// different shape entirely (`openrouter/some-model`), so checking them against
// our catalog would reject models that work. Nothing can be concluded about
// them here, and a false rejection is worse than a skipped check.
func ValidateForProvider(provider, model string) error {
	if model == "" {
		return nil
	}
	allowed, ok := AllowedForProvider(provider)
	if !ok {
		return nil
	}
	if SupportedByProvider(provider, model) {
		return nil
	}
	return fmt.Errorf("%w: %q is not a %s model (allowed: %v)", ErrModelNotOnProvider, model, provider, allowed)
}

// SupportedByProvider reports whether provider can run model. An empty model and
// a provider with no authoritative catalog both report true — see
// agent.StaticCatalogSupports for why an uncertain answer must be permissive.
//
// This is the boolean form of ValidateForProvider, for callers that want to fall
// back to a safe default rather than surface an error.
func SupportedByProvider(provider, model string) bool {
	return agent.StaticCatalogSupports(provider, model)
}

// SetOnAutopilot persists the model column on an existing autopilot row.
// Empty model clears the override. Kept as a separate UPDATE so the upstream
// CreateAutopilot / UpdateAutopilot signatures stay untouched — same pattern
// as access.SetScope (JEH-724).
func SetOnAutopilot(ctx context.Context, q *db.Queries, autopilotID pgtype.UUID, model string) error {
	if err := Validate(model); err != nil {
		return err
	}
	return q.SetAutopilotModel(ctx, db.SetAutopilotModelParams{
		ID:    autopilotID,
		Model: model,
	})
}

// SetOnTaskForProvider persists the per-task override after checking it against
// the provider that will actually run the task. Prefer this over SetOnTask
// wherever the target runtime is known (FIR-3287).
//
// It exists because SetOnTask validates against the cross-provider union, which
// is both too weak (it accepts an Anthropic model destined for a Codex runtime)
// and too strong (it rejects a real model belonging to an uncatalogued provider
// such as hermes). Knowing the provider resolves both.
func SetOnTaskForProvider(ctx context.Context, q *db.Queries, taskID pgtype.UUID, provider, model string) error {
	if model == "" {
		return nil
	}
	if err := ValidateForProvider(provider, model); err != nil {
		return err
	}
	return q.SetAgentTaskModelOverride(ctx, db.SetAgentTaskModelOverrideParams{
		ID:            taskID,
		ModelOverride: model,
	})
}

// SetOnTask persists the per-task override on an agent_task_queue row. Called
// by the autopilot dispatcher immediately after the upstream CreateAgentTask
// when the source autopilot carries a model override.
func SetOnTask(ctx context.Context, q *db.Queries, taskID pgtype.UUID, model string) error {
	if model == "" {
		return nil
	}
	if err := Validate(model); err != nil {
		return err
	}
	return q.SetAgentTaskModelOverride(ctx, db.SetAgentTaskModelOverrideParams{
		ID:            taskID,
		ModelOverride: model,
	})
}

// SetThinkingOnTask persists the per-task thinking override on an
// agent_task_queue row. Empty thinking is a no-op so the agent profile remains
// the default.
func SetThinkingOnTask(ctx context.Context, q *db.Queries, taskID pgtype.UUID, thinking string) error {
	if thinking == "" {
		return nil
	}
	return q.SetAgentTaskThinkingOverride(ctx, db.SetAgentTaskThinkingOverrideParams{
		ID:               taskID,
		ThinkingOverride: thinking,
	})
}

// Resolve returns the model the daemon should pass to the agent CLI. Resolution
// order is task.model_override → agent.model → "" (lets the CLI pick its own
// default). The daemon's env-var fallback (MULTICA_<PROVIDER>_MODEL) layers
// above "" — Resolve does not look at env vars because they live in daemon
// config space, not the DB.
//
// Both inputs are pgtype.Text because they may be NULL or empty in storage.
func Resolve(taskOverride, agentModel pgtype.Text) string {
	if taskOverride.Valid && taskOverride.String != "" {
		return taskOverride.String
	}
	if agentModel.Valid && agentModel.String != "" {
		return agentModel.String
	}
	return ""
}

// RuntimeBriefLine returns the string the daemon should inject into the
// runtime brief so the agent can self-report which model it is running on.
// Empty model → empty line, the caller should omit it. This is Phase 2 (a)
// of JEH-1310: "give the agent visibility of its own model", deliberately
// info-only (the agent cannot change its own model mid-run).
func RuntimeBriefLine(model string) string {
	if model == "" {
		return ""
	}
	return fmt.Sprintf("Current model: %s", model)
}
