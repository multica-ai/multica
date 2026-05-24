package daemon

// CEREBRO-PATCH(daemon-infisical-spawn): fetch assigned Infisical refs as env vars at spawn.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/infisical"
)

func (d *Daemon) prepareInfisicalSpawn(ctx context.Context, agent *AgentData, taskLog *slog.Logger) (map[string]string, error) {
	if agent == nil || len(agent.InfisicalSecrets) == 0 {
		return nil, nil
	}
	client := infisical.Client{
		SiteURL:   os.Getenv("INFISICAL_SITE_URL"),
		Token:     os.Getenv("INFISICAL_TOKEN"),
		ProjectID: os.Getenv("INFISICAL_PROJECT_ID"),
	}
	env := make(map[string]string, len(agent.InfisicalSecrets))
	for _, ref := range agent.InfisicalSecrets {
		envName := strings.TrimSpace(ref.EnvVarName)
		if envName == "" {
			continue
		}
		if isBlockedEnvKey(envName) {
			taskLog.Warn("infisical secret env: blocked key skipped", "key", envName)
			continue
		}
		value, err := client.FetchRawSecret(ctx, infisical.SecretRef{
			SecretName:  ref.SecretName,
			Environment: ref.Environment,
			SecretPath:  ref.SecretPath,
		})
		if err != nil {
			return nil, fmt.Errorf("fetch %s from infisical: %w", envName, err)
		}
		env[envName] = value
	}
	return env, nil
}
