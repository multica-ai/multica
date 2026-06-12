// Package octo implements the Octo IM integration: per-agent bot installations,
// inbound message dispatch into chat sessions, outbound replies, identity
// binding, and the WebSocket connection hub.
//
// It builds on the WuKongIM transport layer in the transport subpackage
// (github.com/multica-ai/multica/server/internal/integrations/octo/transport)
// and follows the same structural boundaries as the Lark integration
// (internal/integrations/lark). The layering is one-directional:
//
//	transport  — WuKongIM binary protocol (socket) + Octo REST client. No
//	             knowledge of business types.
//	business   — this package. Depends on transport and the generated DB
//	             queries, never the reverse.
//
// The moving parts:
//
//   - Hub (hub.go) owns one WebSocket connection per active installation,
//     coordinated across replicas by a DB lease. It bridges each decoded
//     message to the Dispatcher and forwards the verdict to the OutcomeReplier.
//   - Dispatcher (dispatcher.go) converts an inbound message into
//     chat_session + chat_message rows and enqueues an agent task via the
//     shared service.TaskService, behind a two-phase dedup gate.
//   - Patcher (outbound.go) relays the agent's reply back to Octo on
//     chat:done / task:failed events.
//   - OutcomeReplier (outcome_replier.go) handles the synchronous, pre-agent
//     outcomes: DM an unbound sender a binding link, or notify the user when
//     the agent is offline/archived.
//   - BindingTokenService (binding_token.go) mints and redeems the one-shot
//     tokens behind the {PublicURL}/octo/bind?token= flow.
//   - InstallationService (client.go) creates/revokes installations and
//     encrypts the bot token at rest.
//
// Expired binding tokens and stale inbound-dedup rows are purged by the
// octo_cleanup scheduler job (internal/scheduler/jobs_octo_cleanup.go). The
// DB-layer queries this package depends on are defined in
// pkg/db/queries/octo.sql (migration 119).
package octo
