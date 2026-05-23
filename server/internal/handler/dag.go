package handler

import (
	"encoding/json"
	"net/http"

	"github.com/multica-ai/multica/server/internal/dagcore"
	"github.com/multica-ai/multica/server/internal/daggraph"
	"github.com/multica-ai/multica/server/internal/service"
)

// DAGGraphAnalysisRequest is the input for on-demand graph analysis.
type DAGGraphAnalysisRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

// DAGGraphAnalysisResponse is the robot-mode deterministic output for agents.
type DAGGraphAnalysisResponse struct {
	Cycles       [][]string                 `json:"cycles"`
	Topological  []string                   `json:"topological_order,omitempty"`
	CriticalPath map[string]int             `json:"critical_path,omitempty"`
	MissingLinks []dagcore.MissingInverse   `json:"missing_inverse_links,omitempty"`
	NodeCount    int                        `json:"node_count"`
	EdgeCount    int                        `json:"edge_count"`
	Status       string                     `json:"status"` // computed | error
}

// DAGGraphAnalysis runs cycle detection, topological sort, and critical path
// over the DAG link projections for a workspace. Designed for robot-mode
// consumption: deterministic, no TUI, JSON-only.
func (h *Handler) DAGGraphAnalysis(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	// Fetch all active links for the workspace using raw DB executor
	const linkQuery = `
		SELECT from_id, to_id, type
		FROM dag_link_projection
		WHERE workspace_id = $1 AND active = TRUE
	`
	rows, err := h.DB.Query(r.Context(), linkQuery, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dag links")
		return
	}

	var edges []daggraph.Edge
	for rows.Next() {
		var fromID, toID, linkType string
		if err := rows.Scan(&fromID, &toID, &linkType); err != nil {
			rows.Close()
			writeError(w, http.StatusInternalServerError, "failed to scan dag link")
			return
		}
		edges = append(edges, daggraph.Edge{From: fromID, To: toID, Type: linkType})
	}
	rows.Close()

	// Also fetch all records (nodes without edges should still be counted)
	const recordQuery = `
		SELECT id, type
		FROM dag_record_projection
		WHERE workspace_id = $1 AND tombstoned_event_id IS NULL
	`
	recRows, err := h.DB.Query(r.Context(), recordQuery, wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load dag records")
		return
	}

	var nodes []daggraph.Node
	for recRows.Next() {
		var id, typ string
		if err := recRows.Scan(&id, &typ); err != nil {
			recRows.Close()
			writeError(w, http.StatusInternalServerError, "failed to scan dag record")
			return
		}
		nodes = append(nodes, daggraph.Node{ID: id, Type: typ})
	}
	recRows.Close()

	g := daggraph.NewGraphWithNodes(edges, nodes)
	resp := DAGGraphAnalysisResponse{
		NodeCount: g.NodesCount(),
		EdgeCount: len(edges),
		Status:    "computed",
		Cycles:    make([][]string, 0),
	}
	resp.Cycles = g.Cycles()
	if resp.Cycles == nil {
		resp.Cycles = make([][]string, 0)
	}

	// Topological sort (fails if cycles exist)
	order, err := g.TopologicalSort()
	if err == nil {
		resp.Topological = order
		resp.CriticalPath = g.CriticalPath()
	}

	// Inverse link validation
	// For multica, blocks/blocked_by is the primary dependency pair
	inverseTypes := map[string]string{"blocks": "blocked_by", "blocked_by": "blocks"}
	resp.MissingLinks = dagcore.MissingInverseLinks(edgesToLinks(edges), inverseTypes)

	writeJSON(w, http.StatusOK, resp)
}

// DAGEventAppend accepts a DAG core event and appends it to the log.
func (h *Handler) DAGEventAppend(w http.ResponseWriter, r *http.Request) {
	wsID := h.resolveWorkspaceID(r)
	wsUUID, ok := parseUUIDOrBadRequest(w, wsID, "workspace_id")
	if !ok {
		return
	}

	var req struct {
		Event dagcore.Event `json:"event"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}

	dagSvc := service.NewDAGService(h.Queries, h.TxStarter)
	evt, err := dagSvc.AppendEvent(r.Context(), wsUUID, req.Event)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"event_id": evt.ID,
		"status":   "appended",
	})
}

// edgesToLinks converts daggraph.Edge to dagcore.Link for validation.
func edgesToLinks(edges []daggraph.Edge) []dagcore.Link {
	links := make([]dagcore.Link, len(edges))
	for i, e := range edges {
		links[i] = dagcore.Link{FromID: e.From, ToID: e.To, Type: e.Type}
	}
	return links
}
