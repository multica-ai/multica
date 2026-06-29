package connectiontools

// agent_resolver.go is the production AgentResolver: it resolves the calling
// agent's workspace + runtime + owner from the upstream agent table via
// db.Queries.GetAgent. The handler keeps AgentResolver as an interface so it can
// be unit-tested with a fake; this is the only place the upstream db package is
// touched.

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// agentGetter is the slice of db.Queries the resolver needs.
type agentGetter interface {
	GetAgent(ctx context.Context, id pgtype.UUID) (db.Agent, error)
}

// QueriesAgentResolver resolves agents through *db.Queries (or any agentGetter).
type QueriesAgentResolver struct {
	q agentGetter
}

// NewQueriesAgentResolver wraps the upstream queries as an AgentResolver.
func NewQueriesAgentResolver(q agentGetter) *QueriesAgentResolver {
	return &QueriesAgentResolver{q: q}
}

// ResolveAgent returns the agent's workspace, runtime, and owner ids. A lookup
// error propagates so the caller fails closed.
func (r *QueriesAgentResolver) ResolveAgent(ctx context.Context, agentID pgtype.UUID) (workspaceID, runtimeID, ownerID pgtype.UUID, err error) {
	agent, err := r.q.GetAgent(ctx, agentID)
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, err
	}
	return agent.WorkspaceID, agent.RuntimeID, agent.OwnerID, nil
}
