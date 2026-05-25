package daemon

// CEREBRO-PATCH(daemon-infisical-spawn): fetch secrets from assigned Infisical folders as env vars at spawn.

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/multica-ai/multica/server/internal/cerebro/infisical"
)

func (d *Daemon) prepareInfisicalSpawn(ctx context.Context, agent *AgentData, taskLog *slog.Logger) (map[string]string, error) {
	if agent == nil || len(agent.InfisicalFolders) == 0 {
		return nil, nil
	}
	client := infisical.Client{
		SiteURL:   os.Getenv("INFISICAL_SITE_URL"),
		Token:     os.Getenv("INFISICAL_TOKEN"),
		ProjectID: os.Getenv("INFISICAL_PROJECT_ID"),
	}
	env := make(map[string]string)
	for _, folder := range agent.InfisicalFolders {
		secrets, err := client.ListFolderSecrets(ctx, infisical.FolderRef{
			Environment: folder.Environment,
			SecretPath:  folder.SecretPath,
		})
		if err != nil {
			return nil, fmt.Errorf("list infisical folder %s%s: %w", folder.Environment, folder.SecretPath, err)
		}
		for _, secret := range secrets {
			key := strings.TrimSpace(secret.Key)
			if key == "" {
				continue
			}
			if isBlockedEnvKey(key) {
				taskLog.Warn("infisical secret env: blocked key skipped", "key", key)
				continue
			}
			env[key] = secret.Value
		}
	}
	return env, nil
}
