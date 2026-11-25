package handler

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/rundiff"
)

// Stable machine-readable codes for the relay outcomes (). The UI maps
// them onto its three display states; anything unrecognized degrades to the
// generic failure branch.
const (
	fileChangeCodeRuntimeOffline = "runtime_offline"
	fileChangeCodeRuntimeTimeout = "runtime_timeout"
	fileChangeCodeRecordPurged   = "record_purged"
	fileChangeCodeRelayBusy      = "relay_busy"
)

// writeFileChangeError answers a relay failure. Relay outcomes are per-request
// truth — a purged record stays purged, an offline runtime recovers within
// seconds — so the answer must never sit in an HTTP cache: a cached 410 would
// keep reporting "purged" after the content returns, and a cached 503 would
// keep reporting "offline" after the runtime is back.
func writeFileChangeError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Cache-Control", "no-store")
	writeErrorCode(w, status, code, msg)
}

func (h *Handler) SaveRunFileChanges(w http.ResponseWriter, r *http.Request) {
	task, ws, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}
	var c rundiff.Collection
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, rundiff.MaxUploadBytes))
	if err := decoder.Decode(&c); err != nil {
		writeError(w, 400, "invalid file changes payload")
		return
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		writeError(w, 400, "invalid trailing payload")
		return
	}
	if err := c.ValidateAndSanitize(); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	id, ok := parseUUIDOrBadRequest(w, c.CollectionID, "collection_id")
	if !ok {
		return
	}
	// git-root bodies are never stored — the daemon serves them on
	// demand from its pinned trees. ValidateAndSanitize already dropped them;
	// only directory-root files (no object database on the runtime) keep the
	// inline-body path. Everything an older daemon still uploads lands here
	// and is discarded.
	bodies := map[string]rundiff.File{}
	for i := range c.Files {
		f := &c.Files[i]
		if f.Before != nil || f.After != nil {
			bodies[f.ID] = *f
		}
	}
	manifest, err := json.Marshal(c)
	if err != nil {
		writeError(w, 500, "encode manifest failed")
		return
	}
	body, err := json.Marshal(bodies)
	if err != nil {
		writeError(w, 500, "encode bodies failed")
		return
	}
	// Authorization preceded the request body and may now be stale. Join the
	// workspace deletion lock protocol only after parsing, so a slow upload
	// cannot hold teardown open. The shared lock must survive the INSERT and
	// COMMIT; an unlocked existence check would leave the late-write race open.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "persist file changes failed")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)
	wsID := parseUUID(ws)
	if _, err := qtx.LockWorkspaceForRunFileChanges(r.Context(), wsID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "task not found")
		} else {
			writeError(w, 500, "persist file changes failed")
		}
		return
	}
	if _, err := qtx.GetAgentTaskInWorkspace(r.Context(), db.GetAgentTaskInWorkspaceParams{ID: task.ID, WorkspaceID: wsID}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "task not found")
		} else {
			writeError(w, 500, "persist file changes failed")
		}
		return
	}
	rows, err := qtx.SaveRunFileChanges(r.Context(), db.SaveRunFileChangesParams{WorkspaceID: wsID, TaskID: task.ID, CollectionID: id, Manifest: manifest, Bodies: body})
	if err != nil {
		writeError(w, 500, "persist file changes failed")
		return
	}
	if rows == 0 {
		writeError(w, 409, "collection is immutable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "persist file changes failed")
		return
	}
	writeJSON(w, 200, map[string]bool{"ok": true})
}
func (h *Handler) loadRunFileChanges(w http.ResponseWriter, r *http.Request) (rundiff.Collection, db.AgentTaskQueue, pgtype.UUID, bool) {
	empty := rundiff.Empty("", "unavailable", "legacy_no_baseline")
		taskID, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "taskId"), "task_id")
	if !ok {
		return empty, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	task, err := h.Queries.GetAgentTask(r.Context(), taskID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 404, "task not found")
		} else {
			writeError(w, 500, "load task failed")
		}
		return empty, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	ws := h.TaskService.ResolveTaskWorkspaceID(r.Context(), task)
	if ws == "" || ws != middleware.WorkspaceIDFromContext(r.Context()) {
		writeError(w, 404, "task not found")
		return empty, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	wsID := parseUUID(ws)
	if !h.requireTaskVisible(w, r, ws, task, http.StatusNotFound, "task not found") {
		return empty, db.AgentTaskQueue{}, pgtype.UUID{}, false
	}
	raw, err := h.Queries.GetRunFileChanges(r.Context(), db.GetRunFileChangesParams{WorkspaceID: wsID, TaskID: taskID})
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, task, wsID, true
	}
	if err != nil {
		writeError(w, 500, "load file changes failed")
		return empty, task, wsID, false
	}
	var c rundiff.Collection
	if err := json.Unmarshal(raw, &c); err != nil {
		writeError(w, 500, "invalid stored manifest")
		return empty, task, wsID, false
	}
	if c.Status == "collecting" {
		switch task.Status {
		case "completed", "failed", "cancelled":
			c.Status = "unavailable"
			c.Reason = "interrupted"
			c.TotalsComplete = false
		}
	}
	return c, task, wsID, true
}
func (h *Handler) ListRunFileChanges(w http.ResponseWriter, r *http.Request) {
	c, _, _, ok := h.loadRunFileChanges(w, r)
	if !ok {
		return
	}
	offset, limit := 0, 100
	if s := r.URL.Query().Get("cursor"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 0 || n > rundiff.MaxFiles {
			writeError(w, 400, "invalid cursor")
			return
		}
		offset = n
	}
	if s := r.URL.Query().Get("limit"); s != "" {
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 100 {
			writeError(w, 400, "invalid limit")
			return
		}
		limit = n
	}
	writeJSON(w, 200, c.Page(offset, limit))
}

