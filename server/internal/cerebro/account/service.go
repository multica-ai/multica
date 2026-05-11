package account

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
)

const (
	EventAccountCreated = "account:created"
	EventAccountDeleted = "account:deleted"
)

var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrInvalidProvider      = errors.New("provider is required")
	ErrInvalidLoginIdentity = errors.New("login_identity is required")
	ErrAccountAlreadyExists = errors.New("account already exists for this provider and login_identity")
)

type Service struct {
	Cerebro *cerebrodb.Queries
	Bus     *events.Bus
}

func NewService(cerebro *cerebrodb.Queries, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Bus: bus}
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.CerebroAccount, error) {
	return s.Cerebro.ListCerebroAccounts(ctx, workspaceID)
}

func (s *Service) Get(ctx context.Context, workspaceID, accountID pgtype.UUID) (cerebrodb.CerebroAccount, error) {
	a, err := s.Cerebro.GetCerebroAccount(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return cerebrodb.CerebroAccount{}, ErrAccountNotFound
		}
		return cerebrodb.CerebroAccount{}, err
	}
	if a.WorkspaceID != workspaceID {
		return cerebrodb.CerebroAccount{}, ErrAccountNotFound
	}
	return a, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, actorID pgtype.UUID, provider, loginIdentity string) (cerebrodb.CerebroAccount, error) {
	provider = strings.TrimSpace(provider)
	loginIdentity = strings.TrimSpace(loginIdentity)
	if provider == "" {
		return cerebrodb.CerebroAccount{}, ErrInvalidProvider
	}
	if loginIdentity == "" {
		return cerebrodb.CerebroAccount{}, ErrInvalidLoginIdentity
	}

	a, err := s.Cerebro.CreateCerebroAccount(ctx, cerebrodb.CreateCerebroAccountParams{
		WorkspaceID:   workspaceID,
		Provider:      provider,
		LoginIdentity: loginIdentity,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return cerebrodb.CerebroAccount{}, ErrAccountAlreadyExists
		}
		return cerebrodb.CerebroAccount{}, err
	}
	s.publish(EventAccountCreated, workspaceID, actorID, map[string]any{"account": accountResponseFromModel(a)})
	return a, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, actorID, accountID pgtype.UUID) (cerebrodb.CerebroAccount, error) {
	a, err := s.Get(ctx, workspaceID, accountID)
	if err != nil {
		return cerebrodb.CerebroAccount{}, err
	}
	if err := s.Cerebro.DeleteCerebroAccount(ctx, accountID); err != nil {
		return cerebrodb.CerebroAccount{}, err
	}
	s.publish(EventAccountDeleted, workspaceID, actorID, map[string]any{"account": accountResponseFromModel(a)})
	return a, nil
}

func (s *Service) publish(eventType string, workspaceID, actorID pgtype.UUID, payload any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "member",
		ActorID:     util.UUIDToString(actorID),
		Payload:     payload,
	})
}

func isUniqueViolation(err error) bool {
	const uniqueViolation = "23505"
	type pgErr interface {
		SQLState() string
	}
	var pe pgErr
	if errors.As(err, &pe) {
		return pe.SQLState() == uniqueViolation
	}
	return false
}
