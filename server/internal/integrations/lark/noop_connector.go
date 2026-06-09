package lark

import (
	"context"
	"log/slog"

	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// NoopConnector satisfies EventConnector by holding the run context
// open without dialing anything and without emitting any inbound
// events. It exists so the Hub can be wired into the boot path before
// the real Lark long-connection client lands: the lease lifecycle,
// supervisor / renewer goroutines, sweep + shutdown plumbing all run
// against a real DB, and the operator sees the Hub's "lease acquired"
// log lines once an installation exists — without the Hub silently
// retrying a half-finished real connector and looking like the wire
// protocol is broken when it has not been implemented yet.
//
// The next stage of the MVP (MUL-2671) replaces this with a connector
// that opens the actual Lark WebSocket long connection, decodes
// events, and calls emit. At that point the NoopConnectorFactory call
// site in router.go swaps to the real factory; the Hub itself does
// not change.
type NoopConnector struct {
	logger *slog.Logger
}

// NewNoopConnector returns a connector that blocks until the Hub
// cancels its run context. Logger may be nil; callers typically pass
// slog.Default.
func NewNoopConnector(logger *slog.Logger) *NoopConnector {
	if logger == nil {
		logger = slog.Default()
	}
	return &NoopConnector{logger: logger}
}

// Run blocks until ctx is cancelled and then returns nil. A nil return
// tells the Hub the connection ended cleanly (no backoff retry storm
// on shutdown / lease loss). Because Run never errors and runs for as
// long as the lease is held, the Hub's "uptime >= ResetBackoffAfter"
// branch will reset the backoff on every supervisor cycle — which is
// the right thing for a placeholder.
func (c *NoopConnector) Run(ctx context.Context, inst db.LarkInstallation, _ EventEmitter) error {
	c.logger.Info("lark noop connector: holding lease (real long-conn not yet implemented)",
		"installation_id", uuidString(inst.ID),
		"app_id", inst.AppID,
	)
	<-ctx.Done()
	c.logger.Info("lark noop connector: exiting on ctx cancel",
		"installation_id", uuidString(inst.ID),
	)
	return nil
}

// NoopConnectorFactory returns a ConnectorFactory that hands every
// installation a NoopConnector sharing the supplied logger. Used by
// the router during the bootstrap stage; replaced by the real
// connector factory once the wire-protocol implementation lands.
func NoopConnectorFactory(logger *slog.Logger) ConnectorFactory {
	c := NewNoopConnector(logger)
	return func(_ db.LarkInstallation) (EventConnector, error) {
		return c, nil
	}
}
