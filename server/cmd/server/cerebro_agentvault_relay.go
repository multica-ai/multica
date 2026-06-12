package main

// CEREBRO-PATCH(cerebro-agentvault-relay-file): Agent Vault relay listener (TECH-3196).
//
// Starts a dedicated forward-proxy listener, separate from the chi API mux, so
// the CONNECT tunnel bypasses the workspace auth middleware (the relay does its
// own proxy-auth). Agents set HTTPS_PROXY to this listener and authenticate
// with their claim-time minted, vault-scoped token; the relay chains CONNECT to
// the internal Agent Vault MITM proxy, which swaps in the real credential. The
// agent never reaches the internal path nor holds the underlying secret.
//
// No-op unless AGENT_VAULT_RELAY_LISTEN is set AND Agent Vault is configured.

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/cerebro/agentvault"
)

func startAgentVaultRelay(pool *pgxpool.Pool) {
	addr := strings.TrimSpace(os.Getenv("AGENT_VAULT_RELAY_LISTEN"))
	if addr == "" {
		return
	}
	mod, ok := agentvault.NewModule(pool)
	if !ok {
		slog.Warn("agentvault relay: AGENT_VAULT_RELAY_LISTEN set but Agent Vault not configured; relay disabled")
		return
	}
	srv := &http.Server{Addr: addr, Handler: mod.Relay}
	go func() {
		slog.Info("agentvault relay listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("agentvault relay stopped", "error", err)
		}
	}()
}
