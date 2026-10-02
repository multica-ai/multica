package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/qoderruntime"
)

const qoderAPIURL = "https://api.qoder.com/api/v1/cloud"

type qoderCatalogItem struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Only names and identifiers leave the server. Provider configurations may contain secrets.
func fetchQoderCatalog(ctx context.Context, baseURL, token, kind string) ([]qoderCatalogItem, error) {
	client := &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	items := []qoderCatalogItem{}
	cursor := ""
	seen := map[string]bool{}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for page := 0; page < 100; page++ {
		query := url.Values{"limit": {"100"}}
		if cursor != "" {
			query.Set("after_id", cursor)
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/"+kind+"?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("invalid QCA catalog request")
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("QCA connection failed")
		}
		if res.StatusCode != http.StatusOK {
			res.Body.Close()
			return nil, fmt.Errorf("QCA catalog returned HTTP %d", res.StatusCode)
		}
		var result struct {
			Data []struct {
				ID         string  `json:"id"`
				Name       string  `json:"name"`
				ArchivedAt *string `json:"archived_at"`
			} `json:"data"`
			HasMore bool   `json:"has_more"`
			LastID  string `json:"last_id"`
		}
		raw, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
		res.Body.Close()
		if err != nil || len(raw) > 2<<20 || json.Unmarshal(raw, &result) != nil || result.Data == nil {
			return nil, fmt.Errorf("invalid QCA catalog response")
		}
		for _, item := range result.Data {
			prefix := "env_"
			if kind == "agents" {
				prefix = "agent_"
			}
			if !strings.HasPrefix(item.ID, prefix) {
				return nil, fmt.Errorf("invalid QCA catalog identity")
			}
			if item.ArchivedAt != nil || seen[item.ID] {
				continue
			}
			seen[item.ID] = true
			name := strings.TrimSpace(item.Name)
			if name == "" {
				name = item.ID
			}
			items = append(items, qoderCatalogItem{ID: item.ID, Name: name})
		}
		if !result.HasMore {
			return items, nil
		}
		if result.LastID == "" || result.LastID == cursor || len(result.Data) == 0 {
			return nil, fmt.Errorf("invalid QCA pagination")
		}
		cursor = result.LastID
	}
	return nil, fmt.Errorf("QCA catalog exceeds pagination limit")
}

func (h *Handler) ListQoderAgents(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	cfg, _, _, _, err := h.readQoderSettings(r.Context(), uuidToString(ws))
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 409, "configure a QCA connection first")
		return
	}
	if err != nil {
		writeError(w, 500, "cannot read QCA configuration")
		return
	}
	items, err := fetchQoderCatalog(r.Context(), qoderAPIURL, cfg.QoderToken, "agents")
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func (h *Handler) ListQoderEnvironments(w http.ResponseWriter, r *http.Request) {
	ws, ok := parseUUIDOrBadRequest(w, chi.URLParam(r, "id"), "workspace_id")
	if !ok {
		return
	}
	var input struct {
		Token string `json:"qoder_token"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10))
	dec.DisallowUnknownFields()
	if dec.Decode(&input) != nil {
		writeError(w, 400, "invalid QCA token request")
		return
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		writeError(w, 400, "request must contain one JSON object")
		return
	}
	token := strings.TrimSpace(input.Token)
	if token == "" {
		cfg, _, _, _, err := h.readQoderSettings(r.Context(), uuidToString(ws))
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, 500, "cannot read QCA configuration")
			return
		}
		token = cfg.QoderToken
	}
	if token == "" || strings.ContainsAny(token, "\r\n\t ") {
		writeError(w, 400, "QCA token is required")
		return
	}
	items, err := fetchQoderCatalog(r.Context(), qoderAPIURL, token, "environments")
	if err != nil {
		writeError(w, 502, err.Error())
		return
	}
	writeJSON(w, 200, map[string]any{"items": items})
}

func validateQoderAgentConfig(raw []byte) error {
	var cfg struct {
		AgentID string `json:"qoder_agent_id"`
	}
	if json.Unmarshal(raw, &cfg) != nil || !qoderruntime.ValidAgentID(cfg.AgentID) {
		return fmt.Errorf("select a QCA Agent in the agent runtime configuration")
	}
	return nil
}
