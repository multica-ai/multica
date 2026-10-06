package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) worktreeReadinessRuntime(w http.ResponseWriter, r *http.Request) (db.AgentRuntime, bool) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return rt, false
	}
	if !rt.DaemonID.Valid || rt.DaemonID.String == "" {
		writeError(w, http.StatusBadRequest, "runtime is not bound to a daemon")
		return rt, false
	}
	if id := middleware.DaemonIDFromContext(r.Context()); id != "" && id != rt.DaemonID.String {
		writeError(w, http.StatusForbidden, "runtime belongs to another daemon")
		return rt, false
	}
	if middleware.DaemonIDFromContext(r.Context()) == "" && (!rt.OwnerID.Valid || requestUserID(r) != uuidToString(rt.OwnerID)) {
		writeError(w, http.StatusForbidden, "only the runtime owner may report machine readiness")
		return rt, false
	}
	return rt, true
}

func (h *Handler) ListDaemonWorktreeResources(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.worktreeReadinessRuntime(w, r)
	if !ok {
		return
	}
	rows, err := h.Queries.ListWorktreeResourcesForDaemon(r.Context(), db.ListWorktreeResourcesForDaemonParams{WorkspaceID: rt.WorkspaceID, DaemonID: rt.DaemonID.String})
	if err != nil {
		writeError(w, 500, "failed to load worktree resources")
		return
	}
	resources := make([]protocol.WorktreeReadinessResource, 0, len(rows))
	for _, row := range rows {
		resources = append(resources, protocol.WorktreeReadinessResource{ID: uuidToString(row.ID), ResourceRef: row.ResourceRef})
	}
	writeJSON(w, 200, map[string]any{"resources": resources})
}

func (h *Handler) ReportDaemonWorktreeReadiness(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.worktreeReadinessRuntime(w, r)
	if !ok {
		return
	}
	var report protocol.WorktreeReadinessReport
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32768)).Decode(&report); err != nil {
		writeError(w, 400, "invalid readiness report")
		return
	}
	id, ok := parseUUIDOrBadRequest(w, report.ID, "resource_id")
	if !ok {
		return
	}
	m := report.Measurement
	if m.FileCount < 0 || m.TotalBytes < 0 || m.SymlinkCount < 0 || len(m.LargestPaths) > 3 || (m.Status != "ready" && m.Status != "blocked" && m.Status != "unavailable") {
		writeError(w, 400, "invalid readiness measurement")
		return
	}
	// Never trust a daemon's ready verdict when its measured payload exceeds the
	// server's replay limits. Only the owning daemon can submit measurements.
	m.MaxFiles = protocol.WorktreeReplayMaxFiles
	m.MaxBytes = protocol.WorktreeReplayMaxBytes
	m.CheckedAt = ""
	m.ExpiresAt = ""
	if m.Status != "unavailable" {
		m.Status = "ready"
		if m.FileCount > m.MaxFiles || m.TotalBytes > m.MaxBytes || m.SymlinkCount > 0 {
			m.Status = "blocked"
		}
	}
	m.Message = ""
	data, _ := json.Marshal(m)
	n, err := h.Queries.RecordWorktreeReadiness(r.Context(), db.RecordWorktreeReadinessParams{ResourceID: id, WorkspaceID: rt.WorkspaceID, DaemonID: rt.DaemonID.String, ResourceRef: report.ResourceRef, Status: m.Status, Measurement: data})
	if err != nil {
		writeError(w, 500, "failed to record readiness")
		return
	}
	if n == 0 {
		writeError(w, 409, "resource changed or is not owned by this daemon")
		return
	}
	if h.TaskService != nil {
		ids, err := h.Queries.ListWorktreeReadinessRuntimeIDs(r.Context(), db.ListWorktreeReadinessRuntimeIDsParams{WorkspaceID: rt.WorkspaceID, DaemonID: rt.DaemonID.String})
		if err == nil {
			for _, id := range ids {
				h.TaskService.NotifyWorktreeReadinessChanged(id)
			}
		}
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}

func resourceUsesWorktree(r db.ProjectResource) bool {
	var ref localDirectoryRef
	return r.ResourceType == "local_directory" && json.Unmarshal(r.ResourceRef, &ref) == nil && ref.ExecutionMode == localDirectoryModeWorktree
}

func readinessResponse(row *db.LocalWorktreeReadiness, now time.Time) *protocol.WorktreeReadiness {
	m := protocol.WorktreeReadiness{Status: "checking", ReasonCode: "awaiting_measurement", MaxFiles: protocol.WorktreeReplayMaxFiles, MaxBytes: protocol.WorktreeReplayMaxBytes, LargestPaths: []protocol.WorktreeReadinessPath{}, Message: "Waiting for this machine to inspect the source. Keep its runtime online and up to date."}
	if row == nil {
		return &m
	}
	_ = json.Unmarshal(row.Measurement, &m)
	m.Status = row.Status
	m.UsageUnavailable = row.Status == "unavailable"
	m.ReasonCode = ""
	m.CheckedAt = row.CheckedAt.Time.UTC().Format(time.RFC3339)
	m.ExpiresAt = row.ExpiresAt.Time.UTC().Format(time.RFC3339)
	if !row.ExpiresAt.Valid || !row.ExpiresAt.Time.After(now) {
		m.Status = "unavailable"
		m.ReasonCode = "stale_measurement"
	}
	switch m.Status {
	case "ready":
		m.Message = ""
	case "blocked":
		m.ReasonCode = "untracked_limit"
		if m.FileCount <= m.MaxFiles && m.TotalBytes <= m.MaxBytes {
			m.ReasonCode = "untracked_symlinks"
		}
		m.Message = "Ignore or remove the offending files; queued tasks resume automatically. Untracked symlinks cannot be replayed."
	default:
		if m.ReasonCode == "" {
			m.ReasonCode = "inspection_failed"
		}
		m.Status = "unavailable"
		m.Message = "No fresh source measurement is available. Keep this machine's runtime online and up to date; queued tasks will wait."
	}
	if m.LargestPaths == nil {
		m.LargestPaths = []protocol.WorktreeReadinessPath{}
	}
	return &m
}

func daemonReadinessAvailable(runtimes []db.AgentRuntime, daemonID string, now time.Time) bool {
	var newest *db.AgentRuntime
	for i := range runtimes {
		rt := &runtimes[i]
		if rt.DaemonID.String == daemonID && (newest == nil || runtimeSeenAfter(rt, newest)) {
			newest = rt
		}
	}
	return newest != nil && newest.Status == "online" && newest.LastSeenAt.Valid && newest.LastSeenAt.Time.After(now.Add(-90*time.Second)) && runtimeHasCapability(newest.Metadata, protocol.DaemonCapabilityWorktreeReadinessV1)
}
