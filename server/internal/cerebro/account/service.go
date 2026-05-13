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
	EventAccountUpdated = "account:updated"
)

var (
	ErrAccountNotFound      = errors.New("account not found")
	ErrInvalidProvider      = errors.New("provider is required")
	ErrInvalidLoginIdentity = errors.New("login_identity is required")
	ErrAccountAlreadyExists = errors.New("account already exists for this provider and login_identity")
	ErrInvalidUsagePct      = errors.New("usage_window_pct must be between 0 and 100")
)

type Service struct {
	Cerebro *cerebrodb.Queries
	Bus     *events.Bus
}

func NewService(cerebro *cerebrodb.Queries, bus *events.Bus) *Service {
	return &Service{Cerebro: cerebro, Bus: bus}
}

func (s *Service) List(ctx context.Context, workspaceID pgtype.UUID) ([]Account, error) {
	rows, err := s.Cerebro.ListCerebroAccounts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}
	out := make([]Account, len(rows))
	for i, r := range rows {
		out[i] = accountFromList(r)
	}
	return out, nil
}

func (s *Service) ListWithAvailability(ctx context.Context, workspaceID pgtype.UUID) ([]cerebrodb.ListCerebroAccountsWithAvailabilityRow, error) {
	return s.Cerebro.ListCerebroAccountsWithAvailability(ctx, workspaceID)
}

func (s *Service) Get(ctx context.Context, workspaceID, accountID pgtype.UUID) (Account, error) {
	r, err := s.Cerebro.GetCerebroAccount(ctx, accountID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, err
	}
	a := accountFromGet(r)
	if a.WorkspaceID != workspaceID {
		return Account{}, ErrAccountNotFound
	}
	return a, nil
}

func (s *Service) Create(ctx context.Context, workspaceID, actorID pgtype.UUID, provider, loginIdentity string) (Account, error) {
	provider = strings.TrimSpace(provider)
	loginIdentity = strings.TrimSpace(loginIdentity)
	if provider == "" {
		return Account{}, ErrInvalidProvider
	}
	if loginIdentity == "" {
		return Account{}, ErrInvalidLoginIdentity
	}

	r, err := s.Cerebro.CreateCerebroAccount(ctx, cerebrodb.CreateCerebroAccountParams{
		WorkspaceID:   workspaceID,
		Provider:      provider,
		LoginIdentity: loginIdentity,
	})
	if err != nil {
		if isUniqueViolation(err) {
			return Account{}, ErrAccountAlreadyExists
		}
		return Account{}, err
	}
	a := accountFromCreate(r)
	s.publish(EventAccountCreated, workspaceID, actorID, "member", map[string]any{"account": accountResponseFromModel(a)})
	return a, nil
}

// Upsert creates a cerebro_account row for (workspace_id, provider,
// login_identity) if one does not already exist, otherwise bumps updated_at
// and returns the existing row. Intended for the daemon-driven registration
// path where the same identity is reported on every heartbeat — callers do
// not need to distinguish "created" from "already existed". When a fresh
// row is inserted EventAccountCreated is published; the bumped-updated_at
// case is silent to avoid heartbeat-spam on the event bus.
func (s *Service) Upsert(ctx context.Context, workspaceID pgtype.UUID, provider, loginIdentity string) (Account, bool, error) {
	provider = strings.TrimSpace(provider)
	loginIdentity = strings.TrimSpace(loginIdentity)
	if provider == "" {
		return Account{}, false, ErrInvalidProvider
	}
	if loginIdentity == "" {
		return Account{}, false, ErrInvalidLoginIdentity
	}

	r, err := s.Cerebro.UpsertCerebroAccount(ctx, cerebrodb.UpsertCerebroAccountParams{
		WorkspaceID:   workspaceID,
		Provider:      provider,
		LoginIdentity: loginIdentity,
	})
	if err != nil {
		return Account{}, false, err
	}
	a := Account{ID: r.ID, WorkspaceID: r.WorkspaceID, Provider: r.Provider, LoginIdentity: r.LoginIdentity, CreatedAt: r.CreatedAt, UpdatedAt: r.UpdatedAt}
	created := r.CreatedAt.Valid && r.UpdatedAt.Valid && r.CreatedAt.Time.Equal(r.UpdatedAt.Time)
	if created {
		s.publish(EventAccountCreated, workspaceID, pgtype.UUID{}, "daemon", map[string]any{"account": accountResponseFromModel(a)})
	}
	return a, created, nil
}

