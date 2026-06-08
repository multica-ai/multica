package wakeup

import (
	"context"
	"log/slog"
	"time"
)

func RunSweeper(ctx context.Context, svc *Service, interval time.Duration) {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rows, err := svc.ClaimDueTime(ctx, 25)
			if err != nil {
				slog.Error("cerebro wakeup sweep failed", "error", err)
				continue
			}
			for _, row := range rows {
				go svc.Dispatch(context.Background(), row)
			}
		}
	}
}
