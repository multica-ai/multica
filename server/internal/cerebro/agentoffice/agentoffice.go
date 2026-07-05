// Package agentoffice implements Phase 1 of the "Agent Office" (FIR-1775):
// versioning + governance for an agent's full runtime context, mirroring the
// skill-governance model (skill_version / skill_change_request) but for the
// agent COMPOSITE — instructions, bound skills, model, thinking_level,
// mcp_config, custom_args, runtime_config, persona_sandbox, and the NAMES (never
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
	"regexp"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
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
	Instructions   string          `json:"instructions"`
	Description    string          `json:"description"`
	Model          string          `json:"model"`
	ThinkingLevel  string          `json:"thinking_level"`
	McpConfig      json.RawMessage `json:"mcp_config"`
	CustomArgs     json.RawMessage `json:"custom_args"`
	RuntimeConfig  json.RawMessage `json:"runtime_config"`
	PersonaSandbox string          `json:"persona_sandbox"`
	SkillIDs       []string        `json:"skill_ids"`
	CustomEnvKeys  []string        `json:"custom_env_keys"`
}

// --- Snapshot compose / encode ---

// ComposeCurrentSnapshot builds a ContextSnapshot from the live agent row plus
// its currently bound skill ids. It is the source of truth for "what does the
// agent look like right now" — used to seed a propose flow and to diff against.
func ComposeCurrentSnapshot(agent cerebrodb.Agent, skillIDs []pgtype.UUID) ContextSnapshot {
	ids := make([]string, 0, len(skillIDs))
	for _, s := range skillIDs {
		ids = append(ids, util.UUIDToString(s))
	}
	return ContextSnapshot{
		Instructions:   agent.Instructions,
		Description:    agent.Description,
		Model:          agent.Model.String,
		ThinkingLevel:  agent.ThinkingLevel.String,
		McpConfig:      rawOrEmpty(agent.McpConfig),
		CustomArgs:     rawOrEmpty(agent.CustomArgs),
		RuntimeConfig:  rawOrEmpty(agent.RuntimeConfig),
		PersonaSandbox: agent.PersonaSandbox.String,
		SkillIDs:       ids,
		CustomEnvKeys:  customEnvKeys(agent.CustomEnv),
	}
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
		PersonaSandbox: textOrNull(snap.PersonaSandbox),
		McpConfig:      jsonOrEmpty(snap.McpConfig),
		CustomArgs:     jsonOrEmpty(snap.CustomArgs),
		RuntimeConfig:  jsonOrEmpty(snap.RuntimeConfig),
		ContextVersion: newVersion,
	})
	if err != nil {
		return cerebrodb.Agent{}, fmt.Errorf("apply snapshot: %w", err)
	}
	if err := qtx.DeleteAgentSkillsForContext(ctx, agentID); err != nil {
		return cerebrodb.Agent{}, fmt.Errorf("clear skill bindings: %w", err)
	}
	for _, raw := range snap.SkillIDs {
		sid, err := util.ParseUUID(raw)
		if err != nil {
			// A snapshot skill id that no longer parses is skipped rather than
			// failing the whole apply — the binding simply drops.
			continue
		}
		if err := qtx.InsertAgentSkillForContext(ctx, cerebrodb.InsertAgentSkillForContextParams{
			AgentID: agentID,
			SkillID: sid,
		}); err != nil {
			return cerebrodb.Agent{}, fmt.Errorf("rebind skill %s: %w", raw, err)
		}
	}
	return agent, nil
}

// --- Semver helpers (mirrors skill_ownership.go) ---

var semverRe = regexp.MustCompile(`^(\d+)\.(\d+)\.(\d+)$`)

// ValidSemver reports whether s is a strict X.Y.Z form.
func ValidSemver(s string) bool { return semverRe.MatchString(s) }

// SemverGT reports whether a > b under strict semver.
func SemverGT(a, b string) bool {
	am := semverRe.FindStringSubmatch(a)
	bm := semverRe.FindStringSubmatch(b)
	if am == nil || bm == nil {
		return false
	}
	for i := 1; i <= 3; i++ {
		if am[i] == bm[i] {
			continue
		}
		if len(am[i]) != len(bm[i]) {
			return len(am[i]) > len(bm[i])
		}
		return am[i] > bm[i]
	}
	return false
}