// GetRunFileChange serves one file's body. Three paths ():
//
//   - body_status outside {available, redacted}: metadata only, as before.
//   - directory roots keep their inline bodies in the database.
//   - git roots relay to the runtime's daemon: queue a request, hint the WS,
//     wait up to fileChangeRelayWait for the daemon's POST.
//
// Outcomes: 200 body, 503 runtime_offline, 504 runtime_timeout, 410
// record_purged, 429 relay_busy. Every attempt logs one structured line with
// user / task / file / result / elapsed and never the content.
func (h *Handler) GetRunFileChange(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	c, task, ws, ok := h.loadRunFileChanges(w, r)
	if !ok {
		return
	}
	id := chi.URLParam(r, "fileId")
	var file *rundiff.File
	for i := range c.Files {
		if c.Files[i].ID == id {
			file = &c.Files[i]
			break
		}
	}
	if file == nil {
		h.logFileChangeRelay(r, task.ID, id, "not_found", start)
		writeError(w, 404, "file change not found")
		return
	}
	if file.BodyStatus != "available" && file.BodyStatus != "redacted" {
		h.logFileChangeRelay(r, task.ID, id, "metadata", start)
		writeJSON(w, 200, file)
		return
	}
	if h.fileChangeRootKind(&c, file.RootID) != "git" {
		h.serveStoredFileChangeBody(w, r, task, ws, file, start)
		return
	}
	h.relayFileChangeBody(w, r, task, file, start)
}

func (h *Handler) fileChangeRootKind(c *rundiff.Collection, rootID string) string {
	for i := range c.Roots {
		if c.Roots[i].ID == rootID {
			return c.Roots[i].Kind
		}
	}
	return ""
}

// serveStoredFileChangeBody returns a directory-root body from the database.
// After the bodies-clearing migration the row is gone; that is a purged
// record, never "no changes".
func (h *Handler) serveStoredFileChangeBody(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue, ws pgtype.UUID, file *rundiff.File, start time.Time) {
	body, err := h.Queries.GetRunFileChangeBody(r.Context(), db.GetRunFileChangeBodyParams{WorkspaceID: ws, TaskID: task.ID, FileID: file.ID})
	if err != nil || len(body) == 0 {
		h.logFileChangeRelay(r, task.ID, file.ID, "purged", start)
		writeFileChangeError(w, http.StatusGone, fileChangeCodeRecordPurged, "run file change body is purged")
		return
	}
	var f rundiff.File
	if err := json.Unmarshal(body, &f); err != nil {
		h.logFileChangeRelay(r, task.ID, file.ID, "error", start)
		writeError(w, 500, "invalid stored body")
		return
	}
	h.logFileChangeRelay(r, task.ID, file.ID, "ok", start)
	writeJSON(w, 200, f)
}

