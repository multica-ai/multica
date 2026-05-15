// Package autopilotmodel — per-autopilot and per-task model override (JEH-1310).
//
// In upstream the model an agent runs on is resolved at agent level only
// (server/internal/daemon/daemon.go: agent.Model → MULTICA_<PROVIDER>_MODEL
// env → CLI default). Cerebro adds a third tier above agent.Model so that
// individual autopilots can ship cheap status pings on Haiku without
// duplicating the agent itself, and so that a future per-mention picker can
// override a single run.
//
// The registry below is the *valid choice list* the API accepts and the UI
// renders. It is deliberately small (Anthropic models only, identifiers
// matched against the strings the Claude CLI's --model flag accepts) — adding
// a model is a one-line PR, not a migration.
package autopilotmodel

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Static list of accepted model IDs. Order matters — the API and UI present
// them cheapest → most expensive so a reader scanning the dropdown sees the
// cost-saving option first.
const (
	ModelHaiku  = "claude-haiku-4-5-20251001"
	ModelSonnet = "claude-sonnet-4-6"
	ModelOpus   = "claude-opus-4-7"
)

// Allowed returns the registry in display order. Used by the API to expose
// the list to the UI without hardcoding strings on the client (so adding a
// model on the server is enough — no coordinated FE release needed).
func Allowed() []string {
	return []string{ModelHaiku, ModelSonnet, ModelOpus}
}

// ErrUnknownModel is returned by Validate when a caller supplies a model ID
// that is not in the registry. The empty string is NOT an error — it means
// "use the agent default", same semantics as autopilot.model = NULL.
var ErrUnknownModel = errors.New("model not in registry")

// Validate checks model against the registry. An empty string passes (clears
// the override). Returns a wrapped error including the offending value so a
// 400 response can include the list of acceptable values inline.
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
