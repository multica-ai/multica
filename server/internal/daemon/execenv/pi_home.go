package execenv

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/multica-ai/multica/server/internal/piagent"
)

// PreparePiHome creates a task-scoped Pi agent directory. Host auth, packages,
// and future top-level state remain available through the same generic mirror
// strategy used by Hermes, while models.json is derived locally so Multica's
// provider selection cannot modify the user's global Pi configuration.
func PreparePiHome(rootDir, sourceHome string, cfg piagent.Config, logger *slog.Logger) (string, error) {
	if sourceHome == "" {
		sourceHome = os.Getenv("PI_CODING_AGENT_DIR")
	}
	if sourceHome == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolve Pi home: %w", err)
		}
		sourceHome = filepath.Join(home, ".pi", "agent")
	}
	var err error
	sourceHome, err = filepath.Abs(sourceHome)
	if err != nil {
		return "", fmt.Errorf("resolve Pi source home: %w", err)
	}
	piHome := filepath.Join(rootDir, "pi-agent")
	piHome, err = filepath.Abs(piHome)
	if err != nil {
		return "", fmt.Errorf("resolve Pi task home: %w", err)
	}
	if filepath.Clean(sourceHome) == filepath.Clean(piHome) {
		return "", fmt.Errorf("Pi source home and task home must differ")
	}
	if err := os.MkdirAll(piHome, 0o700); err != nil {
		return "", fmt.Errorf("create Pi task home: %w", err)
	}
	if err := mirrorPiHome(sourceHome, piHome); err != nil {
		return "", err
	}
	if err := writeDerivedPiModels(sourceHome, piHome, cfg); err != nil {
		return "", err
	}
	logger.Debug("prepared task-scoped Pi home", "path", piHome, "provider", cfg.Provider)
	return piHome, nil
}

func mirrorPiHome(sourceHome, piHome string) error {
	entries, err := os.ReadDir(sourceHome)
	if err != nil {
		if os.IsNotExist(err) {
			return reconcilePiMirrors(piHome, nil)
		}
		return fmt.Errorf("read Pi source home: %w", err)
	}
	mirrored := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		if entry.Name() == "models.json" {
			continue
		}
		name := entry.Name()
		if err := linkSharedHermesEntry(filepath.Join(sourceHome, name), filepath.Join(piHome, name)); err != nil {
			return fmt.Errorf("mirror Pi home entry %s: %w", name, err)
		}
		mirrored[name] = struct{}{}
	}
	return reconcilePiMirrors(piHome, mirrored)
}

func reconcilePiMirrors(piHome string, mirrored map[string]struct{}) error {
	entries, err := os.ReadDir(piHome)
	if err != nil {
		return fmt.Errorf("read Pi task home: %w", err)
	}
	for _, entry := range entries {
		if entry.Name() == "models.json" {
			continue
		}
		if _, ok := mirrored[entry.Name()]; ok {
			continue
		}
		if err := os.RemoveAll(filepath.Join(piHome, entry.Name())); err != nil {
			return fmt.Errorf("remove stale Pi home entry %s: %w", entry.Name(), err)
		}
	}
	return nil
}

func writeDerivedPiModels(sourceHome, piHome string, cfg piagent.Config) error {
	root := map[string]any{}
	sourcePath := filepath.Join(sourceHome, "models.json")
	if data, err := os.ReadFile(sourcePath); err == nil {
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("parse source Pi models.json: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("read source Pi models.json: %w", err)
	}

	providers, ok := root["providers"].(map[string]any)
	if !ok {
		providers = map[string]any{}
		root["providers"] = providers
	}
	providers[cfg.Provider] = map[string]any{
		"baseUrl": cfg.BaseURL,
		"api":     cfg.API,
		"apiKey":  "$" + piagent.APIKeyEnv,
		"models": []any{
			map[string]any{"id": cfg.Model},
		},
	}
	data, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal Pi models.json: %w", err)
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Join(piHome, "models.json"), data, 0o600)
}
