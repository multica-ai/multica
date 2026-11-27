package main

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// accessAllowlistEmailLookup resolves the account email behind credentials
// that carry only a user ID. A missing user resolves to "" and is denied.
func accessAllowlistEmailLookup(queries *db.Queries) auth.UserEmailLookup {
	return func(ctx context.Context, userID string) (string, error) {
		id, err := util.ParseUUID(userID)
		if err != nil {
			return "", nil
		}
		user, err := queries.GetUser(ctx, id)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		if err != nil {
			return "", err
		}
		return user.Email, nil
	}
}
