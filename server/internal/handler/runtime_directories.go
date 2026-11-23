package handler

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

func (h *Handler) requireDirectoryRuntimeOwner(w http.ResponseWriter, r *http.Request, source string) (db.AgentRuntime, bool) {
	rt, member, ok := h.requireRuntimeReadAccess(w, r, source, chi.URLParam(r, "runtimeId"))
	if !ok {
		return db.AgentRuntime{}, false
	}
	if !rt.OwnerID.Valid || uuidToString(rt.OwnerID) != uuidToString(member.UserID) {
		writeError(w, http.StatusForbidden, "insufficient permissions")
		return db.AgentRuntime{}, false
	}
	return rt, true
}

func (h *Handler) InitiateListDirectories(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDirectoryRuntimeOwner(w, r, obsmetrics.RuntimeLookupSourceRuntimeAPI)
	if !ok {
		return
	}
	if rt.Status != "online" {
		writeError(w, http.StatusServiceUnavailable, "runtime is offline")
		return
	}
	if err := validateChatDirectoryRuntime(rt, "", uuidToString(rt.OwnerID)); err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req, err := h.DirectoryListStore.Create(r.Context(), uuidToString(rt.ID), rt.DaemonID.String, body.Path)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to enqueue directory listing")
		return
	}
	h.requestDaemonPendingWork(uuidToString(rt.ID), protocol.PendingWorkKindDirectories)
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) GetDirectoryListRequest(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDirectoryRuntimeOwner(w, r, obsmetrics.RuntimeLookupSourceRuntimeDirectoryPoll)
	if !ok {
		return
	}
	req, err := h.DirectoryListStore.Get(r.Context(), chi.URLParam(r, "requestId"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load directory request")
		return
	}
	if req == nil || req.RuntimeID != uuidToString(rt.ID) || req.DaemonID != rt.DaemonID.String {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	writeJSON(w, http.StatusOK, req)
}

func (h *Handler) ReportDirectoryListResult(w http.ResponseWriter, r *http.Request) {
	rt, ok := h.requireDaemonRuntimeAccess(w, r, chi.URLParam(r, "runtimeId"))
	if !ok {
		return
	}
	requestID := chi.URLParam(r, "requestId")
	req, err := h.DirectoryListStore.Get(r.Context(), requestID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load directory request")
		return
	}
	if req == nil || req.RuntimeID != uuidToString(rt.ID) || req.DaemonID != rt.DaemonID.String {
		writeError(w, http.StatusNotFound, "request not found")
		return
	}
	if directoryRequestTerminal(req) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
		return
	}
	if req.Status != "running" {
		writeError(w, http.StatusConflict, "directory request has not been dispatched")
		return
	}
	var body struct {
		Status string                     `json:"status"`
		Result *protocol.DirectoryListing `json:"result"`
		Error  string                     `json:"error"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.Status != "completed" && body.Status != "failed" || body.Status == "completed" && (body.Result == nil || body.Result.Path == "") {
		writeError(w, http.StatusBadRequest, "invalid directory result")
		return
	}
	if body.Status == "failed" {
		body.Result = nil
	}
	if err := h.DirectoryListStore.Finish(r.Context(), requestID, body.Result, body.Error); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to persist directory result")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
