package handler

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/multica-ai/multica/server/internal/marketplace"
)

// PreviewClaudeMarketplace validates a Claude Code marketplace manifest without
// fetching or installing anything. Installation remains an explicit native
// plugin publish/preview/consent flow, so an untrusted catalog cannot execute
// code or silently grant an MCP scope.
//
// POST /api/workspaces/{id}/plugins/marketplace/preview
// Body: {"manifest": <contents of marketplace.json>}
func (h *Handler) PreviewClaudeMarketplace(w http.ResponseWriter, r *http.Request) {
	if !h.requirePluginsV1(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, marketplace.MaxManifestBytes)
	var request struct {
		Manifest json.RawMessage `json:"manifest"`
	}
	if err := json.NewDecoder(r.Body).Decode(&request); err != nil || len(request.Manifest) == 0 {
		writeError(w, http.StatusBadRequest, "manifest is required and must be valid JSON")
		return
	}
	if _, err := io.ReadAll(r.Body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid marketplace manifest")
		return
	}
	manifest, err := marketplace.ParseMarketplace(request.Manifest)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	type source struct {
		Kind       marketplace.SourceKind `json:"kind"`
		Repository string                 `json:"repository,omitempty"`
		URL        string                 `json:"url,omitempty"`
		Ref        string                 `json:"ref,omitempty"`
		Subdir     string                 `json:"subdir,omitempty"`
		Package    string                 `json:"package,omitempty"`
		Version    string                 `json:"version,omitempty"`
	}
	type plugin struct {
		Name        string `json:"name"`
		Description string `json:"description,omitempty"`
		Version     string `json:"version,omitempty"`
		Source      source `json:"source"`
	}
	plugins := make([]plugin, 0, len(manifest.Plugins))
	for _, item := range manifest.Plugins {
		plugins = append(plugins, plugin{
			Name: item.Name, Description: item.Description, Version: item.Version,
			Source: source{Kind: item.Source.Kind, Repository: item.Source.Repository, URL: item.Source.URL, Ref: item.Source.Ref, Subdir: item.Source.Subdir, Package: item.Source.Package, Version: item.Source.Version},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"name": manifest.Name, "owner": manifest.Owner, "description": manifest.Description,
		"version": manifest.Version, "plugins": plugins,
	})
}
