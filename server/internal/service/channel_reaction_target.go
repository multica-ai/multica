package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// CaptureChannelReactionTargets runs in the cancellation transaction, before
// deleting bindings and delivery rows. Pass its result to BroadcastCancelledTasks
// after commit, so a different process can clean up without any Add state.
func CaptureChannelReactionTargets(ctx context.Context, q interface {
	GetChannelTaskDelivery(context.Context, pgtype.UUID) (db.ChannelTaskDelivery, error)
}, tasks []db.AgentTaskQueue) (map[string]*events.ChannelReactionTarget, error) {
	targets := make(map[string]*events.ChannelReactionTarget)
	for _, task := range tasks {
		delivery, err := q.GetChannelTaskDelivery(ctx, task.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("capture channel reaction target: %w", err)
		}
		if !delivery.ChannelMessageID.Valid || delivery.ChannelMessageID.String == "" {
			continue
		}
		targets[util.UUIDToString(task.ID)] = &events.ChannelReactionTarget{
			ChannelType:    delivery.ChannelType,
			InstallationID: util.UUIDToString(delivery.InstallationID),
			MessageID:      delivery.ChannelMessageID.String,
		}
	}
	return targets, nil
}