// relayFileChangeBody queues a fetch order for the runtime's daemon and waits
// for its response.
func (h *Handler) relayFileChangeBody(w http.ResponseWriter, r *http.Request, task db.AgentTaskQueue, file *rundiff.File, start time.Time) {
	result := func(code string) string {
		if code == "" {
			return "ok"
		}
		return code
	}
	// Per-instance in-flight cap: a full relay buffer answers 429 before any
	// daemon round trip is paid.
	select {
	case h.fileChangeRelaySlots <- struct{}{}:
		defer func() { <-h.fileChangeRelaySlots }()
	default:
		w.Header().Set("Retry-After", "3")
		h.logFileChangeRelay(r, task.ID, file.ID, fileChangeCodeRelayBusy, start)
		writeFileChangeError(w, http.StatusTooManyRequests, fileChangeCodeRelayBusy, "too many file change relays in flight")
		return
	}

	if h.FileChangeRelayStore == nil {
		h.logFileChangeRelay(r, task.ID, file.ID, "error", start)
		writeError(w, 500, "file change relay unavailable")
		return
	}
	runtimeID := uuidToString(task.RuntimeID)
	// The runtime row is the cluster-safe liveness signal (heartbeats keep
	// status fresh); a missing or offline runtime can never serve the body.
	if runtimeID == "" {
		h.logFileChangeRelay(r, task.ID, file.ID, fileChangeCodeRuntimeOffline, start)
		writeFileChangeError(w, http.StatusServiceUnavailable, fileChangeCodeRuntimeOffline, "runtime is offline")
		return
	}
	rt, err := h.Queries.GetAgentRuntime(r.Context(), parseUUID(runtimeID))
	if err != nil || rt.Status != "online" {
		h.logFileChangeRelay(r, task.ID, file.ID, fileChangeCodeRuntimeOffline, start)
		writeFileChangeError(w, http.StatusServiceUnavailable, fileChangeCodeRuntimeOffline, "runtime is offline")
		return
	}

	now := time.Now()
	req := &FileChangeRelayRequest{
		ID:          randomID(),
		TaskID:      uuidToString(task.ID),
		FileID:      file.ID,
		RuntimeID:   runtimeID,
		UserID:      requestUserID(r),
		WorkspaceID: uuidToString(rt.WorkspaceID),
		CreatedAt:   now,
		ExpiresAt:   now.Add(fileChangeRelayTTL),
	}
	if err := h.FileChangeRelayStore.Create(r.Context(), req); err != nil {
		h.logFileChangeRelay(r, task.ID, file.ID, "error", start)
		writeError(w, 500, "queue file change relay failed")
		return
	}
	// Best-effort wakeup: a lost hint costs the full TTL wait and a retry.
	h.notifyDaemonFileChangesRequested(runtimeID)
	resp, found := h.FileChangeRelayStore.WaitResponse(r.Context(), req.ID, now.Add(fileChangeRelayWait))
	if !found {
		h.logFileChangeRelay(r, task.ID, file.ID, fileChangeCodeRuntimeTimeout, start)
		writeFileChangeError(w, http.StatusGatewayTimeout, fileChangeCodeRuntimeTimeout, "runtime did not deliver the file body")
		return
	}
	switch resp.State {
	case "purged":
		h.logFileChangeRelay(r, task.ID, file.ID, fileChangeCodeRecordPurged, start)
		writeFileChangeError(w, http.StatusGone, fileChangeCodeRecordPurged, "run file change body is purged")
		return
	case "ok":
		if len(resp.File) == 0 {
			h.logFileChangeRelay(r, task.ID, file.ID, "error", start)
			writeError(w, 500, "invalid relay response")
			return
		}
		var f rundiff.File
		if err := json.Unmarshal(resp.File, &f); err != nil || f.ID != file.ID {
			h.logFileChangeRelay(r, task.ID, file.ID, "error", start)
			writeError(w, 500, "invalid relay response")
			return
		}
		// Defense in depth on content the daemon fetched: the same pipeline
		// the upload path applies, applied to the relay path.
		if f.Before != nil {
			s := rundiff.SafeText(*f.Before)
			f.Before = &s
		}
		if f.After != nil {
			s := rundiff.SafeText(*f.After)
			f.After = &s
		}
		h.logFileChangeRelay(r, task.ID, file.ID, result(""), start)
		writeJSON(w, 200, f)
		return
	default:
		h.logFileChangeRelay(r, task.ID, file.ID, "error", start)
		writeError(w, 500, "invalid relay response")
		return
	}
}

