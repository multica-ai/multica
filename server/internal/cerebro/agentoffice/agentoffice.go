// Package agentoffice implements Phase 1 of the "Agent Office" (FIR-1775):
// versioning + governance for an agent's full runtime context, mirroring the
// skill-governance model (skill_version / skill_change_request) but for the
// agent COMPOSITE — instructions, bound skills, model, thinking_level,
// mcp_config, custom_args, runtime_config, and the NAMES (never
// values) of custom_env keys.
//
// It lives in the cerebro zone so it survives upstream syncs. The HTTP layer is
// in handler.go; this file holds the service, the composite snapshot type, and
// the pure helpers (semver, diff, snapshot compose/apply).
package agentoffice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/versioning"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// TxStarter is the subset of *pgxpool.Pool the service needs to open a
// transaction. Matches the pattern used by the grants service.
type TxStarter interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Service wires the agent-context governance queries together. Cerebro holds the
// cerebrodb queries; Tx (the pool) lets review/rollback run atomically. Bus is
// the event bus used to fan a "change request proposed" event out to the
// inbox/notification channel matrix (mirrors the skill-governance flow); it may
// be nil in tests, in which case notifications are silently skipped.
type Service struct {
	Cerebro *cerebrodb.Queries
	Tx      TxStarter
	Bus     *events.Bus
}

// New constructs the service.
func New(cerebro *cerebrodb.Queries, tx TxStarter, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Tx: tx, Bus: bus}
}

// ContextSnapshot is the composite document stored in
// agent_context_version.snapshot and agent_change_request.proposed_snapshot. It
// mirrors the JSONB built by the backfill in the 9100 migration. custom_env
// holds key NAMES only — secret values are never versioned here.
type ContextSnapshot struct {
	Instructions  string          `json:"instructions"`
	Description   string          `json:"description"`
	RuntimeID     string          `json:"runtime_id,omitempty"`
	Model         string          `json:"model"`
	ThinkingLevel string          `json:"thinking_level"`
	McpConfig     json.RawMessage `json:"mcp_config"`
	CustomArgs    json.RawMessage `json:"custom_args"`
	RuntimeConfig json.RawMessage `json:"runtime_config"`
	SkillIDs      []string        `json:"skill_ids"`
	CustomEnvKeys []string        `json:"custom_env_keys"`
	// AlwaysOnSkillIDs (FIR-3805) is the subset of SkillIDs whose full text is
	// pasted into the agent's instructions on every run instead of being listed
	// as a one-line, load-on-demand entry. It is a parallel list rather than a
	// richer SkillIDs element so existing snapshots decode unchanged and the
	// versioned diff of "which skills are bound" stays readable on its own.
	AlwaysOnSkillIDs []string `json:"always_on_skill_ids,omitempty"`
}

// --- Snapshot compose / encode ---

// ComposeCurrentSnapshot builds a ContextSnapshot from the live agent row plus
// its currently bound skill ids. It is the source of truth for "what does the
// agent look like right now" — used to seed a propose flow and to diff against.
func ComposeCurrentSnapshot(agent cerebrodb.Agent, bindings []cerebrodb.ListAgentSkillIDsForContextRow) ContextSnapshot {
	ids := make([]string, 0, len(bindings))
	var alwaysOn []string
	for _, b := range bindings {
		id := util.UUIDToString(b.SkillID)
		ids = append(ids, id)
		if b.AlwaysOn {
			alwaysOn = append(alwaysOn, id)
		}
	}
	return ContextSnapshot{
		Instructions:  agent.Instructions,
		Description:   agent.Description,
		RuntimeID:     util.UUIDToString(agent.RuntimeID),
		Model:         agent.Model.String,
		ThinkingLevel: agent.ThinkingLevel.String,
		McpConfig:     rawOrEmpty(agent.McpConfig),
		CustomArgs:    rawOrEmpty(agent.CustomArgs),
		RuntimeConfig: rawOrEmpty(agent.RuntimeConfig),
		SkillIDs:      ids,
		CustomEnvKeys: customEnvKeys(agent.CustomEnv),

		AlwaysOnSkillIDs: alwaysOn,
	}
}

