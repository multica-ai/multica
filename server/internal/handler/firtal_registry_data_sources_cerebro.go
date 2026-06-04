package handler

// CEREBRO-PATCH(firtal-registry-data-sources): TECH-2925 admin proxy for FDR data source catalog.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
)

const firtalRegistryListPath = "/api/registry/data-sources"

// FirtalRegistryDataSource is the wire shape for the admin allowlist picker.
type FirtalRegistryDataSource struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
}

type firtalRegistryWorkspaceCache struct {
	expires time.Time
	items   []FirtalRegistryDataSource
}

var (
	firtalRegistryDataSourcesCacheMu sync.Mutex
	firtalRegistryDataSourcesCache   = map[string]firtalRegistryWorkspaceCache{}
	firtalRegistryDataSourcesTTL     = 60 * time.Second
)

// ListFirtalRegistryDataSources handles GET /api/agents/{id}/firtal-registry/data-sources.
// Proxies the workspace-configured Firtal Data Registry catalog for the admin UI.
func (h *Handler) ListFirtalRegistryDataSources(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	agent, ok := h.loadAgentForUser(w, r, id)
	if !ok {
		return
	}
	if _, ok := h.canManageAgent(w, r, agent); !ok {
		return
	}

	wsID := agent.WorkspaceID.String()
	if cached, ok := getCachedFirtalRegistryDataSources(wsID); ok {
		writeJSON(w, http.StatusOK, cached)
		return
	}

	baseURL, apiKey, err := h.loadFirtalRegistryCredentials(r.Context(), agent.WorkspaceID)
	if err != nil {
		if strings.Contains(err.Error(), "not configured") {
			writeError(w, http.StatusUnauthorized, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	items, err := fetchFirtalRegistryDataSources(r.Context(), baseURL, apiKey)
	if err != nil {
		writeError(w, http.StatusBadGateway, "data registry: "+err.Error())
		return
	}

	setCachedFirtalRegistryDataSources(wsID, items)
	writeJSON(w, http.StatusOK, items)
}

func getCachedFirtalRegistryDataSources(workspaceID string) ([]FirtalRegistryDataSource, bool) {
	firtalRegistryDataSourcesCacheMu.Lock()
	defer firtalRegistryDataSourcesCacheMu.Unlock()
	entry, ok := firtalRegistryDataSourcesCache[workspaceID]
	if !ok || time.Now().After(entry.expires) {
		return nil, false
	}
	out := make([]FirtalRegistryDataSource, len(entry.items))
	copy(out, entry.items)
	return out, true
}

func setCachedFirtalRegistryDataSources(workspaceID string, items []FirtalRegistryDataSource) {
	firtalRegistryDataSourcesCacheMu.Lock()
	defer firtalRegistryDataSourcesCacheMu.Unlock()
	copied := make([]FirtalRegistryDataSource, len(items))
	copy(copied, items)
	firtalRegistryDataSourcesCache[workspaceID] = firtalRegistryWorkspaceCache{
		expires: time.Now().Add(firtalRegistryDataSourcesTTL),
		items:   copied,
	}
}

func fetchFirtalRegistryDataSources(ctx context.Context, baseURL, apiKey string) ([]FirtalRegistryDataSource, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+firtalRegistryListPath, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, errHTTPStatus(resp.StatusCode, body)
	}
	return normalizeFirtalRegistryDataSources(body)
}

func normalizeFirtalRegistryDataSources(raw []byte) ([]FirtalRegistryDataSource, error) {
	var asArray []map[string]any
	if err := json.Unmarshal(raw, &asArray); err == nil {
		return mapFirtalRegistryDataSources(asArray), nil
	}
	var envelope map[string]any
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if data, ok := envelope["data"].([]any); ok {
		items := make([]map[string]any, 0, len(data))
		for _, item := range data {
			if m, ok := item.(map[string]any); ok {
				items = append(items, m)
			}
		}
		return mapFirtalRegistryDataSources(items), nil
	}
	return []FirtalRegistryDataSource{}, nil
}

func mapFirtalRegistryDataSources(items []map[string]any) []FirtalRegistryDataSource {
	out := make([]FirtalRegistryDataSource, 0, len(items))
	for _, item := range items {
		id, _ := item["id"].(string)
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		name, _ := item["name"].(string)
		name = strings.TrimSpace(name)
		if name == "" {
			name = id
		}
		row := FirtalRegistryDataSource{ID: id, Name: name}
		if desc, ok := item["description"].(string); ok {
			desc = strings.TrimSpace(desc)
			if desc != "" {
				row.Description = &desc
			}
		}
		out = append(out, row)
	}
	return out
}

type httpStatusError struct {
	code int
	body string
}

func (e httpStatusError) Error() string {
	msg := strings.TrimSpace(e.body)
	if len(msg) > 512 {
		msg = msg[:512] + "…"
	}
	return fmt.Sprintf("HTTP %d: %s", e.code, msg)
}

func errHTTPStatus(code int, body []byte) error {
	return httpStatusError{code: code, body: string(body)}
}
