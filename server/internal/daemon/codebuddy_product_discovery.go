package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/pkg/agent"
)

func (d *Daemon) discoverCodeBuddyProductModels(entry AgentEntry) (*DiscoveredModels, error) {
	disc := entry.ModelsDiscovery
	productPath, err := resolveCodeBuddyProductPath(entry)
	if err != nil {
		return nil, err
	}
	cacheKey := "ext-codebuddy-product:" + entry.ManifestID + ":" + productPath
	if cached := modelDiscoveryCache.get(cacheKey); cached != nil {
		return cached, nil
	}

	data, err := os.ReadFile(productPath)
	if err != nil {
		return nil, fmt.Errorf("read CodeBuddy product catalog %s: %w", productPath, err)
	}
	result, err := parseCodeBuddyProductModelsJSON(data)
	if err != nil {
		return nil, fmt.Errorf("parse CodeBuddy product catalog %s: %w", productPath, err)
	}

	ttl := 60 * time.Second
	if disc != nil && disc.CacheTTLSeconds > 0 {
		ttl = time.Duration(disc.CacheTTLSeconds) * time.Second
	}
	if len(result.Models) > 0 {
		modelDiscoveryCache.set(cacheKey, result, ttl)
	}
	return result, nil
}

type codeBuddyProductCatalog struct {
	Models []codeBuddyProductModel `json:"models"`
}

type codeBuddyProductModel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Label       string `json:"label"`
	DisplayName string `json:"displayName"`
	Default     bool   `json:"default"`
	Hidden      bool   `json:"hidden"`
	Disabled    bool   `json:"disabled"`
}

func parseCodeBuddyProductModelsJSON(data []byte) (*DiscoveredModels, error) {
	var catalog codeBuddyProductCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, err
	}

	models := make([]agent.Model, 0, len(catalog.Models))
	seen := make(map[string]struct{}, len(catalog.Models))
	hasDefault := false
	for _, raw := range catalog.Models {
		id := strings.TrimSpace(raw.ID)
		if id == "" || raw.Hidden || raw.Disabled {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		label := strings.TrimSpace(raw.Name)
		if label == "" {
			label = strings.TrimSpace(raw.Label)
		}
		if label == "" {
			label = strings.TrimSpace(raw.DisplayName)
		}
		if label == "" {
			label = id
		}

		m := agent.Model{
			ID:       id,
			Label:    label,
			Provider: "codebuddy",
			Default:  raw.Default,
		}
		if m.Default {
			hasDefault = true
		}
		models = append(models, m)
	}

	if !hasDefault && len(models) > 0 {
		models[0].Default = true
	}

	return &DiscoveredModels{Models: models}, nil
}

func resolveCodeBuddyProductPath(entry AgentEntry) (string, error) {
	if productPath := strings.TrimSpace(lookupRuntimeEnv(entry, "ACC_PRODUCT_CONFIG_PATH")); productPath != "" {
		return expandRuntimePath(productPath), nil
	}

	root, err := findCodeBuddyPackageRoot(entry.Path)
	if err != nil {
		return "", err
	}

	if productFile := strings.TrimSpace(lookupRuntimeEnv(entry, "MULTICA_CODEBUDDY_PRODUCT_FILE")); productFile != "" {
		if filepath.IsAbs(productFile) {
			return productFile, nil
		}
		return filepath.Join(root, productFile), nil
	}

	if currentModel := readCodeBuddyCurrentModel(); currentModel != "" {
		if candidate := findCodeBuddyProductForModel(root, currentModel); candidate != "" {
			return candidate, nil
		}
	}

	for _, candidate := range codeBuddyProductCandidates(root) {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("no CodeBuddy product catalog found under %s", root)
}

func findCodeBuddyPackageRoot(execPath string) (string, error) {
	if strings.TrimSpace(execPath) == "" {
		return "", fmt.Errorf("CodeBuddy executable path is empty")
	}

	var candidates []string
	addCandidate := func(path string) {
		if path == "" {
			return
		}
		if root := codeBuddyPackageRootFromPath(path); root != "" {
			candidates = append(candidates, root)
		}
		dir := filepath.Dir(path)
		candidates = append(candidates, filepath.Join(dir, "node_modules", "@tencent-ai", "codebuddy-code"))
	}

	addCandidate(execPath)
	if resolved, err := exec.LookPath(execPath); err == nil {
		addCandidate(resolved)
		if realPath, err := filepath.EvalSymlinks(resolved); err == nil {
			addCandidate(realPath)
		}
	}

	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		candidate = filepath.Clean(candidate)
		if _, ok := seen[candidate]; ok {
			continue
		}
		seen[candidate] = struct{}{}
		if stat, err := os.Stat(filepath.Join(candidate, "package.json")); err == nil && !stat.IsDir() {
			return candidate, nil
		}
	}

	return "", fmt.Errorf("cannot locate @tencent-ai/codebuddy-code package for %q", execPath)
}

