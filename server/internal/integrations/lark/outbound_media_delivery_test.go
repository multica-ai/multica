package lark

import (
	"context"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func (f *fakePatcherQueries) GetChannelReplyTurn(_ context.Context, id pgtype.UUID) (db.GetChannelReplyTurnRow, error) {
	return db.GetChannelReplyTurnRow{TurnID: id}, nil
}
func (f *fakePatcherQueries) AcquireChannelReplyDelivery(_ context.Context, arg db.AcquireChannelReplyDeliveryParams) (db.ChannelReplyDelivery, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.mediaLedgerErr != nil {
		return db.ChannelReplyDelivery{}, f.mediaLedgerErr
	}
	if f.mediaLedger == nil {
		f.mediaLedger = map[string]db.ChannelReplyDelivery{}
	}
	key := uuidString(arg.TurnID)
	row := f.mediaLedger[key]
	if row.Phase == "settled" || row.OwnerToken.Valid {
		return db.ChannelReplyDelivery{}, pgx.ErrNoRows
	}
	row.TurnID, row.TaskID, row.OwnerToken, row.Phase = arg.TurnID, arg.TaskID, arg.OwnerToken, arg.Phase
	f.mediaLedger[key] = row
	return row, nil
}
func (f *fakePatcherQueries) MarkChannelReplyDeliverySending(_ context.Context, arg db.MarkChannelReplyDeliverySendingParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := uuidString(arg.TurnID)
	row := f.mediaLedger[key]
	if row.OwnerToken != arg.OwnerToken || row.SendState == "in_flight" || row.SendState == "unknown" {
		return 0, nil
	}
	row.SendState = "in_flight"
	f.mediaLedger[key] = row
	return 1, nil
}

func (f *fakePatcherQueries) RenewChannelReplyDelivery(_ context.Context, arg db.RenewChannelReplyDeliveryParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	row := f.mediaLedger[uuidString(arg.TurnID)]
	if row.OwnerToken != arg.OwnerToken || row.Phase == "settled" {
		return 0, nil
	}
	return 1, f.mediaLedgerErr
}
func (f *fakePatcherQueries) SettleChannelReplyDelivery(_ context.Context, arg db.SettleChannelReplyDeliveryParams) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	key := uuidString(arg.TurnID)
	row := f.mediaLedger[key]
	if row.OwnerToken != arg.OwnerToken {
		return 0, nil
	}
	row.Phase, row.OwnerToken = "settled", pgtype.UUID{}
	f.mediaLedger[key] = row
	return 1, nil
}
