package handler

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// GET /api/se/health — infrastructure health snapshot (SE-37641).
//
// The payload is produced outside the backend: a host-side cron on the infra
// host queries Prometheus (targets, node metrics, 24h history) and uploads a
// JSON snapshot into the backend uploads volume. The handler serves that file
// to workspace owners/admins; when the snapshot is missing or the path is not
// configured it degrades to a clean 503 payload instead of an error page.
//
// Snapshot path resolution: MULTICA_HEALTH_SNAPSHOT_PATH env override, else
// the uploads volume default (docker-compose.selfhost.yml mounts
// backend_uploads:/app/data/uploads).

const seHealthSnapshotDefaultPath = "/app/data/uploads/infra-health-snapshot.json"

const seHealthSnapshotMaxBytes = 2 << 20 // 2 MiB

type SEInfraHealthTarget struct {
	Job      string `json:"job"`
	Instance string `json:"instance"`
	Health   string `json:"health"`
}

type SEInfraNodeNow struct {
	Load1       float64 `json:"load1"`
	MemAvailGB  float64 `json:"mem_avail_gb"`
	MemTotalGB  float64 `json:"mem_total_gb"`
	SwapUsedGB  float64 `json:"swap_used_gb"`
	SwapTotalGB float64 `json:"swap_total_gb"`
}

type SEInfraSync struct {
	OK          bool   `json:"ok"`
	LastSuccess string `json:"last_success,omitempty"`
	Streak      int    `json:"streak,omitempty"`
}

type SEInfraHealthSnapshot struct {
	GeneratedAt string                    `json:"generated_at"`
	Overall     string                    `json:"overall"`
	Targets     []SEInfraHealthTarget     `json:"targets"`
	Nodes       map[string]SEInfraNodeNow `json:"nodes"`
	History     map[string][][]float64    `json:"history"`
	Syncs       map[string]SEInfraSync    `json:"syncs,omitempty"`
}

// SE-37663: Agent Fleet health. Computed live from agent_runtime and
// agent_task_queue (read-only aggregates) and merged into the infra health
// response under "agent_fleet". Failure classes follow the agent_error.*
// namespace already written by the task pipeline into failure_reason, so no
// log parsing is involved. Any query error degrades to status "unavailable"
// instead of failing the whole endpoint.

type SEFleetRuntime struct {
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	Status     string `json:"status"`
	LastSeenAt string `json:"last_seen_at,omitempty"`
}

type SEFleetAgentFailure struct {
	AgentID          string `json:"agent_id"`
	AgentName        string `json:"agent_name"`
	Completed        int64  `json:"completed"`
	Failed           int64  `json:"failed"`
	QuotaFailed      int64  `json:"quota_failed"`
	AuthFailed       int64  `json:"auth_failed"`
	FirstFailureAt   string `json:"first_failure_at,omitempty"`
	LastFailureAt    string `json:"last_failure_at,omitempty"`
	LastErrorExcerpt string `json:"last_error_excerpt,omitempty"`
}

type SEAgentFleet struct {
	Status            string                `json:"status"`
	QuotaStreakActive bool                  `json:"quota_streak_active"`
	AuthStreakActive  bool                  `json:"auth_streak_active"`
	Runtimes          []SEFleetRuntime      `json:"runtimes"`
	FailingAgents     []SEFleetAgentFailure `json:"failing_agents"`
}

// seInfraHealthResponse embeds the file snapshot so its fields stay at the
// top level (backwards compatible) and adds the live fleet section.
type seInfraHealthResponse struct {
	SEInfraHealthSnapshot
	AgentFleet *SEAgentFleet `json:"agent_fleet,omitempty"`
}

func seFleetTimeString(ts pgtype.Timestamptz) string {
	if !ts.Valid {
		return ""
	}
	return ts.Time.UTC().Format(time.RFC3339)
}

