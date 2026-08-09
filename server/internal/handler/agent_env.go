package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/logger"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// envSentinel is the masked marker the UI / clients see in place of a
// real value. A PUT body carrying it for a given key means "do not
// overwrite the existing value for that key" — a defense-in-depth
// guard so a client that round-trips a partially-revealed map cannot
// silently destroy real secrets by saving the masked placeholder.
const envSentinel = "****"

// agentEnvActivityRevealed and agentEnvActivityUpdated are the
// activity_log `action` constants for the two env-management
// endpoints. Stored on rows where `issue_id IS NULL` (env access is not
// tied to any issue). Owners can later query them — a queryable audit
// UI is out of scope for this PR, but the rows are written now so the
// data is captured from day one. Workspace activity history will
// eventually surface them; for now they're forensic-only.
const (
	agentEnvActivityRevealed = "agent_env_revealed"
	agentEnvActivityUpdated  = "agent_env_updated"
)

// AgentEnvResponse is the wire shape for the dedicated env-management
// endpoint. Kept distinct from `AgentResponse` so secrets cannot leak
// back into the generic agent resource by accident — a future
// refactor that adds a field to AgentResponse cannot accidentally
// pull env values along.
type AgentEnvResponse struct {
	AgentID   string            `json:"agent_id"`
	CustomEnv map[string]string `json:"custom_env"`
}

// UpdateAgentEnvRequest is the wire shape for `PUT
// /api/agents/{id}/env`. Only `custom_env` is accepted — fewer
// surfaces, less to misuse.
type UpdateAgentEnvRequest struct {
	CustomEnv map[string]string `json:"custom_env"`
}

// authorizeAgentEnv enforces the per-request auth contract for the env
// endpoints:
//
//  1. The actor MUST resolve to a member (human). Any request authored
//     by an agent token — even one whose backing member is a workspace
//     owner, or the very human who owns the target agent — is rejected.
//     This is the key fix for the impersonation/lateral-movement risk
//     that motivated MUL-2600: an agent running in the workspace cannot
//     use its host's owner credentials to reveal another agent's
//     secrets.
//  2. The member must be a workspace owner/admin, or the agent's own
//     human owner (MUL-5438).
//
// Rule 2 used to be workspace-role-only, which made env the single
// endpoint in the agent permission model that ignored agent ownership:
// canManageAgent already lets the owner update/archive/restore, and
// canViewAgentSecrets already lets the owner read mcp_config. The
// asymmetry was worst on create — POST /api/agents accepts custom_env
// from any member — so a member could write secrets into their own
// agent and then never read or rotate them.
//
// Returns the loaded agent and the authenticated member on success.
// All non-2xx branches write their own response and return ok=false.
func (h *Handler) authorizeAgentEnv(w http.ResponseWriter, r *http.Request) (db.Agent, db.Member, bool) {
	agentID := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, agentID)
	if !ok {
		return db.Agent{}, db.Member{}, false
	}

	workspaceID := uuidToString(agent.WorkspaceID)
	userID := requestUserID(r)

	// Reject agent actors before anything else. resolveActor returns
	// "agent" iff both X-Agent-ID and a valid X-Task-ID are present and
	// the task belongs to that agent — so this guard is precise and
	// cannot be tricked by a member-supplied header.
	actorType, _ := h.resolveActor(r, userID, workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access env management endpoints")
		return db.Agent{}, db.Member{}, false
	}

	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "agent not found", "owner", "admin", "member")
	if !ok {
		return db.Agent{}, db.Member{}, false
	}
	if !canManageAgentEnv(agent, member) {
		writeError(w, http.StatusForbidden, "only the agent owner or a workspace owner/admin can manage this agent's env")
		return db.Agent{}, db.Member{}, false
	}

	return agent, member, true
}

// canManageAgentEnv is the pure half of the env authorization rule:
// a workspace owner/admin, or the human who owns the agent. Mirrors
// canManageAgent (update/archive) so a member who can manage an agent
// can also rotate its secrets.
//
// The owner comparison deliberately runs against member.UserID rather
// than requestUserID(r). agent.owner_id is nullable (migration 001) and
// uuidToString renders a NULL UUID as "", so comparing against a raw
// header — which can also be empty — would make every NULL-owner agent
// readable by anyone. member.UserID only exists after the membership
// lookup succeeded, and the empty-owner guard below closes the case
// from the other side as well.
func canManageAgentEnv(agent db.Agent, member db.Member) bool {
	return canManageAgentForMember(agent, member)
}

