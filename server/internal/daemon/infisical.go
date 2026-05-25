package daemon

// CEREBRO-PATCH(daemon-infisical-spawn): inject backend-fetched secrets as env
// at spawn. The daemon does NOT call Infisical itself — the backend resolves
// the agent's grants via the per-user scoped Infisical machine identity at
// claim time and ships the resolved key/value secrets in the claim payload.
// See FIR-2192.

import (
	"context"
	"log/slog"
	"strings"
)

func (d *Daemon) prepareInfisicalSpawn(_ context.Context, agent *AgentData, taskLog *slog.Logger) (map[string]string, error) {
	if agent == nil || len(agent.InfisicalSecrets) == 0 {
		return nil, nil
	}
	env := make(map[string]string, len(agent.InfisicalSecrets))
	for key, value := range agent.InfisicalSecrets {
		k := strings.TrimSpace(key)
		if k == "" {
			continue
		}
		if isBlockedEnvKey(k) {
			taskLog.Warn("infisical secret env: blocked key skipped", "key", k)
			continue
		}
		env[k] = value
	}
	return env, nil
}
