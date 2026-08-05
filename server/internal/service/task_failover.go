package service

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/taskfailure"
)

type agentRuntimeFailoverConfig struct {
	Failover struct {
		RuntimeIDs []string `json:"runtime_ids"`
	} `json:"failover"`
}

func configuredFailoverRuntimeIDs(raw []byte) []pgtype.UUID {
	var cfg agentRuntimeFailoverConfig
	if len(raw) == 0 || json.Unmarshal(raw, &cfg) != nil {
		return nil
	}

	ids := make([]pgtype.UUID, 0, len(cfg.Failover.RuntimeIDs))
	seen := make(map[[16]byte]struct{}, len(cfg.Failover.RuntimeIDs))
	for _, rawID := range cfg.Failover.RuntimeIDs {
		id, err := util.ParseUUID(rawID)
		if err != nil {
			continue
		}
		if _, duplicate := seen[id.Bytes]; duplicate {
			continue
		}
		seen[id.Bytes] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

// RuntimeResumeCompatible limits cross-runtime continuation to another account
// served by the same daemon and provider. That guarantees a shared filesystem
// for workdir reuse while preventing session ids from crossing workspace,
// provider, machine, or Multica-owner boundaries.
func RuntimeResumeCompatible(source, target db.AgentRuntime) bool {
	return source.WorkspaceID.Valid && source.WorkspaceID == target.WorkspaceID &&
		source.OwnerID.Valid && source.OwnerID == target.OwnerID &&
		source.DaemonID.Valid && source.DaemonID == target.DaemonID &&
		source.Provider != "" && source.Provider == target.Provider
}

func isFailoverOnlyRetryReason(reason string) bool {
	return reason == string(taskfailure.ReasonAgentProviderQuotaLimit) ||
		reason == string(taskfailure.ReasonAgentProviderCapacityOrRateLimit)
}

func selectConfiguredFailoverRuntime(currentID pgtype.UUID, configured []pgtype.UUID, runtimes []db.AgentRuntime) (pgtype.UUID, bool) {
	byID := make(map[[16]byte]db.AgentRuntime, len(runtimes))
	for _, runtime := range runtimes {
		if runtime.ID.Valid {
			byID[runtime.ID.Bytes] = runtime
		}
	}
	source, ok := byID[currentID.Bytes]
	if !currentID.Valid || !ok {
		return pgtype.UUID{}, false
	}
	for _, candidateID := range configured {
		if !candidateID.Valid || candidateID == currentID {
			continue
		}
		candidate, ok := byID[candidateID.Bytes]
		if ok && candidate.Status == "online" && RuntimeResumeCompatible(source, candidate) {
			return candidate.ID, true
		}
	}
	return pgtype.UUID{}, false
}

func (s *TaskService) configuredFailoverRuntime(ctx context.Context, agent db.Agent, currentID pgtype.UUID) (pgtype.UUID, bool, error) {
	configured := configuredFailoverRuntimeIDs(agent.RuntimeConfig)
	if len(configured) == 0 {
		return pgtype.UUID{}, false, nil
	}
	ids := make([]pgtype.UUID, 0, len(configured)+1)
	ids = append(ids, currentID)
	ids = append(ids, configured...)
	runtimes, err := s.Queries.GetAgentRuntimes(ctx, ids)
	if err != nil {
		return pgtype.UUID{}, false, err
	}
	target, ok := selectConfiguredFailoverRuntime(currentID, configured, runtimes)
	return target, ok, nil
}