// GetAgentEnv returns the plaintext custom_env map for a single agent
// after gating through authorizeAgentEnv. Every successful read writes
// an `agent_env_revealed` row to activity_log (keys only, never
// values) so workspace owners have a trail of who saw which keys.
//
// Audit semantics are fail-closed: if we cannot persist the audit row
// we MUST NOT serve the plaintext. A reveal we cannot record is
// indistinguishable from an unaudited reveal, which would silently
// break the MUL-2600 promise of "every reveal leaves a queryable
// trail". Operators who hit a 500 here see the audit-log outage and
// can fix it; the alternative — quietly handing out secrets — is
// invisible.
func (h *Handler) GetAgentEnv(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}

	customEnv := unmarshalCustomEnv(agent)

	revealedKeys := sortedKeys(customEnv)
	details, _ := json.Marshal(map[string]any{
		"agent_id":      uuidToString(agent.ID),
		"agent_name":    agent.Name,
		"revealed_keys": revealedKeys,
		"key_count":     len(revealedKeys),
	})
	if _, err := h.Queries.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{}, // env access is not tied to an issue
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentEnvActivityRevealed,
		Details:     details,
	}); err != nil {
		slog.Error("agent_env_revealed audit write failed; refusing to serve plaintext",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; refusing to serve env without a recorded reveal")
		return
	}

	writeJSON(w, http.StatusOK, AgentEnvResponse{
		AgentID:   uuidToString(agent.ID),
		CustomEnv: customEnv,
	})
}

// UpdateAgentEnv replaces an agent's custom_env wholesale. The **** marker is
// honoured per-key: any value equal to envSentinel is treated as
// "keep the existing value for that key", protecting against the
// scenario where a UI fetches the env, exposes some values but leaves
// others masked, and then naively PUTs the whole map back. A
// straightforward write would have stored literal `****` in place of
// the real secret. Audit log captures the symmetric difference between
// old and new keys but never values.
//
// Persist + audit run inside one DB transaction so they commit
// together or roll back together. An audit-write outage cannot leave
// an unaudited env mutation on disk, and a persist failure does not
// leave a phantom audit row claiming a change that never happened.
func (h *Handler) UpdateAgentEnv(w http.ResponseWriter, r *http.Request) {
	agent, member, ok := h.authorizeAgentEnv(w, r)
	if !ok {
		return
	}

	var req UpdateAgentEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.CustomEnv == nil {
		req.CustomEnv = map[string]string{}
	}

	existing := unmarshalCustomEnv(agent)
	merged, audit := mergeAgentEnv(existing, req.CustomEnv)

	envBytes, err := json.Marshal(merged)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode env")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		slog.Error("agent_env update: begin tx failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	updated, err := qtx.UpdateAgentCustomEnv(r.Context(), db.UpdateAgentCustomEnvParams{
		ID:        agent.ID,
		CustomEnv: envBytes,
	})
	if err != nil {
		slog.Warn("update agent custom_env failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}

	auditDetails := map[string]any{
		"agent_id":       uuidToString(agent.ID),
		"agent_name":     agent.Name,
		"added_keys":     audit.added,
		"removed_keys":   audit.removed,
		"changed_keys":   audit.changed,
		"preserved_keys": audit.preserved,
	}
	details, _ := json.Marshal(auditDetails)
	if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
		WorkspaceID: agent.WorkspaceID,
		IssueID:     pgtype.UUID{},
		ActorType:   pgtype.Text{String: "member", Valid: true},
		ActorID:     parseUUID(uuidToString(member.UserID)),
		Action:      agentEnvActivityUpdated,
		Details:     details,
	}); err != nil {
		slog.Error("agent_env_updated audit write failed; rolling back update",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "audit log write failed; env update rolled back")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		slog.Error("agent_env update: tx commit failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(agent.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to update env")
		return
	}

	// Broadcast an agent:status update so connected clients refresh the
	// "N variables configured" indicator. Payload is the redacted
	// AgentResponse — no env values are sent. Skills are reloaded so the
	// broadcast doesn't tell subscribers the agent has no skills (#3459).
	resp := h.agentToResponse(updated)
	if err := h.attachAgentSkills(r.Context(), &resp, updated.ID); err != nil {
		slog.Warn("load agent skills after env update failed",
			append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(updated.ID))...)
		writeError(w, http.StatusInternalServerError, "failed to load agent skills")
		return
	}
	workspaceID := uuidToString(updated.WorkspaceID)
	h.publish(protocol.EventAgentStatus, workspaceID, "member", uuidToString(member.UserID), map[string]any{"agent": broadcastAgentResponse(resp)})

	writeJSON(w, http.StatusOK, AgentEnvResponse{
		AgentID:   uuidToString(updated.ID),
		CustomEnv: merged,
	})
}

