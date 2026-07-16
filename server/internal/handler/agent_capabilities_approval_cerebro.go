package handler

// CEREBRO-PATCH(agent-capabilities-approval-handler): FIR-3212 Approval slice —
// what approving a pending proposal actually does to the agent.
//
// GET /api/agents/{id}/capabilities/approval-impact?fields=instructions,model
//
// The change-request queue renders a field diff, and that is where the approver's
// information runs out. The diff proves `instructions` changed; it cannot say
// that this agent runs on an engine that discards the system prompt, so the
// approval is a no-op. The vocabulary needed to answer that (silent vs logged
// drop, native vs prepended prompt) belongs next to the matrix it is tested
// against — so the answer is computed in internal/cerebro/capabilities and only
// rendered in the browser.
//
// Why the CHANGED FIELDS are an input rather than derived here: the queue already
// computes them to draw the diff, from snapshots it already holds. Re-deriving
// them server-side would mean loading both snapshots and re-implementing the
// frontend's normalisation, and the two copies would disagree the first time
// either changed. The server owns "what does this field mean on this engine?" —
// which is knowledge the frontend cannot have. It does not need to own "which
// fields differ", which is a diff the caller is already looking at.
//
// This is a pure read of a static matrix: no snapshot is fetched and nothing is
// mutated, so an unknown or bogus field name is answered, not rejected — the
// classifier reports it as unknown_field rather than 400ing a panel that is only
// trying to explain a proposal.

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	cerebrocapabilities "github.com/multica-ai/multica/server/internal/cerebro/capabilities"
)

// approvalFieldLimit caps how many field names one request may ask about. The
// whole snapshot is 10 keys; the cap exists so a caller-supplied query string
// cannot be echoed back as an arbitrarily long response.
const approvalFieldLimit = 64

// AgentCapabilityApproval is the approval-consequences card: the engine the
// agent runs on, plus what each changed field will do on it.
type AgentCapabilityApproval struct {
	AgentID string `json:"agent_id"`
	// Runtime is the same shape the capabilities card publishes, so the panel can
	// name the engine (and its CLI version) the consequences were computed
	// against instead of asserting them from nowhere.
	Runtime AgentCapabilityRuntimeOptions      `json:"runtime"`
	Impact  cerebrocapabilities.ApprovalImpact `json:"impact"`
}

// buildAgentCapabilityApproval is the pure core (unit-tested without a DB).
func buildAgentCapabilityApproval(agentID string, runtime AgentCapabilityRuntimeOptions, changedFields []string) AgentCapabilityApproval {
	return AgentCapabilityApproval{
		AgentID: agentID,
		Runtime: runtime,
		Impact:  cerebrocapabilities.ApprovalImpactFor(runtime.Provider, changedFields),
	}
}

// parseApprovalFields splits the comma-separated `fields` query param, trimming
// blanks and capping the count. nil when nothing usable was sent — the impact for
// an empty change list is legitimately "nothing changes", not an error.
func parseApprovalFields(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		field := strings.TrimSpace(part)
		if field == "" {
			continue
		}
		out = append(out, field)
		if len(out) == approvalFieldLimit {
			break
		}
	}
	return out
}

// GetAgentCapabilityApprovalImpact handles
// GET /api/agents/{id}/capabilities/approval-impact?fields=a,b.
//
// Access control is delegated to loadAgentForUser — the same gate as the
// capabilities card and the swap preview.
func (h *Handler) GetAgentCapabilityApprovalImpact(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}

	writeJSON(w, http.StatusOK, buildAgentCapabilityApproval(
		uuidToString(agent.ID),
		h.buildRuntimeExecOptions(r, agent),
		parseApprovalFields(r.URL.Query().Get("fields")),
	))
}
