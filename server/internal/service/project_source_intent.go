package service

import (
	"context"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/util"
)

type sourceDecisionsKey struct{}

func WithProjectSourceDecisions(ctx context.Context, decisions map[string]string) context.Context {
	return context.WithValue(ctx, sourceDecisionsKey{}, decisions)
}
func noCodeOnce(ctx context.Context, projectID pgtype.UUID) pgtype.Bool {
	decisions, _ := ctx.Value(sourceDecisionsKey{}).(map[string]string)
	yes := decisions[util.UUIDToString(projectID)] == "no_code_once"
	return pgtype.Bool{Bool: yes, Valid: yes}
}