// maxMergeAgentsEnvBatch bounds one bulk env write, for the same reason
// maxMigrateAgentsBatch bounds a migration: an unbounded selection would row-lock
// an unbounded set for the length of a transaction.
const maxMergeAgentsEnvBatch = 200

// mergeAgentsEnvRequest is the wire shape for PATCH /api/agents/env.
//
// The verb is deliberate. PUT /api/agents/{id}/env replaces an agent's env
// wholesale, which is unusable for injection: preserving the keys you are not
// touching requires knowing their names, and the only way to learn them is
// GET /env — a plaintext read of every secret plus an `agent_env_revealed`
// audit row per agent. This endpoint merges instead, so injecting one key into
// twenty agents never reveals a single existing value.
type mergeAgentsEnvRequest struct {
	AgentIDs []string `json:"agent_ids"`
	// Keys to add or overwrite. Values are secret material and never come
	// back in the response.
	Set map[string]string `json:"set"`
}

// mergeAgentsEnvResult reports what happened to ONE agent, in key names only.
//
// Only keys the caller submitted are named, so the response tells them nothing
// they did not already type — except whether each key already existed on each
// agent. That existence bit is the accepted minimum leak for this endpoint
// (MUL-5758 review): the caller is the agent's owner or a workspace admin, both
// of whom may read the full env through the detail page anyway.
type mergeAgentsEnvResult struct {
	AgentID string `json:"agent_id"`
	Name    string `json:"name"`
	// Submitted keys the agent did not have before.
	AddedKeys []string `json:"added_keys"`
	// Submitted keys that replaced a different existing value.
	OverwrittenKeys []string `json:"overwritten_keys"`
	// Total keys configured on the agent after the merge. Mirrors
	// custom_env_key_count on the agent resource so the caller can refresh
	// its row without a second read.
	KeyCount int `json:"key_count"`
}

type mergeAgentsEnvResponse struct {
	Results []mergeAgentsEnvResult `json:"results"`
	Skipped []skippedAgentResult   `json:"skipped"`
}