// NormalizeAlwaysOnSkills (FIR-3805) drops any always-on id that is not in the
// snapshot's bound skill set, de-duplicates the rest, and orders it like
// SkillIDs so two snapshots describing the same state compare equal. Callers
// that assemble a snapshot from client input run it before storing; snapshots
// composed from the live rows are already consistent by construction.
func NormalizeAlwaysOnSkills(s ContextSnapshot) ContextSnapshot {
	if len(s.AlwaysOnSkillIDs) == 0 {
		s.AlwaysOnSkillIDs = nil
		return s
	}
	flagged := make(map[string]bool, len(s.AlwaysOnSkillIDs))
	for _, id := range s.AlwaysOnSkillIDs {
		flagged[id] = true
	}
	var out []string
	seen := make(map[string]bool, len(flagged))
	for _, id := range s.SkillIDs {
		if flagged[id] && !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	s.AlwaysOnSkillIDs = out
	return s
}

// EncodeSnapshot marshals a snapshot to JSONB bytes, never returning nil so the
// NOT NULL column always gets a value.
func EncodeSnapshot(s ContextSnapshot) []byte {
	b, err := json.Marshal(s)
	if err != nil || len(b) == 0 {
		return []byte("{}")
	}
	return b
}

// DecodeSnapshot parses stored JSONB back into a snapshot.
func DecodeSnapshot(raw []byte) ContextSnapshot {
	var s ContextSnapshot
	if len(raw) == 0 {
		return s
	}
	_ = json.Unmarshal(raw, &s)
	return s
}

// customEnvKeys extracts the sorted key names of a custom_env JSONB object,
// dropping all values. A malformed or empty blob yields an empty slice.
func customEnvKeys(raw []byte) []string {
	if len(raw) == 0 {
		return []string{}
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return []string{}
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sortStrings(keys)
	return keys
}

func rawOrEmpty(b []byte) json.RawMessage {
	if len(b) == 0 {
		return json.RawMessage("{}")
	}
	return json.RawMessage(b)
}

func jsonOrEmpty(r json.RawMessage) []byte {
	if len(r) == 0 {
		return []byte("{}")
	}
	return []byte(r)
}

func textOrNull(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

func uuidOrNull(s string) pgtype.UUID {
	id, err := util.ParseUUID(s)
	if err != nil {
		return pgtype.UUID{}
	}
	return id
}

// sortStrings is a tiny insertion sort to avoid pulling in sort just for key
// ordering (keeps the dependency surface small and deterministic).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

// --- Apply (used by review-merge and rollback) ---

// ApplySnapshotTx writes a snapshot onto the live agent row, replaces the skill
// bindings, and bumps context_version — all on the supplied transaction-scoped
// queries. The caller owns the transaction (begin/commit/rollback). custom_env
// values are untouched: only names are versioned.
func (s *Service) ApplySnapshotTx(ctx context.Context, qtx *cerebrodb.Queries, agentID pgtype.UUID, snap ContextSnapshot, newVersion string) (cerebrodb.Agent, error) {
	agent, err := qtx.ApplyAgentContextSnapshot(ctx, cerebrodb.ApplyAgentContextSnapshotParams{
		ID:             agentID,
		Instructions:   snap.Instructions,
		Description:    snap.Description,
		Model:          textOrNull(snap.Model),
		ThinkingLevel:  textOrNull(snap.ThinkingLevel),
		McpConfig:      jsonOrEmpty(snap.McpConfig),
		CustomArgs:     jsonOrEmpty(snap.CustomArgs),
		RuntimeConfig:  jsonOrEmpty(snap.RuntimeConfig),
		RuntimeID:      uuidOrNull(snap.RuntimeID),
		ContextVersion: newVersion,
	})
	if err != nil {
		return cerebrodb.Agent{}, fmt.Errorf("apply snapshot: %w", err)
	}
	if err := qtx.DeleteAgentSkillsForContext(ctx, agentID); err != nil {
		return cerebrodb.Agent{}, fmt.Errorf("clear skill bindings: %w", err)
	}
	alwaysOn := make(map[string]bool, len(snap.AlwaysOnSkillIDs))
	for _, raw := range snap.AlwaysOnSkillIDs {
		alwaysOn[raw] = true
	}
	for _, raw := range snap.SkillIDs {
		sid, err := util.ParseUUID(raw)
		if err != nil {
			// A snapshot skill id that no longer parses is skipped rather than
			// failing the whole apply — the binding simply drops.
			continue
		}
		if err := qtx.InsertAgentSkillForContext(ctx, cerebrodb.InsertAgentSkillForContextParams{
			AgentID:  agentID,
			SkillID:  sid,
			AlwaysOn: alwaysOn[raw],
		}); err != nil {
			return cerebrodb.Agent{}, fmt.Errorf("rebind skill %s: %w", raw, err)
		}
	}
	return agent, nil
}

// --- Semver helpers (shared foundation, FIR-2698) ---

// ValidSemver, SemverGT and BumpPatch are thin aliases over the shared
// versioning package so existing call sites keep their names. The
// implementations moved to server/internal/cerebro/versioning together with
// the LCS diff engine — this package keeps only the agent-context-specific
// snapshot compose/apply/render logic.

// ValidSemver reports whether s is a strict X.Y.Z form.
func ValidSemver(s string) bool { return versioning.ValidSemver(s) }

// SemverGT reports whether a > b under strict semver.
func SemverGT(a, b string) bool { return versioning.SemverGT(a, b) }

// BumpPatch returns the next patch version of a valid semver, used when a
// rollback needs a fresh version number greater than current.
func BumpPatch(v string) string { return versioning.BumpPatch(v) }

// --- Diff ---

// DiffSnapshots renders each snapshot to a canonical text form and returns a
// unified-diff-style string between them. The render covers the whole composite
// (metadata header + instructions block) so a reviewer sees every drift.
func DiffSnapshots(base, proposed ContextSnapshot) string {
	return versioning.UnifiedDiff(RenderSnapshot(base), RenderSnapshot(proposed), "agent-context")
}

// RenderSnapshot produces a stable, human-readable text rendering of a snapshot
// for diffing and review. JSONB blobs are compacted so formatting noise does not
// show up as a change.
func RenderSnapshot(s ContextSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "runtime_id: %s\n", s.RuntimeID)
	fmt.Fprintf(&b, "model: %s\n", s.Model)
	fmt.Fprintf(&b, "thinking_level: %s\n", s.ThinkingLevel)
	// Lifted out of the runtime_config blob below: a knob that changes how the
	// whole system prompt reaches the model must be legible to a reviewer, not
	// buried as one key in a line of compacted JSON. FIR-3212.
	fmt.Fprintf(&b, "system_prompt_mode: %s\n", SystemPromptModeOf(s))
	// Same rule for the other two brief layers (FIR-3212): a knob that removes
	// whole sections of what the agent reads must be legible to a reviewer.
	fmt.Fprintf(&b, "workspace_brief_mode: %s\n", WorkspaceBriefModeOf(s))
	fmt.Fprintf(&b, "tools_brief_mode: %s\n", ToolsBriefModeOf(s))
	runSettings := RuntimeSettingsOf(s)
	fmt.Fprintf(&b, "speed_mode: %s\n", runSettings.SpeedMode)
	fmt.Fprintf(&b, "max_turns: %d\n", runSettings.MaxTurns)
	fmt.Fprintf(&b, "timeout_minutes: %d\n", runSettings.TimeoutMinutes)
	fmt.Fprintf(&b, "skills: %s\n", strings.Join(s.SkillIDs, ", "))
	// FIR-3805: rendered on its own line so flipping "always on" for one skill
	// shows up in review as its own change, not as noise inside the skills line.
	fmt.Fprintf(&b, "always_on_skills: %s\n", strings.Join(s.AlwaysOnSkillIDs, ", "))
	fmt.Fprintf(&b, "custom_env_keys: %s\n", strings.Join(s.CustomEnvKeys, ", "))
	fmt.Fprintf(&b, "mcp_config: %s\n", compactJSON(s.McpConfig))
	fmt.Fprintf(&b, "custom_args: %s\n", compactJSON(s.CustomArgs))
	fmt.Fprintf(&b, "runtime_config: %s\n", compactJSON(RuntimeConfigRest(s)))
	fmt.Fprintf(&b, "--- instructions ---\n%s\n", s.Instructions)
	return b.String()
}

func compactJSON(r json.RawMessage) string {
	if len(r) == 0 {
		return "{}"
	}
	var buf bytes.Buffer
	if err := json.Compact(&buf, r); err != nil {
		return string(r)
	}
	return buf.String()
}
