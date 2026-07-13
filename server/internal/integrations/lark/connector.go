package lark

import "context"

// EventEmitter is the per-message callback an EventConnector calls for
// each decoded inbound message. It dispatches the normalized message and
// returns the typed outcome plus any infrastructure error.
//
// The connector reacts only to the error: a non-nil error is a real infra
// failure (DB down, dispatcher misconfigured) and the connector should
// surface it and let the engine reconnect under backoff; a nil error means
// the message was accepted and classified (it may still have been dropped
// for a product reason — that is not an error, and any outbound reply the
// verdict implies is handled off the ACK path by the runtime). The
// connector MUST NOT bypass emit by writing to the DB directly; emit is the
// only ingress path.
//
// Historically the Feishu Hub passed this to the connector; with the
// channel-agnostic engine the feishuChannel adapter wraps the connector and
// supplies an emit that normalizes each event and hands it to the engine
// Router (injected via channel.Config.Handler).
type EventEmitter func(ctx context.Context, msg InboundMessage) (DispatchResult, error)

// EventConnector is the per-installation Feishu transport: it opens the
// Lark long connection, decodes events, normalizes them into
// InboundMessage, and calls emit for each. Run MUST block until either ctx
// is cancelled (returns nil) or the connection ends and cannot be recovered
// locally (returns an error). Implementations MUST tolerate repeated Run
// calls on different contexts — the engine may Run, return, and Run again
// after backoff.
type EventConnector interface {
	Run(ctx context.Context, inst Installation, emit EventEmitter) error
}

// ConnectorFactory builds an EventConnector. Kept for the bootstrap /
// fallback path (NoopConnectorFactory); the real WS connector is a single
// shared instance whose Run is parameterized by the installation.
type ConnectorFactory func(inst Installation) (EventConnector, error)
