package octo

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/multica-ai/multica/server/internal/integrations/octo/transport"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// socketConnector is the production Connector: it registers the bot over REST to
// obtain an im_token + ws_url, opens an transport.Socket, and bridges inbound messages
// until the context is cancelled. Reconnect/backoff is handled inside transport.Socket.
type socketConnector struct {
	installations *InstallationService
	logger        *slog.Logger
}

// NewConnectorFactory returns a ConnectorFactory backed by transport.Socket. The
// factory decrypts each installation's bot token via the InstallationService.
func NewConnectorFactory(installations *InstallationService, logger *slog.Logger) ConnectorFactory {
	if logger == nil {
		logger = slog.Default()
	}
	c := &socketConnector{installations: installations, logger: logger}
	return func(inst db.OctoInstallation) (Connector, error) {
		return c, nil
	}
}

// Run registers the bot, opens the WS, and blocks until ctx is cancelled.
func (c *socketConnector) Run(ctx context.Context, inst db.OctoInstallation, onMessage func(transport.BotMessage)) error {
	token, err := c.installations.DecryptBotToken(inst)
	if err != nil {
		return fmt.Errorf("decrypt bot token: %w", err)
	}

	// Register to obtain im_token + ws_url (the bot token alone can't open the WS).
	hc := transport.NewHTTPClient(inst.ApiUrl, token)
	reg, err := hc.Register(ctx, false, "Multica", "")
	if err != nil {
		return fmt.Errorf("register bot: %w", err)
	}

	sock := transport.NewSocket(transport.SocketOptions{
		WSURL:     reg.WSURL,
		UID:       reg.RobotID,
		Token:     reg.IMToken,
		OnMessage: onMessage,
		OnError: func(e error) {
			c.logger.Warn("octo connector: socket error", "installation", uuidString(inst.ID), "err", e.Error())
		},
		Logf: func(format string, args ...any) {
			c.logger.Debug(fmt.Sprintf(format, args...))
		},
	})
	sock.Connect(ctx)
	defer sock.Disconnect()

	<-ctx.Done()
	return ctx.Err()
}
