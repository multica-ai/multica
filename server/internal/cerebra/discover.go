package cerebra

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// DiscoverModels probes all locally available model runtimes and returns a
// deduplicated list of model IDs ready for BuildTierMapFromCatalog.
// Safe to call from external sidecars — no daemon internals required.
func DiscoverModels(ctx context.Context) []string {
	seen := make(map[string]bool)
	var models []string

	add := func(id string) {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			models = append(models, id)
		}
	}

	for _, m := range probeOllama(ctx) {
		add(m)
	}
	return models
}

type ollamaTagsResp struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// probeOllama queries the local Ollama daemon (127.0.0.1:11434) for installed models.
// Returns nil silently if Ollama is not running — no error surfaced to callers.
func probeOllama(ctx context.Context) []string {
	ctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", "http://127.0.0.1:11434/api/tags", nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp.StatusCode != 200 {
		return nil
	}
	defer resp.Body.Close()

	var tags ollamaTagsResp
	if err := json.NewDecoder(resp.Body).Decode(&tags); err != nil {
		return nil
	}

	out := make([]string, 0, len(tags.Models))
	for _, m := range tags.Models {
		name := strings.TrimSpace(m.Name)
		if name != "" {
			out = append(out, "ollama/"+name)
		}
	}
	return out
}
