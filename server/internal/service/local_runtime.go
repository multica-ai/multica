package service

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/pkg/agent"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// DetectedAgent represents a locally detected agent CLI.
type DetectedAgent struct {
	Provider  string `json:"provider"`
	Path      string `json:"path"`
	Version   string `json:"version"`
	Status    string `json:"status"` // "available", "unavailable"
	Error     string `json:"error,omitempty"`
	IsCustom  bool   `json:"is_custom_path"`
}

// LocalRuntimeService manages detection and health checking of local agent CLIs.
type LocalRuntimeService struct {
	Queries *db.Queries

	mu          sync.Mutex
	healthTimer *time.Timer
}

// NewLocalRuntimeService creates a new LocalRuntimeService.
func NewLocalRuntimeService(q *db.Queries) *LocalRuntimeService {
	return &LocalRuntimeService{Queries: q}
}

// supportedProviders lists the agent CLIs we attempt to detect.
var supportedProviders = []string{"claude", "codex", "opencode"}

// DetectAgents probes for available agent CLI installations.
// Returns detected agents and persists results to the database.
func (s *LocalRuntimeService) DetectAgents(ctx context.Context, workspaceID pgtype.UUID) ([]DetectedAgent, error) {
	var results []DetectedAgent

	// Load any existing custom paths from DB.
	existing, _ := s.Queries.ListLocalAgentConfigs(ctx, workspaceID)
	customPaths := make(map[string]string)
	for _, cfg := range existing {
		if cfg.IsCustomPath {
			customPaths[cfg.Provider] = cfg.CliPath
		}
	}

	for _, provider := range supportedProviders {
		detected := s.probeAgent(ctx, provider, customPaths[provider])
		results = append(results, detected)

		// Persist detection result.
		var healthErr pgtype.Text
		if detected.Error != "" {
			healthErr = pgtype.Text{String: detected.Error, Valid: true}
		}
		s.Queries.UpsertLocalAgentConfig(ctx, db.UpsertLocalAgentConfigParams{
			WorkspaceID:     workspaceID,
			Provider:        provider,
			CliPath:         detected.Path,
			Version:         detected.Version,
			Status:          detected.Status,
			IsCustomPath:    detected.IsCustom,
			LastHealthCheck: pgtype.Timestamptz{Time: time.Now(), Valid: true},
			HealthError:     healthErr,
		})
	}

	return results, nil
}

// probeAgent checks if a specific agent CLI is available.
func (s *LocalRuntimeService) probeAgent(ctx context.Context, provider, customPath string) DetectedAgent {
	cliName := provider
	path := customPath
	isCustom := customPath != ""

	if path == "" {
		// Auto-detect from PATH.
		detectedPath, err := exec.LookPath(cliName)
		if err != nil {
			return DetectedAgent{
				Provider: provider,
				Status:   "unavailable",
				Error:    fmt.Sprintf("%s CLI not found on PATH", cliName),
			}
		}
		path = detectedPath
	}

	// Try to get version.
	versionCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	version, err := agent.DetectVersion(versionCtx, path)
	if err != nil {
		return DetectedAgent{
			Provider: provider,
			Path:     path,
			Status:   "unavailable",
			Error:    fmt.Sprintf("failed to detect version: %v", err),
			IsCustom: isCustom,
		}
	}

	return DetectedAgent{
		Provider: provider,
		Path:     path,
		Version:  strings.TrimSpace(version),
		Status:   "available",
		IsCustom: isCustom,
	}
}

// SetCustomPath updates the CLI path for a provider.
func (s *LocalRuntimeService) SetCustomPath(ctx context.Context, workspaceID pgtype.UUID, provider, path string) (*DetectedAgent, error) {
	// Validate path exists.
	if _, err := exec.LookPath(path); err != nil {
		return nil, fmt.Errorf("path %q not found or not executable: %w", path, err)
	}

	// Probe the custom path.
	detected := s.probeAgent(ctx, provider, path)

	var healthErr pgtype.Text
	if detected.Error != "" {
		healthErr = pgtype.Text{String: detected.Error, Valid: true}
	}
	s.Queries.UpsertLocalAgentConfig(ctx, db.UpsertLocalAgentConfigParams{
		WorkspaceID:     workspaceID,
		Provider:        provider,
		CliPath:         detected.Path,
		Version:         detected.Version,
		Status:          detected.Status,
		IsCustomPath:    true,
		LastHealthCheck: pgtype.Timestamptz{Time: time.Now(), Valid: true},
		HealthError:     healthErr,
	})

	return &detected, nil
}

// HealthCheckAll runs health checks on all configured agents for a workspace.
func (s *LocalRuntimeService) HealthCheckAll(ctx context.Context, workspaceID pgtype.UUID) ([]DetectedAgent, error) {
	return s.DetectAgents(ctx, workspaceID)
}

// StartPeriodicHealthCheck starts a background health check loop.
func (s *LocalRuntimeService) StartPeriodicHealthCheck(ctx context.Context, workspaceID pgtype.UUID, interval time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.healthTimer != nil {
		s.healthTimer.Stop()
	}

	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := s.HealthCheckAll(ctx, workspaceID); err != nil {
					slog.Warn("periodic health check failed", "workspace_id", workspaceID, "error", err)
				}
			}
		}
	}()
}