func seFleetUUIDString(id pgtype.UUID) string {
	if !id.Valid {
		return ""
	}
	return uuid.UUID(id.Bytes).String()
}

func (h *Handler) buildSEAgentFleet(ctx context.Context, workspaceID pgtype.UUID) *SEAgentFleet {
	fleet := &SEAgentFleet{
		Status:        "ok",
		Runtimes:      []SEFleetRuntime{},
		FailingAgents: []SEFleetAgentFailure{},
	}

	runtimes, err := h.Queries.ListFleetRuntimes(ctx, workspaceID)
	if err != nil {
		fleet.Status = "unavailable"
		return fleet
	}
	for _, r := range runtimes {
		fleet.Runtimes = append(fleet.Runtimes, SEFleetRuntime{
			Name:       r.Name,
			Provider:   r.Provider,
			Status:     r.Status,
			LastSeenAt: seFleetTimeString(r.LastSeenAt),
		})
	}

	stats, err := h.Queries.GetFleetAgentRunStats(ctx, workspaceID)
	if err != nil {
		fleet.Status = "unavailable"
		return fleet
	}
	lastFailures, err := h.Queries.GetFleetAgentLastFailures(ctx, workspaceID)
	if err != nil {
		fleet.Status = "unavailable"
		return fleet
	}
	lastErrorByAgent := make(map[string]string, len(lastFailures))
	for _, f := range lastFailures {
		lastErrorByAgent[seFleetUUIDString(f.AgentID)] = f.ErrorExcerpt
	}

	for _, s := range stats {
		agentID := seFleetUUIDString(s.AgentID)
		fleet.FailingAgents = append(fleet.FailingAgents, SEFleetAgentFailure{
			AgentID:          agentID,
			AgentName:        s.AgentName,
			Completed:        s.Completed,
			Failed:           s.Failed,
			QuotaFailed:      s.QuotaFailed,
			AuthFailed:       s.AuthFailed,
			FirstFailureAt:   s.FirstFailureAt,
			LastFailureAt:    s.LastFailureAt,
			LastErrorExcerpt: lastErrorByAgent[agentID],
		})
	}

	streaks, err := h.Queries.GetFleetFailureStreaks(ctx, workspaceID)
	if err != nil {
		fleet.Status = "unavailable"
		return fleet
	}
	fleet.QuotaStreakActive = streaks.QuotaStreakActive
	fleet.AuthStreakActive = streaks.AuthStreakActive

	if fleet.QuotaStreakActive || fleet.AuthStreakActive || len(fleet.FailingAgents) > 0 {
		fleet.Status = "warn"
	}
	return fleet
}

// GET /api/se/health
func (h *Handler) GetSEInfraHealth(w http.ResponseWriter, r *http.Request) {
	workspaceID := h.resolveWorkspaceID(r)

	if _, ok := h.requireWorkspaceRole(w, r, workspaceID, "workspace not found", "owner", "admin"); !ok {
		return
	}

	wsUUID, ok := parseUUIDOrBadRequest(w, workspaceID, "workspace_id")
	if !ok {
		return
	}

	path := os.Getenv("MULTICA_HEALTH_SNAPSHOT_PATH")
	if path == "" {
		path = seHealthSnapshotDefaultPath
	}

	f, err := os.Open(path)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":  "unavailable",
			"message": "infra health snapshot is not available",
		})
		return
	}
	defer f.Close()

	dec := json.NewDecoder(io.LimitReader(f, seHealthSnapshotMaxBytes))
	var snap SEInfraHealthSnapshot
	if err := dec.Decode(&snap); err != nil {
		writeError(w, http.StatusBadGateway, "infra health snapshot is malformed")
		return
	}

	writeJSON(w, http.StatusOK, seInfraHealthResponse{
		SEInfraHealthSnapshot: snap,
		AgentFleet:            h.buildSEAgentFleet(r.Context(), wsUUID),
	})
}
