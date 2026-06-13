package main

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/integrations/email"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// emailNotifierQueries bridges email.NotifierQueries onto *db.Queries by
// projecting onto the single fields the notifier needs. Keeps the email
// package independent of the db package's row types.
type emailNotifierQueries struct {
	q *db.Queries
}

func (a emailNotifierQueries) GetUserEmail(ctx context.Context, userID pgtype.UUID) (string, error) {
	u, err := a.q.GetUser(ctx, userID)
	if err != nil {
		return "", err
	}
	return u.Email, nil
}

func (a emailNotifierQueries) GetNotificationPreference(ctx context.Context,
	arg email.GetNotificationPreferenceParams) ([]byte, error) {
	row, err := a.q.GetNotificationPreference(ctx, db.GetNotificationPreferenceParams{
		WorkspaceID: arg.WorkspaceID,
		UserID:      arg.UserID,
	})
	if err != nil {
		return nil, err
	}
	return row.Preferences, nil
}

func (a emailNotifierQueries) GetWorkspaceSlug(ctx context.Context, wsID pgtype.UUID) (string, error) {
	w, err := a.q.GetWorkspace(ctx, wsID)
	if err != nil {
		return "", err
	}
	return w.Slug, nil
}
