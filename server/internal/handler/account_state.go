package handler

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// AccountStateLoader reads auth.AccountState from the user row. A user that
// does not exist has no suspension: credentials for it fail elsewhere.
func AccountStateLoader(queries *db.Queries) auth.AccountStateLoader {
	if queries == nil {
		return nil
	}
	return func(ctx context.Context, userID string) (auth.AccountState, error) {
		id, err := util.ParseUUID(userID)
		if err != nil {
			return auth.AccountState{}, nil
		}
		row, err := queries.GetUserAccountState(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return auth.AccountState{}, nil
		}
		if err != nil {
			return auth.AccountState{}, err
		}
		return auth.AccountState{Suspended: row.Suspended, TokenVersion: row.TokenVersion}, nil
	}
}