func (h *Handler) logFileChangeRelay(r *http.Request, task pgtype.UUID, fileID, result string, start time.Time) {
	slog.Info("run file change relay",
		"user_id", requestUserID(r),
		"task_id", uuidToString(task),
		"file_id", fileID,
		"result", result,
		"elapsed_ms", time.Since(start).Milliseconds(),
	)
}

// notifyDaemonFileChangesRequested nudges the runtime's daemon to pull the
// queued relay request now. Mirrors requestDaemonPendingWork: prefer the relay
// notifier (reaches the node holding the daemon's socket), fall back to the
// local hub.
func (h *Handler) notifyDaemonFileChangesRequested(runtimeID string) {
	if runtimeID == "" {
		return
	}
	if h.DaemonFileChangesRequested != nil {
		h.DaemonFileChangesRequested.NotifyFileChangesRequested(runtimeID)
		return
	}
	if h.DaemonHub != nil {
		h.DaemonHub.NotifyFileChangesRequested(runtimeID)
	}
}

// ListFileChangeRequests hands queued relay requests to the runtime's daemon
// and marks them claimed, mirroring the heartbeat-carried pending-work claim.
func (h *Handler) ListFileChangeRequests(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	if h.FileChangeRelayStore == nil {
		writeJSON(w, 200, map[string]any{"requests": []PendingView{}})
		return
	}
	requests, err := h.FileChangeRelayStore.ClaimPending(r.Context(), uuidToString(rt.ID))
	if err != nil {
		writeError(w, 500, "load file change requests failed")
		return
	}
	if requests == nil {
		requests = []PendingView{}
	}
	writeJSON(w, 200, map[string]any{"requests": requests})
}

// CompleteFileChangeRequest receives the daemon's answer for one relay
// request: the fetched File, or a purged marker when the local record is gone.
func (h *Handler) CompleteFileChangeRequest(w http.ResponseWriter, r *http.Request) {
	requestID := chi.URLParam(r, "requestId")
	if h.FileChangeRelayStore == nil {
		writeError(w, 404, "request not found")
		return
	}
	req, err := h.FileChangeRelayStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, 500, "load request failed")
		return
	}
	if req == nil {
		writeError(w, 404, "request not found")
		return
	}
	// The posting daemon must own the request's runtime, and the task must
	// still be visible to it — a request never crosses runtime or workspace
	// boundaries even though its ID is the only thing in the path.
	if _, ok := h.requireDaemonRuntimeAccess(w, r, req.RuntimeID); !ok {
		return
	}
	if _, _, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, req.TaskID); !ok {
		return
	}
	var resp FileChangeRelayResponse
	// Two sides of at most MaxSideBytes each, JSON escaping at worst doubling
	// that; the reader caps well above the valid envelope.
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
	if err := decoder.Decode(&resp); err != nil {
		writeError(w, 400, "invalid file change response payload")
		return
	}
	switch resp.State {
	case "ok":
		var f rundiff.File
		if err := json.Unmarshal(resp.File, &f); err != nil || f.ID != req.FileID {
			writeError(w, 400, "invalid file change response file")
			return
		}
	case "purged":
	default:
		writeError(w, 400, "invalid file change response state")
		return
	}
	if err := h.FileChangeRelayStore.PutResponse(r.Context(), requestID, &resp); err != nil {
		writeError(w, 500, "store file change response failed")
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}
