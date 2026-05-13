package runtime

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/cerebro/account"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/util"
)

// EventRuntimeAccountChanged fires when the current_account_id link on a
// runtime row actually moves (first bind or switch to a different account).
// Idempotent re-reports — same account already linked — produce no event.
const EventRuntimeAccountChanged = "runtime:account_changed"

// AccountService owns the daemon-driven registration path: the daemon reports
// "this runtime is logged in as <provider, login_identity>" and we (a) upsert
// the cerebro_account row, (b) link the runtime to it via current_account_id.
//
// The two steps are not transactional — if step (b) fails after step (a) we
// leave a dangling account row, which is harmless: the next heartbeat will
// re-run both. We deliberately do NOT use a tx here so the upsert event can
// fire even when the runtime link path is degraded.
type AccountService struct {
	Cerebro    *cerebrodb.Queries
	AccountSvc *account.Service
	Bus        *events.Bus
}

// NewAccountService constructs the cerebro runtime account service. Called
// from the router during server bootstrap.
func NewAccountService(cerebro *cerebrodb.Queries, accountSvc *account.Service, bus *events.Bus) *AccountService {
	return &AccountService{Cerebro: cerebro, AccountSvc: accountSvc, Bus: bus}
}

// RecordAccount upserts the cerebro_account row for the reported identity
// and links the runtime to it. It is idempotent: re-reporting the same
// identity on every heartbeat is cheap (one UPSERT + one UPDATE-on-distinct).
//
// Returns handler.RuntimeAccountRecord so the upstream HTTP layer can shape
// its response without depending on cerebrodb types. Validation failures are
// surfaced as the handler.ErrRuntimeAccount* sentinels so the HTTP layer maps
// them to 400 via errors.Is.
//
// The workspace argument must be the runtime's owning workspace; the HTTP
// edge validates this via requireDaemonRuntimeAccess.
func (s *AccountService) RecordAccount(
	ctx context.Context,
	runtimeID pgtype.UUID,
	workspaceID pgtype.UUID,
	provider string,
	loginIdentity string,
) (handler.RuntimeAccountRecord, error) {
	acc, created, err := s.AccountSvc.Upsert(ctx, workspaceID, provider, loginIdentity)
	switch {
	case errors.Is(err, account.ErrInvalidProvider):
		return handler.RuntimeAccountRecord{}, handler.ErrRuntimeAccountInvalidProvider
	case errors.Is(err, account.ErrInvalidLoginIdentity):
		return handler.RuntimeAccountRecord{}, handler.ErrRuntimeAccountInvalidLoginIdentity
	case err != nil:
		return handler.RuntimeAccountRecord{}, err
	}

	runtimeUpdated := true
	if _, err := s.Cerebro.SetAgentRuntimeCurrentAccount(ctx, cerebrodb.SetAgentRuntimeCurrentAccountParams{
		ID:               runtimeID,
		CurrentAccountID: acc.ID,
	}); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			// No row was updated — either the runtime is already linked to
			// this account (the common heartbeat case) or the runtime row
			// vanished. Distinguish via a follow-up read: a missing runtime
			// surfaces pgx.ErrNoRows to the caller so a stale daemon can be
			// told to deregister; otherwise treat as an idempotent no-op.
			if _, getErr := s.Cerebro.GetAgentRuntimeCurrentAccountID(ctx, runtimeID); getErr != nil {
				return handler.RuntimeAccountRecord{}, getErr
			}
			runtimeUpdated = false
		} else {
			return handler.RuntimeAccountRecord{}, err
		}
	}

	if runtimeUpdated {
		s.publishRuntimeAccountChanged(workspaceID, runtimeID, acc)
	}

	return handler.RuntimeAccountRecord{
		AccountID:      acc.ID,
		Provider:       acc.Provider,
		LoginIdentity:  acc.LoginIdentity,
		CreatedAt:      acc.CreatedAt,
		UpdatedAt:      acc.UpdatedAt,
		AccountCreated: created,
		RuntimeUpdated: runtimeUpdated,
	}, nil
}

func (s *AccountService) publishRuntimeAccountChanged(workspaceID, runtimeID pgtype.UUID, acc account.Account) {
	if s.Bus == nil {
		return
	}
	s.Bus.Publish(events.Event{
		Type:        EventRuntimeAccountChanged,
		WorkspaceID: util.UUIDToString(workspaceID),
		ActorType:   "system",
		Payload: map[string]any{
			"runtime_id":     util.UUIDToString(runtimeID),
			"account_id":     util.UUIDToString(acc.ID),
			"provider":       acc.Provider,
			"login_identity": acc.LoginIdentity,
		},
	})
}
