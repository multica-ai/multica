package main

import (
	"log/slog"
	"net/http"
)

func (s *Server) handleInstallationToken(w http.ResponseWriter, r *http.Request) {
	if s.minter == nil {
		http.Error(w, "token broker not configured", http.StatusServiceUnavailable)
		return
	}

	nonce := r.URL.Query().Get("nonce")
	if nonce == "" {
		http.Error(w, "missing nonce", http.StatusBadRequest)
		return
	}

	prCtx, ok := s.nonces.Consume(nonce)
	if !ok {
		slog.Info("token request: nonce unknown or expired")
		http.Error(w, "nonce not found or expired", http.StatusNotFound)
		return
	}

	token, err := s.minter.MintToken(r.Context(), prCtx.InstallationID)
	if err != nil {
		slog.Error("token mint failed",
			"installation", prCtx.InstallationID,
			"repo", prCtx.Owner+"/"+prCtx.Repo,
			"pr", prCtx.PRNumber,
			"error", err,
		)
		http.Error(w, "token mint failed", http.StatusInternalServerError)
		return
	}

	slog.Info("token minted",
		"installation", prCtx.InstallationID,
		"repo", prCtx.Owner+"/"+prCtx.Repo,
		"pr", prCtx.PRNumber,
	)

	writeJSON(w, http.StatusOK, map[string]any{
		"token":     token,
		"repo":      prCtx.Owner + "/" + prCtx.Repo,
		"pr_number": prCtx.PRNumber,
		"head_sha":  prCtx.HeadSHA,
	})
}
