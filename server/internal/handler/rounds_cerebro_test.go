package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

type failingRoundReplyObserver struct{ called bool }

func (o *failingRoundReplyObserver) ObserveMemberReply(context.Context, pgtype.UUID, pgtype.UUID, pgtype.UUID) error {
	o.called = true
	return errors.New("observer failed")
}

func TestRoundReplyObserverNeverBlocksNormalCommentFlow(t *testing.T) {
	observer := &failingRoundReplyObserver{}
	observeRoundReply(context.Background(), observer, pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{})
	if !observer.called {
		t.Fatal("round reply observer was not called")
	}
}
