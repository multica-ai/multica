package main

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// TokenMinter abstracts the GitHub App installation-token minter so it can be
// wired in post-construction (production) or faked in tests.
type TokenMinter interface {
	MintToken(ctx context.Context, installationID int64) (string, error)
}

type Server struct {
	cfg     *Config
	nonces  *NonceStore
	dedup   *DeliveryDedup
	multica *MulticaClient
	minter  TokenMinter
	router  chi.Router
}

func NewServer(cfg *Config) *Server {
	s := &Server{
		cfg:     cfg,
		nonces:  NewNonceStore(5 * time.Minute),
		dedup:   NewDeliveryDedup(24 * time.Hour),
		multica: NewMulticaClient(cfg.MulticaBaseURL, cfg.MulticaPAT, cfg.MulticaWorkspaceID),
	}
	s.router = s.buildRouter()
	return s
}

// SetTokenMinter wires up the GitHub App token minter. Called from main()
// after Server construction so a failure to init the App doesn't block the
// rest of the server from being assembled (and so tests can pass nil/fakes).
func (s *Server) SetTokenMinter(m TokenMinter) { s.minter = m }

// Close releases background resources (sweepers). Safe to call multiple times.
func (s *Server) Close() {
	s.nonces.Close()
	s.dedup.Close()
}

func (s *Server) Router() http.Handler { return s.router }

func (s *Server) buildRouter() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * time.Second))

	r.Get("/healthz", s.handleHealthz)
	r.Post("/webhook/github", s.handleGitHubWebhook)
	r.Get("/installation-token", s.handleInstallationToken)

	return r
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
