package handler

import (
	"context"

	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/util"
)

// taskCancellationActor snapshots the authenticated actor's display name for
// run history. Identity resolution is best-effort display metadata: the actor
// type and id still persist if the name lookup loses a concurrent race.
func (h *Handler) taskCancellationActor(
	ctx context.Context,
	actorType string,
	actorID string,
) service.TaskCancellationActor {
	actor := service.TaskCancellationActor{Type: actorType}
	id, err := util.ParseUUID(actorID)
	if err != nil {
		return actor
	}
	actor.ID = id
	switch actorType {
	case "member":
		if user, err := h.Queries.GetUser(ctx, id); err == nil {
			actor.Name = user.Name
		}
	case "agent":
		if agent, err := h.Queries.GetAgent(ctx, id); err == nil {
			actor.Name = agent.Name
		}
	}
	return actor
}
