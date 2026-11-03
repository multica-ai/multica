package codexappserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

const modelPageLimit = 32

// RequestFunc is one app-server request, on the process whose defaults matter.
type RequestFunc func(ctx context.Context, method string, params any) (json.RawMessage, error)

// CatalogDefaultModel reads the runtime catalog's default model. It is the last
// step of model resolution, after an explicit choice and the runtime's config,
// and is shared by task execution and local conversations.
func CatalogDefaultModel(ctx context.Context, request RequestFunc) (string, error) {
	var cursor *string
	seen := map[string]bool{}
	defaultModel := ""
	for page := 0; page < modelPageLimit; page++ {
		raw, err := request(ctx, "model/list", map[string]any{"cursor": cursor, "limit": 100})
		if err != nil {
			return "", fmt.Errorf("read default model: %w", err)
		}
		var catalog struct {
			Data []struct {
				ID        string `json:"id"`
				Model     string `json:"model"`
				IsDefault bool   `json:"isDefault"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		if err := json.Unmarshal(raw, &catalog); err != nil || catalog.Data == nil {
			return "", errors.New("invalid default model catalog response")
		}
		for _, model := range catalog.Data {
			if !model.IsDefault {
				continue
			}
			name := model.Model
			if strings.TrimSpace(name) == "" {
				name = model.ID
			}
			if strings.TrimSpace(name) == "" || (defaultModel != "" && defaultModel != name) {
				return "", errors.New("default model catalog is ambiguous")
			}
			defaultModel = name
		}
		if catalog.NextCursor == nil || *catalog.NextCursor == "" {
			if defaultModel == "" {
				return "", errors.New("runtime did not report a configured or default model")
			}
			return defaultModel, nil
		}
		if seen[*catalog.NextCursor] {
			return "", errors.New("default model catalog repeated its cursor")
		}
		seen[*catalog.NextCursor] = true
		cursor = catalog.NextCursor
	}
	return "", errors.New("default model catalog exceeded its page limit")
}