// MergeAgentsEnv adds or overwrites a set of environment keys across one or
// more agents, leaving every key the caller did not name untouched.
//
// One handler for the row menu (one agent) and the batch toolbar (N agents);
// N=1 is not a special case. Deleting keys is deliberately NOT supported here —
// the detail page's env tab owns full env management, including removal, and
// bulk deletion was scoped out because a mistaken bulk delete of secrets is
// unrecoverable from the UI.
//
// Authorization repeats both rules the single-agent env endpoints enforce:
// agent actors are rejected outright, and each agent is writable only by its
// human owner or a workspace owner/admin. Agents that fail the second rule are
// skipped, not fatal — same bulk contract as the migration endpoint.
//
// Persist + audit share one transaction, so the fail-closed promise the
// single-agent path makes holds here too: an audit-log outage cannot leave an
// unaudited env mutation on disk, for any agent in the batch.
func (h *Handler) MergeAgentsEnv(w http.ResponseWriter, r *http.Request) {
	var req mergeAgentsEnvRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	agentIDs, ok := parseBulkAgentIDs(w, req.AgentIDs, maxMergeAgentsEnvBatch)
	if !ok {
		return
	}
	if len(req.Set) == 0 {
		writeError(w, http.StatusBadRequest, "set must contain at least one key")
		return
	}
	// Normalise at the boundary and reject collisions rather than resolving
	// them. Keys are trimmed before they are stored, so `"KEY"` and `" KEY "`
	// name the same variable; accepting both would let Go's randomised map
	// iteration decide which value survives, and the same request would write
	// different secrets on different runs. The UI cannot produce this, but the
	// API and CLI can.
	normalizedSet := make(map[string]string, len(req.Set))
	for key, value := range req.Set {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			writeError(w, http.StatusBadRequest, "env keys must not be blank")
			return
		}
		// The masked marker is what the single-agent path uses to mean
		// "keep the existing value". There is nothing to keep in an
		// injection, so accepting it here would either store a literal
		// "****" or silently drop the key.
		if value == envSentinel {
			writeError(w, http.StatusBadRequest, "env values must not be the masked placeholder")
			return
		}
		if _, dup := normalizedSet[trimmed]; dup {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("duplicate env key %q after trimming whitespace", trimmed))
			return
		}
		normalizedSet[trimmed] = value
	}

	workspaceID := h.resolveWorkspaceID(r)
	if workspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace not resolved")
		return
	}
	// Rule 1 of the env contract (MUL-2600): an agent token may not reach any
	// env-management endpoint, whatever its host member's role is.
	actorType, _ := h.resolveActor(r, requestUserID(r), workspaceID)
	if actorType == "agent" {
		writeError(w, http.StatusForbidden, "agents may not access env management endpoints")
		return
	}
	member, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin", "member")
	if !ok {
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	agents, err := qtx.ListAgentsByIDsForWorkspaceForUpdate(r.Context(), db.ListAgentsByIDsForWorkspaceForUpdateParams{
		AgentIds:    agentIDs,
		WorkspaceID: parseUUID(workspaceID),
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load agents")
		return
	}
	byID := make(map[string]db.Agent, len(agents))
	for _, a := range agents {
		byID[uuidToString(a.ID)] = a
	}
	// Same non-disclosure rule the migration endpoint applies: an agent this
	// caller cannot SEE is reported exactly like an id that never existed, so
	// a bulk request cannot confirm a hidden agent's existence or leak its
	// name through a `forbidden` skip (MUL-5758 security review).
	visible, ok := h.visibleAgentIDSet(r.Context(), agents, actorType, member)
	if !ok {
		writeError(w, http.StatusInternalServerError, "failed to resolve agent visibility")
		return
	}

	out := mergeAgentsEnvResponse{
		Results: []mergeAgentsEnvResult{},
		Skipped: []skippedAgentResult{},
	}
	updatedAgents := make([]db.Agent, 0, len(agentIDs))
	for _, id := range agentIDs {
		key := uuidToString(id)
		agent, found := byID[key]
		if _, seen := visible[key]; found && !seen {
			found = false
		}
		if !found {
			// An agent in another workspace, an agent hidden from this
			// caller, and an id that never existed are all reported
			// identically.
			out.Skipped = append(out.Skipped, skippedAgentResult{AgentID: key, Reason: migrateSkipNotFound})
			continue
		}
		if !canManageAgentEnv(agent, member) {
			out.Skipped = append(out.Skipped, skippedAgentResult{
				AgentID: key,
				Name:    agent.Name,
				Reason:  migrateSkipForbidden,
			})
			continue
		}

		existing := unmarshalCustomEnv(agent)
		merged, result := mergeEnvKeys(existing, normalizedSet)
		result.AgentID = key
		result.Name = agent.Name

		envBytes, err := json.Marshal(merged)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to encode env")
			return
		}
		updated, err := qtx.UpdateAgentCustomEnv(r.Context(), db.UpdateAgentCustomEnvParams{
			ID:        agent.ID,
			CustomEnv: envBytes,
		})
		if err != nil {
			slog.Warn("merge agent env: update failed",
				append(logger.RequestAttrs(r), "error", err, "agent_id", key)...)
			writeError(w, http.StatusInternalServerError, "failed to update env")
			return
		}

		// One audit row per agent, same action as the single-agent write so
		// existing queries over activity_log keep seeing every env mutation.
		// `source` distinguishes the bulk path for anyone reading the trail.
		details, _ := json.Marshal(map[string]any{
			"agent_id":     key,
			"agent_name":   agent.Name,
			"added_keys":   result.AddedKeys,
			"removed_keys": []string{},
			"changed_keys": result.OverwrittenKeys,
			"source":       "bulk_merge",
			"batch_size":   len(agentIDs),
		})
		if _, err := qtx.CreateActivity(r.Context(), db.CreateActivityParams{
			WorkspaceID: agent.WorkspaceID,
			IssueID:     pgtype.UUID{}, // env access is not tied to an issue
			ActorType:   pgtype.Text{String: "member", Valid: true},
			ActorID:     parseUUID(uuidToString(member.UserID)),
			Action:      agentEnvActivityUpdated,
			Details:     details,
		}); err != nil {
			slog.Error("agent_env_updated audit write failed; rolling back bulk merge",
				append(logger.RequestAttrs(r), "error", err, "agent_id", key)...)
			writeError(w, http.StatusInternalServerError, "audit log write failed; env update rolled back")
			return
		}

		out.Results = append(out.Results, result)
		updatedAgents = append(updatedAgents, updated)
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit transaction")
		return
	}

	// Post-commit: refresh each row's "N variables configured" indicator. The
	// payload is the redacted agent resource, which has never carried env
	// values.
	for _, a := range updatedAgents {
		resp := h.agentToResponse(a)
		if err := h.attachAgentSkills(r.Context(), &resp, a.ID); err != nil {
			slog.Warn("load agent skills after bulk env merge failed",
				append(logger.RequestAttrs(r), "error", err, "agent_id", uuidToString(a.ID))...)
			continue
		}
		h.publish(protocol.EventAgentStatus, workspaceID, "member", uuidToString(member.UserID), map[string]any{
			"agent": broadcastAgentResponse(resp),
		})
	}

	slog.Info("agent env merged",
		append(logger.RequestAttrs(r),
			"workspace_id", workspaceID,
			"agents", len(out.Results),
			"skipped", len(out.Skipped),
			"keys", len(normalizedSet))...)

	writeJSON(w, http.StatusOK, out)
}

