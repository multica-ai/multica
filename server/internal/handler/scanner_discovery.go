// CEREBRO-PATCH(scanner-discovery): persona integration changes.
package handler

import (
	"log/slog"
	"net/http"
	"strings"
)

// ScannerDiscoveryRuntime is the public read-only view of a runtime that
// persona's scanner uses to know which tools each runtime exposes. The
// shape is intentionally narrow — name + provider + capabilities — so the
// scanner doesn't need to know about workspace IDs, daemon IDs, or other
// internal Multica state.
type ScannerDiscoveryRuntime struct {
	Name       string   `json:"name"`
	Provider   string   `json:"provider"`
	Tools      []string `json:"tools"`
	MCPServers []string `json:"mcp_servers"`
}

// GetScannerDiscoveryRuntimes is a cross-workspace, read-only endpoint
// consumed by persona's scanner. Persona walks the list and treats each
// runtime as a governed surface, so its coverage report can answer
// "which tools on which runtimes are ungoverned in the active sandbox?"
// (#1 in docs/persona-deferred-work.md).
//
// Auth: requires Authorization: Bearer <ScannerDiscoveryToken> matching
// MULTICA_SCANNER_DISCOVERY_TOKEN. When the env var is empty the endpoint
// is disabled (503) so an operator who hasn't configured the token can't
// accidentally expose runtime topology.
func (h *Handler) GetScannerDiscoveryRuntimes(w http.ResponseWriter, r *http.Request) {
	if h.cfg.ScannerDiscoveryToken == "" {
		writeError(w, http.StatusServiceUnavailable, "scanner discovery is not configured (set MULTICA_SCANNER_DISCOVERY_TOKEN)")
		return
	}

	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) || header[len(prefix):] != h.cfg.ScannerDiscoveryToken {
		writeError(w, http.StatusUnauthorized, "invalid scanner discovery token")
		return
	}

	runtimes, err := h.Queries.ListAllAgentRuntimes(r.Context())
	if err != nil {
		slog.Error("scanner discovery: list runtimes failed", "error", err)
		writeError(w, http.StatusInternalServerError, "failed to list runtimes")
		return
	}

	resp := make([]ScannerDiscoveryRuntime, 0, len(runtimes))
	for _, rt := range runtimes {
		entry := ScannerDiscoveryRuntime{
			Name:       rt.Name,
			Provider:   rt.Provider,
			Tools:      []string{},
			MCPServers: []string{},
		}
		caps := normalizedRuntimeCapabilities(rt.Provider, rt.Capabilities, rt.ToolsConfig) // CEREBRO-PATCH(scanner-discovery-capability-normalize): include registry defaults and runtime MCP config.
		entry.Tools = anyStringSlice(caps["tools"])
		entry.MCPServers = anyStringSlice(caps["mcp_servers"])
		resp = append(resp, entry)
	}

	writeJSON(w, http.StatusOK, map[string]any{"runtimes": resp})
}
