package companybraincensus

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/cerebro/access"
	"github.com/multica-ai/multica/server/internal/cerebro/connections"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/cerebro/toolpolicy"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const featureFlag = "cerebro_company_brain_migration_census"

// Handler serves an admin-gated, read-only snapshot. The router applies the
// existing manage_connections policy before requests reach this handler.
type Handler struct {
	listAgents     func(context.Context, pgtype.UUID) ([]db.Agent, error)
	listAutopilots func(context.Context, pgtype.UUID, db.Member) ([]db.ListAutopilotsRow, error)
	connections    connectionReader
	flags          featureFlagReader
	now            func() time.Time
	call           caller
	policy         connectionPolicy
}

type connectionPolicy interface {
	ConnectionToolVerdicts(context.Context, toolpolicy.TableQuery) ([]toolpolicy.ConnectionToolVerdict, error)
}

type connectionReader interface {
	List(context.Context, pgtype.UUID) ([]connections.Connection, error)
}

type featureFlagReader interface {
	ListCerebroWorkspaceFeatureFlags(context.Context, pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
}

func NewHandler(agents *db.Queries, store *connections.Store, policy connectionPolicy, flags featureFlagReader) *Handler {
	h := &Handler{
		listAgents: agents.ListAgents, connections: store, policy: policy, flags: flags, now: time.Now,
		call: func(ctx context.Context, conn connections.Connection) (json.RawMessage, error) {
			return connections.CallMCPTool(ctx, conn.URL, conn.AuthConfig, claimTool, map[string]any{})
		},
	}
	h.listAutopilots = func(ctx context.Context, workspaceID pgtype.UUID, member db.Member) ([]db.ListAutopilotsRow, error) {
		rows, err := access.ListForUser(ctx, agents, workspaceID, access.Viewer{
			UserID:  member.UserID,
			IsAdmin: member.Role == "owner" || member.Role == "admin",
		}, pgtype.Text{String: "active", Valid: true})
		if err != nil {
			return nil, err
		}
		out := make([]db.ListAutopilotsRow, 0, len(rows))
		for _, row := range rows {
			out = append(out, db.ListAutopilotsRow{
				Autopilot:     row.Autopilot,
				TriggerKinds:  row.TriggerKinds,
				NextRunAt:     row.NextRunAt,
				LastRunStatus: row.LastRunStatus,
			})
		}
		return out, nil
	}
	return h
}

func (h *Handler) resolvePolicy(ctx context.Context, query toolpolicy.TableQuery, conn connections.Connection) (policySnapshot, error) {
	if h.policy == nil {
		return policySnapshot{Whoami: accessDenied}, fmt.Errorf("connection policy unavailable")
	}
	verdicts, err := h.policy.ConnectionToolVerdicts(ctx, query)
	if err != nil {
		return policySnapshot{Whoami: accessDenied}, err
	}
	// Fail closed on a missing verdict. The policy table emits one row per
	// discovered connection tool, so an absent row means the tool was never
	// discovered for this connection and the actor's access is unknown — the
	// census must not infer Allow from silence.
	snapshot := policySnapshot{Whoami: accessDenied}
	found := false
	for _, verdict := range verdicts {
		if verdict.Connection != conn.Name {
			continue
		}
		snapshot.Tools = append(snapshot.Tools, toolAccess{
			Tool: verdict.Tool, Decision: string(verdict.Setting),
		})
		if verdict.Tool == claimTool {
			snapshot.Whoami, found = accessFromSetting(verdict.Setting), true
		}
	}
	sort.Slice(snapshot.Tools, func(i, j int) bool { return snapshot.Tools[i].Tool < snapshot.Tools[j].Tool })
	if !found {
		return snapshot, fmt.Errorf("no connection verdict for %s/%s", conn.Name, claimTool)
	}
	return snapshot, nil
}

func accessFromSetting(setting toolpolicy.Setting) actorAccess {
	switch setting {
	case toolpolicy.SettingAllow:
		return accessAllowed
	case toolpolicy.SettingAsk:
		return accessApprovalRequired
	default:
		return accessDenied
	}
}

func (h *Handler) resolveAccess(ctx context.Context, query toolpolicy.TableQuery, conn connections.Connection) (actorAccess, error) {
	snapshot, err := h.resolvePolicy(ctx, query, conn)
	return snapshot.Whoami, err
}

func (h *Handler) actorAccess(ctx context.Context, agent db.Agent, conn connections.Connection) (actorAccess, error) {
	return h.resolveAccess(ctx, toolpolicy.TableQuery{
		WorkspaceID: agent.WorkspaceID,
		RuntimeID:   agent.RuntimeID,
		AgentID:     agent.ID,
		UserID:      agent.OwnerID,
	}, conn)
}

func (h *Handler) actorPolicy(ctx context.Context, agent db.Agent, conn connections.Connection) (policySnapshot, error) {
	return h.resolvePolicy(ctx, toolpolicy.TableQuery{
		WorkspaceID: agent.WorkspaceID,
		RuntimeID:   agent.RuntimeID,
		AgentID:     agent.ID,
		UserID:      agent.OwnerID,
	}, conn)
}

func (h *Handler) automationPolicy(ctx context.Context, agent db.Agent, automation db.Autopilot, conn connections.Connection) (policySnapshot, error) {
	return h.resolvePolicy(ctx, toolpolicy.TableQuery{
		WorkspaceID: agent.WorkspaceID,
		RuntimeID:   agent.RuntimeID,
		AgentID:     agent.ID,
		UserID:      agent.OwnerID,
		SystemID:    automation.ID,
		IsSystem:    true,
	}, conn)
}

func (h *Handler) callerAccess(ctx context.Context, member db.Member, conn connections.Connection) (actorAccess, error) {
	return h.resolveAccess(ctx, toolpolicy.TableQuery{
		WorkspaceID: member.WorkspaceID,
		UserID:      member.UserID,
	}, conn)
}

func (h *Handler) enabled(ctx context.Context, workspaceID pgtype.UUID) bool {
	if h.flags == nil {
		return false
	}
	rows, err := h.flags.ListCerebroWorkspaceFeatureFlags(ctx, workspaceID)
	if err != nil {
		return false
	}
	for _, row := range rows {
		if row.FlagKey == featureFlag {
			return row.Enabled
		}
	}
	return false
}

// Get handles GET /api/workspaces/{id}/connections/company-brain-migration-census.
func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	workspaceID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, `{"error":"invalid workspace id"}`, http.StatusBadRequest)
		return
	}
	if !h.enabled(r.Context(), workspaceID) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
		return
	}
	member, ok := middleware.MemberFromContext(r.Context())
	if !ok || member.WorkspaceID != workspaceID || !member.UserID.Valid {
		http.Error(w, `{"error":"workspace member required"}`, http.StatusForbidden)
		return
	}
	agents, err := h.listAgents(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, `{"error":"list agents failed"}`, http.StatusInternalServerError)
		return
	}
	autopilots, err := h.listAutopilots(r.Context(), workspaceID, member)
	if err != nil {
		http.Error(w, `{"error":"list automations failed"}`, http.StatusInternalServerError)
		return
	}
	conns, err := h.connections.List(r.Context(), workspaceID)
	if err != nil {
		http.Error(w, `{"error":"list connections failed"}`, http.StatusInternalServerError)
		return
	}
	securedCall := func(ctx context.Context, conn connections.Connection) (json.RawMessage, error) {
		decision, err := h.callerAccess(ctx, member, conn)
		if err != nil || decision != accessAllowed {
			return nil, errCallerAccessDenied
		}
		return h.call(ctx, conn)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(Build(r.Context(), agents, autopilots, conns, securedCall, h.actorPolicy, h.automationPolicy, h.now()))
}
