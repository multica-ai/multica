package runcontrol

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// ProfileSnapshot supplies both the accepted digest and the actual claim
// payload. A second read must never validate an older, separately built payload.
type ProfileSnapshot struct {
	BuiltinHash string
	Agent       db.Agent
	Runtime     db.AgentRuntime
	Bindings    []db.ListEnabledAgentMcpServersRow
	Skills      []db.Skill
	Files       []db.SkillFile
}

func LoadProfileSnapshot(ctx context.Context, q *db.Queries, a db.Agent) (ProfileSnapshot, error) {
	s := ProfileSnapshot{Agent: a}
	var err error
	s.Runtime, err = q.GetAgentRuntime(ctx, a.RuntimeID)
	if err != nil {
		return s, err
	}
	if s.Runtime.ProfileID.Valid {
		return s, errors.New("controller v1 does not support mutable custom runtime-profile commands")
	}
	if !a.Model.Valid || a.Model.String == "" {
		return s, errors.New("controller profiles require an explicit model")
	}
	s.Bindings, err = q.ListEnabledAgentMcpServers(ctx, a.ID)
	if err != nil {
		return s, err
	}
	sort.Slice(s.Bindings, func(i, j int) bool { return Digest(s.Bindings[i]) < Digest(s.Bindings[j]) })
	s.Skills, err = q.ListAgentSkills(ctx, a.ID)
	if err != nil {
		return s, err
	}
	sort.Slice(s.Skills, func(i, j int) bool { return Digest(s.Skills[i].ID) < Digest(s.Skills[j].ID) })
	ids := make([]pgtype.UUID, 0, len(s.Skills))
	for _, skill := range s.Skills {
		ids = append(ids, skill.ID)
	}
	s.Files, err = q.ListSkillFilesBySkillIDs(ctx, ids)
	if err != nil {
		return s, err
	}
	sort.Slice(s.Files, func(i, j int) bool { return Digest(s.Files[i]) < Digest(s.Files[j]) })
	return s, nil
}

func (s ProfileSnapshot) Hash() string {
	a := s.Agent
	rt := s.Runtime
	return Digest([]any{s.BuiltinHash, a.Name, a.SystemKey, rt.ID, rt.Provider, rt.DaemonID, rt.OwnerID, s.Bindings, a.ID, a.WorkspaceID, a.RuntimeID, a.RuntimeMode, a.RuntimeConfig, a.Instructions, a.Description, a.CustomEnv, a.CustomArgs, a.McpConfig, a.Model, a.ThinkingLevel, a.ServiceTier, a.DisabledRuntimeSkills, a.ComposioToolkitAllowlist, a.PermissionMode, s.Skills, s.Files})
}

func ProfileHash(ctx context.Context, q *db.Queries, a db.Agent) (string, error) {
	s, err := LoadProfileSnapshot(ctx, q, a)
	if err != nil {
		return "", err
	}
	return s.Hash(), nil
}
