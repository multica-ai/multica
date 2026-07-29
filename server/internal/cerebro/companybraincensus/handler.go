package companybraincensus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Handler serves an admin-gated, read-only snapshot. The router applies the
// existing manage_connections policy before requests reach this handler.
type Handler struct {
	agents      *db.Queries
	connections *connections.Store
	now         func() time.Time
	call        caller
	policy      connectionPolicy
}

type connectionPolicy interface {
	ConnectionToolVerdicts(context.Context, toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error)
}

func NewHandler(agents *db.Queries, store *connections.Store, policy connectionPolicy) *Handler {
	return &Handler{
		agents: agents, connections: store, policy: policy, now: time.Now,
		call: func(ctx context.Context, conn connections.Connection) (json.RawMessage, error) {
			return connections.CallMCPTool(ctx, conn.URL, conn.AuthConfig, claimTool, map[string]any{})
		},
	}
}

func (h *Handler) actorAccess(ctx context.Context, agent db.Agent, conn connections.Connection) (actorAccess, error) {
	if h.policy == nil {
		return accessDenied, fmt.Errorf("connection policy unavailable")
	}
	verdicts, err := h.policy.ConnectionToolVerdicts(ctx, toolpolicy.TableQuery{
		WorkspaceID: agent.WorkspaceID,
		RuntimeID:   agent.RuntimeID,
		AgentID:     agent.ID,
		UserID:      agent.OwnerID,
	})
	if err != nil {
		return accessDenied, err
	}
	// Fail closed on a missing verdict. The policy table emits one row per
	// discovered connection tool, so an absent row means the tool was never
	// discovered for this connection and the actor's access is unknown — the
	// census must not infer Allow from silence.
	var setting toolpolicy.Setting
	found := false
	for _, verdict := range verdicts {
		if verdict.Connection == conn.Name && verdict.Tool == claimTool {
			setting, found = verdict.Setting, true
			break
		}
	}
	if !found {
		return accessDenied, fmt.Errorf("no connection verdict for %s/%s", conn.Name, claimTool)
	}
	switch setting {
	case toolpolicy.SettingAllow:
		return accessAllowed, nil
	case toolpolicy.SettingAsk:
		return accessApprovalRequired, nil
	default:
		return accessDenied, nil
	}
}

// Get handles GET /api/workspaces/{id}/connections/company-brain-migration-census.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid workspace id"}`, http.StatusBadRequest)
		return
	}
	agents, err := h.agents.ListAgents(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, `{"error":"list agents failed"}`, http.StatusInternalServerError)
		return
	}
	conns, err := h.connections.List(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, `{"error":"list connections failed"}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Build(r.Context(), agents, conns, h.call, h.actorAccess, h.now()))
}
