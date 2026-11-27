package handler

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	// trajectoryGraphReadTimeout bounds every statement of one graph snapshot.
	trajectoryGraphReadTimeout = 10 * time.Second
	// trajectoryGraphRecordBudget bounds the encoded record nodes (runs,
	// activities, comments, attachments) of one response. Records past it are
	// dropped and the graph is marked truncated.
	trajectoryGraphRecordBudget = 6 << 20
	// trajectoryGraphMaxBytes is the hard ceiling of one encoded response.
	trajectoryGraphMaxBytes = 8 << 20
)

func (h *Handler) ListTaskTrajectory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private,no-store")
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if !h.requireTrajectory(w, r) {
		return
	}
	var body struct {
		IssueIDs []string `json:"issue_ids"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10))
	if err := decoder.Decode(&body); err != nil {
		if !writeQueryCancellation(w, r, err) {
			writeError(w, 400, "issue_ids must contain at most 200 UUIDs")
		}
		return
	}
	if decoder.Decode(&struct{}{}) != io.EOF || len(body.IssueIDs) > 200 {
		writeError(w, 400, "issue_ids must contain at most 200 UUIDs")
		return
	}
	ids := []pgtype.UUID{}
	seen := map[pgtype.UUID]bool{}
	for _, value := range body.IssueIDs {
		id, ok := parseUUIDOrBadRequest(w, value, "issue_ids")
		if !ok {
			return
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	h.readTrajectoryGraph(w, r, ids, "", true, false)
}

func (h *Handler) GetTrajectory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "private,no-store")
	if !h.requireTrajectory(w, r) {
		return
	}
	scope := r.URL.Query().Get("scope")
	if scope != "" && scope != "issue" {
		writeError(w, 400, "scope must be issue or omitted")
		return
	}
	h.readTrajectoryGraph(w, r, nil, chi.URLParam(r, "id"), scope != "issue", true)
}

func trajectoryGraphReadError(w http.ResponseWriter, r *http.Request, err error, message string) {
	if errors.Is(r.Context().Err(), context.DeadlineExceeded) {
		writeError(w, http.StatusGatewayTimeout, "trajectory read timed out")
		return
	}
	if !writeQueryCancellation(w, r, err) {
		writeError(w, http.StatusInternalServerError, message)
	}
}

func (h *Handler) readTrajectoryGraph(w http.ResponseWriter, r *http.Request, ids []pgtype.UUID, root string, related, records bool) {
	if _, ok := requireUserID(w, r); !ok {
		return
	}
	if h.TxStarter == nil {
		writeError(w, 500, "failed to read trajectory")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), trajectoryGraphReadTimeout)
	defer cancel()
	r = r.WithContext(ctx)
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		trajectoryGraphReadError(w, r, err, "failed to read trajectory")
		return
	}
	defer func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Second)
		defer cancel()
		_ = tx.Rollback(ctx)
	}()
	if _, err = tx.Exec(r.Context(), "SET TRANSACTION ISOLATION LEVEL REPEATABLE READ, READ ONLY"); err != nil {
		trajectoryGraphReadError(w, r, err, "failed to open trajectory snapshot")
		return
	}
	snapshot := *h
	snapshot.Queries = db.New(tx)
	snapshot.DB = tx
	workspace := snapshot.resolveWorkspaceID(r)
	member, ok := snapshot.requireWorkspaceMember(w, r, workspace, "workspace not found")
	if !ok {
		return
	}
	workspaceID, ok := parseUUIDOrBadRequest(w, workspace, "workspace_id")
	if !ok {
		return
	}
	if records {
		issue, ok := snapshot.loadIssueForUser(w, r, root)
		if !ok {
			return
		}
		ids = []pgtype.UUID{issue.ID}
		root = uuidToString(issue.ID)
	}
	hidden, ok := snapshot.requestHiddenProjects(w, r, workspace)
	if !ok {
		return
	}
	hiddenIDs := hidden.Param()
	if hiddenIDs == nil {
		hiddenIDs = []pgtype.UUID{}
	}
	nodes, err := readTrajectoryGraphNodes(r.Context(), tx, trajectoryGraphIssueSQL, workspaceID, ids, hiddenIDs)
	if err != nil {
		trajectoryGraphReadError(w, r, err, "failed to read trajectory issues")
		return
	}
	if len(nodes) != len(ids) {
		writeError(w, 404, "one or more issues are unavailable in this workspace")
		return
	}
	truncated := false
	if related {
		ids, truncated, err = expandTrajectoryGraph(r.Context(), tx, workspaceID, ids, hiddenIDs)
		if err != nil {
			trajectoryGraphReadError(w, r, err, "failed to read native relationships")
			return
		}
		nodes, err = readTrajectoryGraphNodes(r.Context(), tx, trajectoryGraphIssueSQL, workspaceID, ids, hiddenIDs)
		if err != nil {
			trajectoryGraphReadError(w, r, err, "failed to read related issues")
			return
		}
	}
	if records {
		actorType, _ := snapshot.resolveActor(r, requestUserID(r), workspace)
		allNames := actorType == "agent" || roleAllowed(member.Role, "owner", "admin")
		for _, source := range []struct {
			sql   string
			limit int
		}{
			{trajectoryGraphRunsSQL, 1000}, {trajectoryGraphActivitiesSQL, 2000},
			{trajectoryGraphCommentsSQL, 2000}, {trajectoryGraphAttachmentsSQL, 2000},
		} {
			args := []any{workspaceID, ids, hiddenIDs}
			if source.sql == trajectoryGraphRunsSQL {
				args = append(args, member.UserID, allNames)
			}
			extra, err := readTrajectoryGraphNodes(r.Context(), tx, source.sql, args...)
			if err != nil {
				trajectoryGraphReadError(w, r, err, "failed to read native trajectory records")
				return
			}
			if len(extra) > source.limit {
				extra = extra[:source.limit]
				truncated = true
			}
			nodes = append(nodes, extra...)
		}
	}
	nodes, trimmed := trimTrajectoryGraphRecords(nodes, trajectoryGraphRecordBudget)
	graph := projectTrajectoryGraph(root, nodes, truncated || trimmed)
	rows, err := tx.Query(r.Context(), trajectoryGraphNativeEdgesSQL, workspaceID, ids, hiddenIDs)
	if err != nil {
		trajectoryGraphReadError(w, r, err, "failed to read platform relationships")
		return
	}
	native := []trajectoryGraphEdge{}
	for rows.Next() {
		var raw []byte
		if err = rows.Scan(&raw); err != nil {
			break
		}
		var edge trajectoryGraphEdge
		if err = json.Unmarshal(raw, &edge); err != nil {
			break
		}
		native = append(native, edge)
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		trajectoryGraphReadError(w, r, err, "failed to read platform relationships")
		return
	}
	if len(native) > 2000 {
		native = native[:2000]
		graph.Coverage.Truncated = true
		graph.Coverage.Complete = false
		graph.Coverage.Warnings = append(graph.Coverage.Warnings, "Platform relationship limit reached (2000).")
	}
	graph.Edges = append(graph.Edges, native...)
	if err = tx.Commit(r.Context()); err != nil {
		trajectoryGraphReadError(w, r, err, "failed to finish trajectory read")
		return
	}
	if r.Context().Err() != nil {
		trajectoryGraphReadError(w, r, r.Context().Err(), "failed to finish trajectory read")
		return
	}
	body, err := json.Marshal(graph)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode trajectory")
		return
	}
	if len(body) > trajectoryGraphMaxBytes {
		writeError(w, http.StatusRequestEntityTooLarge, "trajectory is too large; narrow the filters")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

// trimTrajectoryGraphRecords keeps every issue node and the record nodes that
// fit in budget, in read order. It reports whether any record was dropped.
func trimTrajectoryGraphRecords(nodes []trajectoryGraphNode, budget int) ([]trajectoryGraphNode, bool) {
	kept := make([]trajectoryGraphNode, 0, len(nodes))
	used := 0
	trimmed := false
	for _, node := range nodes {
		if node.Kind == "issue" {
			kept = append(kept, node)
			continue
		}
		if trimmed {
			continue
		}
		encoded, err := json.Marshal(node)
		if err != nil || used+len(encoded) > budget {
			trimmed = true
			continue
		}
		used += len(encoded)
		kept = append(kept, node)
	}
	return kept, trimmed
}
