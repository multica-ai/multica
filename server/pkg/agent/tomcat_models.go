package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

const tomcatDiscoveryTimeout = 15 * time.Second

// discoverTomcatModels reads the saved catalog using the same command identity
// as ACP execution, without starting a service, session, or model request.
func discoverTomcatModels(ctx context.Context, runtimeCmd Command) (Catalog, error) {
	if runtimeCmd.Path == "" {
		runtimeCmd.Path = "tomcat"
	}
	runCtx, cancel := context.WithTimeout(ctx, tomcatDiscoveryTimeout)
	defer cancel()
	cmd := runtimeCmd.exec(runCtx, "serve", "models")
	hideAgentWindow(cmd)
	cmd.Env = buildEnv(nil)
	stdout, err := outputOwned(cmd, runtimeCmd.logger)
	if err != nil {
		if runCtx.Err() != nil {
			return Catalog{}, fmt.Errorf("Tomcat model discovery canceled or timed out: %w", runCtx.Err())
		}
		// outputOwned already bounds the stderr tail. Never include stdout: a
		// wrapper or incompatible executable may print unrelated private data.
		detail := ""
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
				detail = ": " + stderr
			}
		}
		return Catalog{}, fmt.Errorf("Tomcat serve models failed; check this runtime's command/configuration, or update its Tomcat if the command is unsupported: %w%s", err, detail)
	}

	// Only consume the stable fields needed by this picker. Other metadata in
	// ListModelsPayload, including preferences and warnings, belongs to Tomcat.
	var payload struct {
		Models []struct {
			ID                       string   `json:"id"`
			Provider                 string   `json:"provider"`
			KeyPresent               *bool    `json:"keyPresent"`
			SupportedReasoningLevels []string `json:"supportedReasoningLevels"`
		} `json:"models"`
		DefaultModelID string `json:"defaultModelId"`
	}
	if err := json.Unmarshal(stdout, &payload); err != nil || payload.Models == nil {
		return Catalog{}, errors.New("Tomcat returned an invalid model catalog from serve models; check the runtime command and update Tomcat")
	}
	models := make([]Model, 0, len(payload.Models))
	seen := make(map[string]bool, len(payload.Models))
	for i, entry := range payload.Models {
		if strings.TrimSpace(entry.ID) == "" || strings.TrimSpace(entry.Provider) == "" || entry.KeyPresent == nil || seen[entry.ID] {
			return Catalog{}, fmt.Errorf("Tomcat returned an invalid or duplicate model entry at index %d", i)
		}
		seen[entry.ID] = true
		if *entry.KeyPresent {
			model := Model{ID: entry.ID, Label: entry.ID, Provider: entry.Provider, Default: entry.ID == payload.DefaultModelID}
			for _, level := range entry.SupportedReasoningLevels {
				if level == "" {
					continue
				}
				if model.Thinking == nil {
					model.Thinking = &ModelThinking{}
				}
				label, ok := claudeEffortLabel[level]
				if !ok {
					label = strings.ToUpper(level[:1]) + level[1:]
				}
				model.Thinking.SupportedLevels = append(model.Thinking.SupportedLevels, ThinkingLevel{Value: level, Label: label})
			}
			models = append(models, model)
		}
	}
	if len(models) == 0 {
		return Catalog{}, errors.New("Tomcat has no models with saved credentials. Save credentials for this runtime in Tomcat Settings or with 'tomcat model key set', then refresh models")
	}
	return Catalog{Models: models}, nil
}
