// Package agentmemory owns the third memory access gate from the locked spec
// (29/6): a per-(user × agent) toggle. Each user turns memory on for ONE agent,
// only for themselves, via two independent switches — may READ memory and may
// WRITE memory. Both default OFF.
//
// Default-off is encoded as absence-of-row in cerebro_user_agent_memory_settings:
// a missing row means {read:false, write:false}, so GetSettings maps pgx.ErrNoRows
// to the zero Settings rather than an error. A row exists only once a user has
// enabled at least one switch on an agent.
//
// This gate is layered UNDER the workspace feature flag (cerebro_memory) and the
// group capability (create_memory): the HTTP layer checks those first, then this
// service records/reads the per-user-per-agent preference. The gate applies to
// private (member/agent) memory only — company memory is readable by every agent
// once the workspace flag is on.
package agentmemory

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

// EventUserAgentMemoryChanged is broadcast when a user flips a memory switch on
// an agent. Per-user state, so the HTTP caller restricts the audience to the
// acting user's own sessions — nobody else needs to learn about it.
const EventUserAgentMemoryChanged = "cerebro_user_agent_memory_changed"

// Settings is the per-(user × agent) memory preference. Both default false.
type Settings struct {
	CanReadMemory  bool `json:"can_read_memory"`
	CanWriteMemory bool `json:"can_write_memory"`
}

// Querier is the slice of cerebrodb.Queries this service needs. Declaring it as
// an interface keeps the service unit-testable without a database. The concrete
// *cerebrodb.Queries satisfies it.
type Querier interface {
	GetUserAgentMemorySettings(ctx context.Context, arg cerebrodb.GetUserAgentMemorySettingsParams) (cerebrodb.GetUserAgentMemorySettingsRow, error)
	UpsertUserAgentMemorySettings(ctx context.Context, arg cerebrodb.UpsertUserAgentMemorySettingsParams) (cerebrodb.UpsertUserAgentMemorySettingsRow, error)
	DeleteUserAgentMemorySettings(ctx context.Context, arg cerebrodb.DeleteUserAgentMemorySettingsParams) error
}

// Service is the data layer for the per-user-per-agent memory toggle.
type Service struct {
	Queries Querier
	Bus     *events.Bus
}

// New constructs a Service. The router wires the concrete *cerebrodb.Queries.
func New(queries Querier, bus *events.Bus) *Service {
	return &Service{Queries: queries, Bus: bus}
}

// GetSettings returns the user's memory switches for one agent. A missing row
// is the default-off case and returns the zero Settings, not an error.
func (s *Service) GetSettings(ctx context.Context, userID, agentID pgtype.UUID) (Settings, error) {
	row, err := s.Queries.GetUserAgentMemorySettings(ctx, cerebrodb.GetUserAgentMemorySettingsParams{
		UserID:  userID,
		AgentID: agentID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Settings{}, nil
		}
		return Settings{}, err
	}
	return Settings{CanReadMemory: row.CanReadMemory, CanWriteMemory: row.CanWriteMemory}, nil
}

// SetSettings upserts the user's switches for one agent. When both switches are
// off it deletes the row instead of storing an all-false row, so "default off"
// stays represented by absence and the table holds only meaningful opt-ins.
func (s *Service) SetSettings(ctx context.Context, workspaceID, userID, agentID pgtype.UUID, next Settings) (Settings, error) {
	if !next.CanReadMemory && !next.CanWriteMemory {
		if err := s.Queries.DeleteUserAgentMemorySettings(ctx, cerebrodb.DeleteUserAgentMemorySettingsParams{
			UserID:  userID,
			AgentID: agentID,
		}); err != nil {
			return Settings{}, err
		}
		s.publish(workspaceID, userID, agentID, Settings{})
		return Settings{}, nil
	}
	row, err := s.Queries.UpsertUserAgentMemorySettings(ctx, cerebrodb.UpsertUserAgentMemorySettingsParams{
		UserID:         userID,
		AgentID:        agentID,
		CanReadMemory:  next.CanReadMemory,
		CanWriteMemory: next.CanWriteMemory,
	})
	if err != nil {
		return Settings{}, err
	}
	out := Settings{CanReadMemory: row.CanReadMemory, CanWriteMemory: row.CanWriteMemory}
	s.publish(workspaceID, userID, agentID, out)
	return out, nil
}

func (s *Service) publish(workspaceID, userID, agentID pgtype.UUID, settings Settings) {
	if s.Bus == nil {
		return
	}
	userStr := util.UUIDToString(userID)
	s.Bus.Publish(events.Event{
		Type:        EventUserAgentMemoryChanged,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "member",
		ActorID:     userStr,
		Payload: map[string]any{
			"user_id":          userStr,
			"agent_id":         util.UUIDToString(agentID),
			"can_read_memory":  settings.CanReadMemory,
			"can_write_memory": settings.CanWriteMemory,
		},
		AudienceUserIDs: []string{userStr},
	})
}
