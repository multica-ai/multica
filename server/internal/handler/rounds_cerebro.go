package handler

import (
	"context"

	"github.com/jackc/pgx/v5/pgtype"
)

// RoundCommentGate stores a member reply for the next round instead of
// starting agents immediately. The concrete implementation lives in the
// cerebro rounds package to keep the upstream handler package independent.
type RoundCommentGate interface {
	HoldComment(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID, string, string, string) (bool, error)
}