func (s *Service) Delete(ctx context.Context, workspaceID, actorID, accountID pgtype.UUID) (Account, error) {
	a, err := s.Get(ctx, workspaceID, accountID)
	if err != nil {
		return Account{}, err
	}
	if err := s.Cerebro.DeleteCerebroAccount(ctx, accountID); err != nil {
		return Account{}, err
	}
	s.publish(EventAccountDeleted, workspaceID, actorID, "member", map[string]any{"account": accountResponseFromModel(a)})
	return a, nil
}

// UsageUpdate is the partial input for UpdateUsage. A nil pointer means
// "leave that column alone"; a non-nil pointer with a non-nil inner value
// is a write; a non-nil pointer with a nil inner value clears the column.
type UsageUpdate struct {
	UsageWindowPct *NullableFloat32
	ThrottledUntil *NullableTime
}

// NullableFloat32 / NullableTime distinguish "absent from the patch" (the
// outer pointer is nil) from "explicitly clear this column" (outer pointer
// non-nil, inner Value nil). Using a single *float32 / *time.Time can't
// express "clear" vs. "leave alone" with a single value.
type NullableFloat32 struct {
	Value *float32
}

type NullableTime struct {
	Value *pgtype.Timestamptz
}

// UpdateUsage applies daemon-driven usage telemetry. Workspace-scoped:
// returns ErrAccountNotFound if the account belongs to a different workspace.
func (s *Service) UpdateUsage(ctx context.Context, workspaceID, actorID, accountID pgtype.UUID, actorType string, update UsageUpdate) (Account, error) {
	existing, err := s.Get(ctx, workspaceID, accountID)
	if err != nil {
		return Account{}, err
	}
	params := cerebrodb.UpdateCerebroAccountUsageParams{ID: existing.ID}
	if update.UsageWindowPct != nil {
		params.UsageWindowPctSet = true
		if v := update.UsageWindowPct.Value; v != nil {
			if *v < 0 || *v > 100 {
				return Account{}, ErrInvalidUsagePct
			}
			params.UsageWindowPct = pgtype.Float4{Float32: *v, Valid: true}
		}
	}
	if update.ThrottledUntil != nil {
		params.ThrottledUntilSet = true
		if v := update.ThrottledUntil.Value; v != nil {
			params.ThrottledUntil = *v
		}
	}
	if !params.UsageWindowPctSet && !params.ThrottledUntilSet {
		return existing, nil
	}
	r, err := s.Cerebro.UpdateCerebroAccountUsage(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, err
	}
	a := accountFromUpdateUsage(r)
	s.publish(EventAccountUpdated, workspaceID, actorID, actorType, map[string]any{"account": accountResponseFromModel(a)})
	return a, nil
}

// ControlsUpdate is the partial input for UpdateControls. Same nil / set
// semantics as UsageUpdate but on bool columns (always non-NULL in the
// schema, so no inner pointer needed).
type ControlsUpdate struct {
	ExtraSpendOn *bool
	PausedManual *bool
}

// UpdateControls applies UI-driven control toggles.
func (s *Service) UpdateControls(ctx context.Context, workspaceID, actorID, accountID pgtype.UUID, update ControlsUpdate) (Account, error) {
	existing, err := s.Get(ctx, workspaceID, accountID)
	if err != nil {
		return Account{}, err
	}
	params := cerebrodb.UpdateCerebroAccountControlsParams{ID: existing.ID}
	if update.ExtraSpendOn != nil {
		params.ExtraSpendOnSet = true
		params.ExtraSpendOn = *update.ExtraSpendOn
	}
	if update.PausedManual != nil {
		params.PausedManualSet = true
		params.PausedManual = *update.PausedManual
	}
	if !params.ExtraSpendOnSet && !params.PausedManualSet {
		return existing, nil
	}
	r, err := s.Cerebro.UpdateCerebroAccountControls(ctx, params)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Account{}, ErrAccountNotFound
		}
		return Account{}, err
	}
	a := accountFromUpdateControls(r)
	s.publish(EventAccountUpdated, workspaceID, actorID, "member", map[string]any{"account": accountResponseFromModel(a)})
	return a, nil
}

func (s *Service) publish(eventType string, workspaceID, actorID pgtype.UUID, actorType string, payload any) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        eventType,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   actorType,
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
