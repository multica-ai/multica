package browsertester

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
)

type ListGroupAgentsFunc func(context.Context, pgtype.UUID) ([]cerebrodb.CerebroGroupAgentAccess, error)

// AgentGroupIDs narrows an owner's browser-testers memberships to groups that
// also explicitly allowlist the calling agent. Both relationships are required
// because several agents can share the same owner.
func AgentGroupIDs(
	ctx context.Context,
	ownerGroups []cerebrodb.CerebroGroup,
	agentID pgtype.UUID,
	listAgents ListGroupAgentsFunc,
) (map[pgtype.UUID]struct{}, error) {
	allowed := make(map[pgtype.UUID]struct{})
	for _, group := range ownerGroups {
		if !strings.EqualFold(strings.TrimSpace(group.Name), "browser-testers") {
			continue
		}
		agents, err := listAgents(ctx, group.ID)
		if err != nil {
			return map[pgtype.UUID]struct{}{}, err
		}
		for _, candidate := range agents {
			if candidate.AgentID == agentID {
				allowed[group.ID] = struct{}{}
				break
			}
		}
	}
	return allowed, nil
}
