package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// Context receipt (MUL-9013). The daemon reports, once per run, which context
// sources it handed the agent — sizes, short hashes, delivery and load
// evidence, never content. Members read it from the run's transcript dialog.

const (
	maxContextReceiptBodyBytes = 256 << 10
	maxContextReceiptSources   = 512
	maxContextReceiptMCP       = 128
	maxContextReceiptString    = 512
)

// contextReceipt is the stored shape. Decoding into it (rather than storing
// the raw body) drops any field this server does not know, so the column only
// ever holds metadata the reader is built for.
type contextReceipt struct {
	SchemaVersion  int                    `json:"schema_version"`
	Runtime        string                 `json:"runtime"`
	RuntimeVersion string                 `json:"runtime_version,omitempty"`
	Model          string                 `json:"model,omitempty"`
	PromptBytes    int                    `json:"prompt_bytes"`
	BriefBytes     int                    `json:"brief_bytes"`
	BriefDelivery  string                 `json:"brief_delivery"`
	ResumedSession bool                   `json:"resumed_session"`
	FreshRetry     bool                   `json:"fresh_retry,omitempty"`
	MCPServers     []string               `json:"mcp_servers"`
	Sources        []contextReceiptSource `json:"sources"`
}

type contextReceiptSource struct {
	Kind            string `json:"kind"`
	Name            string `json:"name,omitempty"`
	Bytes           int    `json:"bytes"`
	SHA256          string `json:"sha256,omitempty"`
	Delivery        string `json:"delivery"`
	Path            string `json:"path,omitempty"`
	Load            string `json:"load"`
	VerifiedVersion string `json:"verified_version,omitempty"`
	Parent          string `json:"parent,omitempty"`
}

func (c *contextReceipt) valid() bool {
	if c.SchemaVersion < 1 || c.Runtime == "" {
		return false
	}
	if len(c.Sources) > maxContextReceiptSources || len(c.MCPServers) > maxContextReceiptMCP {
		return false
	}
	if c.PromptBytes < 0 || c.BriefBytes < 0 {
		return false
	}
	for _, s := range []string{c.Runtime, c.RuntimeVersion, c.Model, c.BriefDelivery} {
		if len(s) > maxContextReceiptString {
			return false
		}
	}
	for _, name := range c.MCPServers {
		if len(name) > maxContextReceiptString {
			return false
		}
	}
	for _, src := range c.Sources {
		if src.Kind == "" || src.Bytes < 0 {
			return false
		}
		for _, s := range []string{src.Kind, src.Name, src.SHA256, src.Delivery, src.Path, src.Load, src.VerifiedVersion, src.Parent} {
			if len(s) > maxContextReceiptString {
				return false
			}
		}
	}
	return true
}

// ReportTaskContextReceipt stores the daemon's receipt for a task, replacing
// any earlier one (the fresh-session retry re-reports).
func (h *Handler) ReportTaskContextReceipt(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxContextReceiptBodyBytes)
	task, workspaceID, ok := h.requireDaemonTaskAccessWithWorkspace(w, r, chi.URLParam(r, "taskId"))
	if !ok {
		return
	}

	var req struct {
		Receipt *contextReceipt `json:"receipt"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Receipt == nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !req.Receipt.valid() {
		writeError(w, http.StatusBadRequest, "invalid receipt")
		return
	}
	if req.Receipt.MCPServers == nil {
		req.Receipt.MCPServers = []string{}
	}
	if req.Receipt.Sources == nil {
		req.Receipt.Sources = []contextReceiptSource{}
	}
	stored, err := json.Marshal(req.Receipt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to encode receipt")
		return
	}
	if err := h.Queries.UpsertTaskContextReceipt(r.Context(), db.UpsertTaskContextReceiptParams{
		TaskID:      task.ID,
		WorkspaceID: parseUUID(workspaceID),
		IssueID:     task.IssueID,
		Receipt:     stored,
	}); err != nil {
		slog.Warn("store task context receipt failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to store receipt")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// GetTaskContextReceipt returns a run's receipt to a member who may read the
// run. A run without one (an older daemon, or a run that never launched)
// answers 200 with a null receipt so the reader can say so without an error.
func (h *Handler) GetTaskContextReceipt(w http.ResponseWriter, r *http.Request) {
	task, ok := h.loadTaskForUser(w, r)
	if !ok {
		return
	}
	row, err := h.Queries.GetTaskContextReceipt(r.Context(), task.ID)
	if err != nil {
		if isNotFound(err) {
			writeJSON(w, http.StatusOK, map[string]any{"receipt": nil})
			return
		}
		slog.Warn("load task context receipt failed", "task_id", uuidToString(task.ID), "error", err)
		writeError(w, http.StatusInternalServerError, "failed to load receipt")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"receipt":     json.RawMessage(row.Receipt),
		"recorded_at": row.UpdatedAt.Time.UTC().Format(time.RFC3339),
	})
}
