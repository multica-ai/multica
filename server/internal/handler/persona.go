package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// PersonaSandboxOption is the trimmed shape the agent settings UI needs.
type PersonaSandboxOption struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SystemOwned bool   `json:"system_owned"`
}

var personaProxyHTTP = &http.Client{Timeout: 3 * time.Second}

// ListPersonaSandboxes proxies persona's /v1/sandboxes for the agent
// settings UI. Returns an empty list (not an error) when persona is not
// configured at all so the dropdown gracefully hides.
func (h *Handler) ListPersonaSandboxes(w http.ResponseWriter, r *http.Request) {
	url := strings.TrimRight(os.Getenv("MULTICA_PERSONA_URL"), "/")
	token := os.Getenv("MULTICA_PERSONA_TOKEN")
	if url == "" || token == "" {
		writeJSON(w, http.StatusOK, map[string]any{"sandboxes": []PersonaSandboxOption{}})
		return
	}

	req, err := http.NewRequestWithContext(r.Context(), "GET", url+"/v1/sandboxes", nil)
	if err != nil {
		writeError(w, http.StatusBadGateway, "persona unreachable")
		return
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := personaProxyHTTP.Do(req)
	if err != nil {
		writeError(w, http.StatusBadGateway, "persona unreachable")
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		writeError(w, http.StatusBadGateway, "persona returned status "+resp.Status)
		return
	}

	var raw struct {
		Sandboxes []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			SystemOwned bool   `json:"system_owned"`
		} `json:"sandboxes"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		writeError(w, http.StatusBadGateway, "persona response unparseable")
		return
	}
	out := make([]PersonaSandboxOption, 0, len(raw.Sandboxes))
	for _, s := range raw.Sandboxes {
		out = append(out, PersonaSandboxOption{Name: s.Name, Description: s.Description, SystemOwned: s.SystemOwned})
	}
	writeJSON(w, http.StatusOK, map[string]any{"sandboxes": out})
}
