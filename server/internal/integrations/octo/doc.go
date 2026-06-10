// Package octo implements the Octo IM integration: per-agent bot installations,
// inbound message dispatch into chat sessions, outbound replies, identity
// binding, and the WebSocket connection hub.
//
// It builds on the WuKongIM transport layer in
// github.com/multica-ai/multica/server/internal/integrations/im and follows the
// same structural boundaries as the Lark integration
// (internal/integrations/lark): the dispatcher converts inbound IM messages into
// chat_session + chat_message rows and enqueues agent tasks via the shared
// TaskService; outbound replies are driven by chat:done / task:failed events.
//
// This file currently only declares the package; the implementation lands in
// later phases (dispatcher, outbound, binding, hub). The DB-layer queries it
// depends on are defined in pkg/db/queries/octo.sql (migration 119).
package octo