// BumpPatch returns the next patch version of a valid semver, used when a
// rollback needs a fresh version number greater than current.
func BumpPatch(v string) string {
	m := semverRe.FindStringSubmatch(v)
	if m == nil {
		return "1.0.0"
	}
	patch := 0
	fmt.Sscanf(m[3], "%d", &patch)
	return fmt.Sprintf("%s.%s.%d", m[1], m[2], patch+1)
}

// --- Diff (mirrors the LCS diff in skill_ownership.go) ---

// DiffSnapshots renders each snapshot to a canonical text form and returns a
// unified-diff-style string between them. The render covers the whole composite
// (metadata header + instructions block) so a reviewer sees every drift.
func DiffSnapshots(base, proposed ContextSnapshot) string {
	return computeUnifiedDiff(RenderSnapshot(base), RenderSnapshot(proposed), "agent-context")
}

// RenderSnapshot produces a stable, human-readable text rendering of a snapshot
// for diffing and review. JSONB blobs are compacted so formatting noise does not
// show up as a change.
func RenderSnapshot(s ContextSnapshot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "model: %s\n", s.Model)
	fmt.Fprintf(&b, "thinking_level: %s\n", s.ThinkingLevel)
	fmt.Fprintf(&b, "persona_sandbox: %s\n", s.PersonaSandbox)
	fmt.Fprintf(&b, "skills: %s\n", strings.Join(s.SkillIDs, ", "))
	fmt.Fprintf(&b, "custom_env_keys: %s\n", strings.Join(s.CustomEnvKeys, ", "))
	fmt.Fprintf(&b, "mcp_config: %s\n", compactJSON(s.McpConfig))
	fmt.Fprintf(&b, "custom_args: %s\n", compactJSON(s.CustomArgs))
	fmt.Fprintf(&b, "runtime_config: %s\n", compactJSON(s.RuntimeConfig))
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

func computeUnifiedDiff(base, proposed, label string) string {
	if base == proposed {
		return ""
	}
	ops := diffLines(splitLines(base), splitLines(proposed))
	var b strings.Builder
	fmt.Fprintf(&b, "--- %s (base)\n+++ %s (proposed)\n", label, label)
	for _, op := range ops {
		switch op.kind {
		case diffEqual:
			fmt.Fprintf(&b, " %s\n", op.line)
		case diffDel:
			fmt.Fprintf(&b, "-%s\n", op.line)
		case diffAdd:
			fmt.Fprintf(&b, "+%s\n", op.line)
		}
	}
	return b.String()
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	out := strings.Split(s, "\n")
	if len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

const (
	diffEqual = iota
	diffAdd
	diffDel
)

type diffOp struct {
	kind int
	line string
}

// diffLines is a textbook LCS-based line diff. Sufficient for instruction-sized
// inputs; the O(N*M) cost is bounded by the truncation applied before storage.
func diffLines(a, b []string) []diffOp {
	n, m := len(a), len(b)
	if n == 0 {
		out := make([]diffOp, m)
		for i, line := range b {
			out[i] = diffOp{kind: diffAdd, line: line}
		}
		return out
	}
	if m == 0 {
		out := make([]diffOp, n)
		for i, line := range a {
			out[i] = diffOp{kind: diffDel, line: line}
		}
		return out
	}
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	var ops []diffOp
	i, j := n, m
	for i > 0 && j > 0 {
		if a[i-1] == b[j-1] {
			ops = append([]diffOp{{kind: diffEqual, line: a[i-1]}}, ops...)
			i--
			j--
		} else if dp[i-1][j] >= dp[i][j-1] {
			ops = append([]diffOp{{kind: diffDel, line: a[i-1]}}, ops...)
			i--
		} else {
			ops = append([]diffOp{{kind: diffAdd, line: b[j-1]}}, ops...)
			j--
		}
	}
	for i > 0 {
		ops = append([]diffOp{{kind: diffDel, line: a[i-1]}}, ops...)
		i--
	}
	for j > 0 {
		ops = append([]diffOp{{kind: diffAdd, line: b[j-1]}}, ops...)
		j--
	}
	return ops
}
