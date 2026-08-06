package wecom

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

// defaultVerifySubscribeTimeout bounds the credential probe. It is shorter
// than ConnConfig's 10s production handshake budget because a user is
// synchronously waiting on the HTTP response: a WeCom endpoint that has not
// answered the handshake in 5s is reported as unreachable rather than held
// open, and the user can retry.
const defaultVerifySubscribeTimeout = 5 * time.Second

// VerifyCredentialsConfig parameterizes the probe. Only BotID and Secret are
// required; the rest exist so tests can point at an httptest server and so a
// private WeCom deployment can override the endpoint.
type VerifyCredentialsConfig struct {
	// DialURL defaults to DefaultDialURL.
	DialURL string
	BotID   string
	Secret  string
	// Dialer defaults to NewGorillaDialer().
	Dialer WSDialer
	// SubscribeTimeout defaults to defaultVerifySubscribeTimeout.
	SubscribeTimeout time.Duration
	Logger           *slog.Logger
}

// VerifyCredentials dials the long-connection endpoint, performs only the
// aibot_subscribe handshake, and closes. It is the manual-install pre-flight:
// a typo'd secret has to fail at submit time, because the alternative is an
// installation whose bot silently never answers — nothing in the product
// surfaces a per-installation auth failure.
//
// Returns *AuthFailedError when WeCom rejected the credentials (non-zero
// errcode). Any other error means the endpoint could not be reached or did not
// complete the handshake, which says nothing about whether the credentials are
// valid — callers must not report those as "wrong secret".
//
// The probe occupies the bot's connection slot for the duration of the
// handshake. That is safe for a fresh install (no supervisor connection exists
// for a bot that is not installed yet) but would kick a live connection, so
// callers must reject an already-connected bot id BEFORE probing.
func VerifyCredentials(ctx context.Context, cfg VerifyCredentialsConfig) error {
	dialURL := cfg.DialURL
	if dialURL == "" {
		dialURL = DefaultDialURL
	}
	timeout := cfg.SubscribeTimeout
	if timeout <= 0 {
		timeout = defaultVerifySubscribeTimeout
	}
	dialer := cfg.Dialer
	if dialer == nil {
		dialer = NewGorillaDialer()
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// NewConn validates the required fields and applies the timing defaults;
	// reusing it keeps the probe's frame encoding identical to the production
	// handshake instead of a parallel copy that can drift.
	conn, err := NewConn(ConnConfig{
		DialURL:          dialURL,
		BotID:            cfg.BotID,
		Secret:           cfg.Secret,
		Dialer:           dialer,
		SubscribeTimeout: timeout,
		Logger:           logger,
	})
	if err != nil {
		return err
	}

	dialCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rawConn, _, err := dialer.DialContext(dialCtx, dialURL, nil)
	if err != nil {
		return fmt.Errorf("wecom verify: dial: %w", err)
	}
	defer func() { _ = rawConn.Close() }()

	// subscribe applies its own SubscribeTimeout deadlines and returns
	// *AuthFailedError for a non-zero errcode. Nothing else runs: no pumps, no
	// ping loop, no callback workers.
	if err := conn.subscribe(dialCtx, rawConn); err != nil {
		var authErr *AuthFailedError
		if errors.As(err, &authErr) {
			return authErr
		}
		return fmt.Errorf("wecom verify: subscribe: %w", err)
	}
	return nil
}
