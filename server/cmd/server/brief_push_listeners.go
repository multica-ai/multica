package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// briefPushEnqueueTimeout bounds the row inserts one publish makes.
const briefPushEnqueueTimeout = 10 * time.Second

// registerBriefPushListeners queues a brief push for each member who chose one
// (plan 9.10). The listener writes rows and does no network I/O: a chat
// platform's latency or failure never reaches the publish, and the retry job
// does the sending. A failed insert is logged and the brief stands.
func registerBriefPushListeners(bus *events.Bus, push *service.BriefPushService) {
	bus.Subscribe(protocol.EventBriefCreated, func(e events.Event) {
		payload, ok := e.Payload.(map[string]any)
		if !ok {
			return
		}
		brief, ok := payload["brief"].(handler.BriefResponse)
		if !ok {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), briefPushEnqueueTimeout)
		defer cancel()
		if _, err := push.EnqueueForBrief(ctx, parseUUID(e.WorkspaceID), parseUUID(brief.ID)); err != nil {
			slog.Error("brief:created push: could not queue deliveries", "brief_id", brief.ID, "error", err)
		}
	})
}
