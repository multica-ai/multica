package main

import (
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// registerCoreEventListeners wires production event side effects in the order
// they must run. Subscriber listeners come first because notification delivery
// reads the subscriber table during the same synchronous bus dispatch.
func registerCoreEventListeners(bus *events.Bus, queries *db.Queries, pushSvc *service.PushService) {
	registerSubscriberListeners(bus, queries)
	registerNotificationListeners(bus, queries, pushSvc)
	registerActivityListeners(bus, queries)
	registerCerebroNoteMentionListener(bus, queries)             // CEREBRO-PATCH(cerebro-note-mentions-register): TECH-3421 — route note @-mentions to notifications.
	registerCerebroSkillNotificationListener(bus, queries)       // CEREBRO-PATCH(skill-notif-channels): FIR-1587 — route skill change-request/fork/agent-assigned notifications through the channel matrix.
	registerCerebroAgentOfficeNotificationListener(bus, queries) // CEREBRO-PATCH(agent-office-notif-channels): FIR-1775 — route agent-context change-request notifications through the channel matrix.
}