func codeBuddyPackageRootFromPath(path string) string {
	slashed := filepath.ToSlash(filepath.Clean(path))
	lower := strings.ToLower(slashed)
	marker := "/node_modules/@tencent-ai/codebuddy-code"
	idx := strings.Index(lower, marker)
	if idx < 0 {
		return ""
	}
	return filepath.FromSlash(slashed[:idx+len(marker)])
}

func lookupRuntimeEnv(entry AgentEntry, key string) string {
	if entry.Env != nil {
		if v, ok := entry.Env[key]; ok {
			return v
		}
	}
	return os.Getenv(key)
}

func expandRuntimePath(path string) string {
	path = os.ExpandEnv(path)
	if strings.HasPrefix(path, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if path == "~" {
				return home
			}
			if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
				return filepath.Join(home, path[2:])
			}
		}
	}
	return path
}

func readCodeBuddyCurrentModel() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	for _, path := range []string{
		filepath.Join(home, ".codebuddy", "settings.json"),
		filepath.Join(home, ".codebuddycn", "settings.json"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var settings struct {
			Model string `json:"model"`
		}
		if err := json.Unmarshal(data, &settings); err != nil {
			continue
		}
		if model := strings.TrimSpace(settings.Model); model != "" {
			return model
		}
	}
	return ""
}

func findCodeBuddyProductForModel(root, modelID string) string {
	modelID = strings.TrimSpace(modelID)
	if modelID == "" {
		return ""
	}
	for _, candidate := range codeBuddyProductCandidates(root) {
		data, err := os.ReadFile(candidate)
		if err != nil {
			continue
		}
		if codeBuddyProductContainsModel(data, modelID) {
			return candidate
		}
	}
	return ""
}

func codeBuddyProductCandidates(root string) []string {
	return []string{
		filepath.Join(root, "product.json"),
		filepath.Join(root, "product.ioa.json"),
		filepath.Join(root, "product.cloudhosted.json"),
		filepath.Join(root, "product.internal.json"),
		filepath.Join(root, "product.selfhosted.json"),
	}
}

func codeBuddyProductContainsModel(data []byte, modelID string) bool {
	var catalog codeBuddyProductCatalog
	if err := json.Unmarshal(data, &catalog); err != nil {
		return false
	}
	for _, model := range catalog.Models {
		if strings.TrimSpace(model.ID) == modelID && !model.Hidden && !model.Disabled {
			return true
		}
	}
	return false
}

func isCodeBuddyEntry(entry AgentEntry) bool {
	provider := strings.ToLower(entry.Provider)
	manifestID := strings.ToLower(entry.ManifestID)
	base := strings.ToLower(filepath.Base(entry.Path))
	return strings.Contains(provider, "codebuddy") ||
		strings.Contains(manifestID, "codebuddy") ||
		base == "codebuddy" ||
		base == "codebuddy.exe" ||
		base == "codebuddy.cmd" ||
		base == "cbc" ||
		base == "cbc.exe" ||
		base == "cbc.cmd"
}
