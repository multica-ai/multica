package lark

import (
	"context"
	"log/slog"
	"time"
)

const (
	legacyReceiptBatchSize  = 100
	legacyReceiptMaxBatches = 100
	legacyReceiptTimeout    = 30 * time.Second
)

type legacyReceiptBackfillStore interface {
	BackfillLegacyFeishuInboundReceipts(context.Context, int32) (int64, error)
}

// BackfillLegacyInboundReceipts preserves positive evidence for old bindings at
// rollout. Session routes are created on inbound contact. Only routes created
// since the current install count; new generations always require live receipt.
// At most 10,000 rows and 30 seconds are spent per boot. Remaining eligible rows
// can be picked up on the next boot without overwriting existing evidence.
func BackfillLegacyInboundReceipts(ctx context.Context, store *ChannelStore, log *slog.Logger) {
	if log == nil {
		log = slog.Default()
	}
	ctx, cancel := context.WithTimeout(ctx, legacyReceiptTimeout)
	defer cancel()
	rows, err := backfillLegacyInboundReceipts(ctx, store.Queries)
	if err != nil {
		log.Warn("lark receipt backfill incomplete", "rows", rows, "error", err)
		return
	}
	log.Info("lark receipt backfill finished", "rows", rows, "batch_limit_reached", rows == legacyReceiptBatchSize*legacyReceiptMaxBatches)
}

func backfillLegacyInboundReceipts(ctx context.Context, store legacyReceiptBackfillStore) (int64, error) {
	var rows int64
	for batch := 0; batch < legacyReceiptMaxBatches; batch++ {
		if err := ctx.Err(); err != nil {
			return rows, err
		}
		n, err := store.BackfillLegacyFeishuInboundReceipts(ctx, legacyReceiptBatchSize)
		if err != nil {
			return rows, err
		}
		rows += n
		if n < legacyReceiptBatchSize {
			break
		}
	}
	return rows, nil
}
