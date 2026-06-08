package handler

// CEREBRO-PATCH(runtime-tools-scan-handler): JEH-1710 — daemon-facing endpoints
// that supply the runtime's MCP config and ingest a tools/list scan result.
//
// Endpoints (daemon-auth, runtime-scoped):
//
//   GET  /api/daemon/runtimes/{runtimeId}/mcp-config  — returns tools_config
//   POST /api/daemon/runtimes/{runtimeId}/tool-scan   — ingests a scan result
//
// The daemon periodically iterates its runtimes, fetches the per-runtime MCP
// config, spawns each declared server, runs tools/list, and POSTs the merged
// result here. The server upserts the (runtime, server, tool) rows so the
// admin UI shows a current inventory without admins having to type tool
// names by hand.

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5/pgtype"

	// CEREBRO-PATCH(runtime-tool-scan-connections): merge workspace Connections into daemon scan config.
	"github.com/multica-ai/multica/server/internal/cerebro/daemonmcp"
)

// RuntimeToolsScanService is the daemon-side seam the cerebro runtimetools
// service implements. Kept distinct from RuntimeToolsAdminService so the
// admin permission gate and the daemon ingest path can evolve independently.
type RuntimeToolsScanService interface {
	RecordScan(ctx context.Context, runtimeID pgtype.UUID, scannedAt time.Time, servers []RuntimeToolScanServer) error
}

// RuntimeToolScanServer is one entry in the daemon's scan payload. Empty Tools
// with a non-empty Error signals an unreachable / failing MCP server — the
// server records the error but does not zero out the previously known
// inventory.
type RuntimeToolScanServer struct {
	Name  string                `json:"name"`
	Tools []RuntimeToolScanTool `json:"tools"`
	Error string                `json:"error,omitempty"`
}

// RuntimeToolScanTool is one tool entry inside a server's tools/list result.
type RuntimeToolScanTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

// runtimeToolScanRequest is the wire body of POST .../tool-scan.
type runtimeToolScanRequest struct {
	ScannedAt time.Time               `json:"scanned_at"`
	Servers   []RuntimeToolScanServer `json:"servers"`
}

// SetRuntimeToolsScan wires the daemon-side ingest service into the handler.
// Called after handler.New() so the upstream constructor stays clean.
func (h *Handler) SetRuntimeToolsScan(svc RuntimeToolsScanService) {
	h.runtimeToolsScan = svc
}

// GetRuntimeMcpConfig handles GET /api/daemon/runtimes/{runtimeId}/mcp-config.
//
// Returns the runtime's raw tools_config JSON (Claude Code --mcp-config
// shape: {"mcpServers": {...}}) so the daemon can iterate the declared MCP
// servers without re-fetching the entire task payload. An empty
// tools_config yields {"mcpServers": {}} so the daemon can decode a stable
// shape regardless of admin state.
func (h *Handler) GetRuntimeMcpConfig(w http.ResponseWriter, r *http.Request) {
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	toolsConfig := json.RawMessage(rt.ToolsConfig)
	if len(toolsConfig) == 0 {
		toolsConfig = json.RawMessage(`{"mcpServers":{}}`)
	}
	// CEREBRO-PATCH(runtime-tool-scan-connections): include enabled workspace Connections during scan.
	if h.ConnectionsInjector != nil {
		connMCP := h.ConnectionsInjector.BuildMCPConfig(r.Context(), rt.WorkspaceID)
		if len(connMCP) > 0 {
			toolsConfig = daemonmcp.Merge(toolsConfig, connMCP)
		}
	}
	// toolsConfig is already a JSON document — proxy it as-is so the
	// daemon decodes the exact shape an admin entered (extra top-level
	// keys preserved). Validate it parses so a corrupt row 502s here
	// instead of crashing the daemon decoder.
	var parsed any
	if err := json.Unmarshal(toolsConfig, &parsed); err != nil {
		writeError(w, http.StatusBadGateway, "stored tools_config is not valid JSON")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(toolsConfig)
}

// IngestRuntimeToolScan handles POST /api/daemon/runtimes/{runtimeId}/tool-scan.
func (h *Handler) IngestRuntimeToolScan(w http.ResponseWriter, r *http.Request) {
	if h.runtimeToolsScan == nil {
		writeError(w, http.StatusServiceUnavailable, "runtime tools scan not enabled")
		return
	}
	runtimeID := chi.URLParam(r, "runtimeId")
	rt, ok := h.requireDaemonRuntimeAccess(w, r, runtimeID)
	if !ok {
		return
	}

	var body runtimeToolScanRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	scannedAt := body.ScannedAt
	if scannedAt.IsZero() {
		scannedAt = time.Now().UTC()
	}
	if err := h.runtimeToolsScan.RecordScan(r.Context(), rt.ID, scannedAt, body.Servers); err != nil {
		writeError(w, http.StatusInternalServerError, "record scan: "+err.Error())
		return
	}
	// CEREBRO-PATCH(runtime-tools-scan-capability-bridge): FIR-2284 — the legacy
	// RecordScan above only fills cerebro_runtime_tool; mirror the scanned MCP
	// tools into the capability register so they show up in the unified table.
	h.persistScannedToolsToCapabilityRegister(r, rt.ID, rt.WorkspaceID, body.Servers)
	w.WriteHeader(http.StatusNoContent)
}