// mergeEnvKeys applies an injection to one agent's env: every submitted key is
// written, every other key is left exactly as it was.
//
// `set` must already be normalised — trimmed keys, no duplicates. The handler
// does that at the request boundary and rejects collisions there, so this
// function never has to pick a winner between two spellings of one key.
//
// Returns the map to persist plus the per-agent result, whose key lists cover
// only what the caller submitted — the caller never learns a key name it did
// not already know.
func mergeEnvKeys(existing, set map[string]string) (map[string]string, mergeAgentsEnvResult) {
	merged := make(map[string]string, len(existing)+len(set))
	for k, v := range existing {
		merged[k] = v
	}
	result := mergeAgentsEnvResult{AddedKeys: []string{}, OverwrittenKeys: []string{}}
	for key, v := range set {
		old, had := existing[key]
		switch {
		case !had:
			result.AddedKeys = append(result.AddedKeys, key)
		case old != v:
			result.OverwrittenKeys = append(result.OverwrittenKeys, key)
		}
		// A submitted key whose value already matches is written anyway (a
		// no-op) but reported in neither list: nothing changed for it.
		merged[key] = v
	}
	sort.Strings(result.AddedKeys)
	sort.Strings(result.OverwrittenKeys)
	result.KeyCount = len(merged)
	return merged, result
}

// envAudit summarises the diff between an agent's existing env and the
// new one, broken down so an auditor can reconstruct exactly which
// keys an operation touched without leaking values. All slices are
// sorted to keep the activity row content deterministic for tests and
// downstream tooling.
type envAudit struct {
	added     []string
	removed   []string
	changed   []string
	preserved []string
}

// mergeAgentEnv applies the **** sentinel rule and returns both the
// final map to persist and an audit summary of which keys changed.
// Behaviour:
//   - request key present, value == "****", key exists in `existing`
//     → keep the existing value, append to preserved
//   - request key present, value == "****", key NOT in `existing`
//     → drop the key (literal "****" is never a valid stored value)
//   - request key present, value != "****", key already in existing
//     with same value → no-op (not counted)
//   - request key present, value != "****", different from existing
//     → write new value, append to changed
//   - request key present, value != "****", key NOT in existing
//     → write new value, append to added
//   - key in existing but absent from request → removed
func mergeAgentEnv(existing, request map[string]string) (map[string]string, envAudit) {
	merged := make(map[string]string, len(request))
	audit := envAudit{}

	for k, v := range request {
		if v == envSentinel {
			if old, ok := existing[k]; ok {
				merged[k] = old
				audit.preserved = append(audit.preserved, k)
			}
			// else: drop. We never persist a literal "****".
			continue
		}
		if old, ok := existing[k]; ok {
			if old == v {
				merged[k] = v
				continue
			}
			merged[k] = v
			audit.changed = append(audit.changed, k)
			continue
		}
		merged[k] = v
		audit.added = append(audit.added, k)
	}

	for k := range existing {
		if _, ok := request[k]; !ok {
			audit.removed = append(audit.removed, k)
		}
	}

	sort.Strings(audit.added)
	sort.Strings(audit.removed)
	sort.Strings(audit.changed)
	sort.Strings(audit.preserved)
	return merged, audit
}

// unmarshalCustomEnv decodes an agent's stored custom_env bytea into a
// map, returning an empty (never nil) map so callers can iterate
// safely.
func unmarshalCustomEnv(a db.Agent) map[string]string {
	out := map[string]string{}
	if len(a.CustomEnv) == 0 {
		return out
	}
	if err := json.Unmarshal(a.CustomEnv, &out); err != nil {
		slog.Warn("failed to unmarshal agent custom_env", "agent_id", uuidToString(a.ID), "error", err)
		return map[string]string{}
	}
	if out == nil {
		return map[string]string{}
	}
	return out
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
