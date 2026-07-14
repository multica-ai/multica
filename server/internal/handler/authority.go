package handler

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/multica-ai/multica/server/internal/authority"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

const (
	authorityRequestMaxBytes = 256
	authorityNonceTTL        = 10 * time.Minute
	authorityHandlerTimeout  = 5 * time.Second
)

type authorityAttestRequest struct {
	Nonce string `json:"nonce"`
}

func (h *Handler) AttestAuthority(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if h.AuthoritySigner == nil {
		writeError(w, http.StatusServiceUnavailable, "authority attestation is not configured")
		return
	}
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if h.AuthorityRateLimiter == nil {
		writeError(w, http.StatusServiceUnavailable, "authority rate limiter is unavailable")
		return
	}
	clientKey := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		clientKey = host
	}
	if !h.AuthorityRateLimiter.Allow(r.Context(), clientKey) {
		writeError(w, http.StatusTooManyRequests, "rate limit exceeded")
		return
	}

	body := http.MaxBytesReader(w, r.Body, authorityRequestMaxBytes)
	defer body.Close()
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var req authorityAttestRequest
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	nonceBytes, err := authority.ValidateNonce(req.Nonce)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), authorityHandlerTimeout)
	defer cancel()

	tx, err := h.TxStarter.Begin(ctx)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin authority attestation")
		return
	}
	defer tx.Rollback(ctx)
	qtx := h.Queries.WithTx(tx)

	_ = qtx.DeleteExpiredAuthorityNonces(ctx)
	nonceHash := sha256.Sum256(nonceBytes)
	claimed, err := qtx.ClaimAuthorityNonce(ctx, db.ClaimAuthorityNonceParams{
		NonceHash: nonceHash[:],
		Ttl: pgtype.Interval{
			Microseconds: authorityNonceTTL.Microseconds(),
			Valid:        true,
		},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to claim authority nonce")
		return
	}
	if claimed == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{
			"code":  "authority_nonce_replay",
			"error": "nonce has already been used",
		})
		return
	}

	var dbIdentity authority.DBIdentity
	if err := tx.QueryRow(ctx, `
		SELECT
			(pg_control_system()).system_identifier::text,
			d.oid::int8,
			current_database()::text
		FROM pg_catalog.pg_database d
		WHERE d.datname = current_database()
	`).Scan(&dbIdentity.SystemIdentifier, &dbIdentity.DatabaseOID, &dbIdentity.DatabaseName); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read database identity")
		return
	}
	stmt := authority.Statement{
		Protocol:     authority.ProtocolVersion,
		Nonce:        req.Nonce,
		AuthorityID:  h.AuthoritySigner.AuthorityID,
		DBIdentity:   dbIdentity,
		IssuedAt:     time.Now().UTC(),
		ServerCommit: h.ServerCommit,
	}
	att, err := h.AuthoritySigner.Sign(stmt)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to sign authority attestation")
		return
	}
	if err := tx.Commit(ctx); err != nil {
		if errors.Is(err, pgx.ErrTxClosed) {
			writeError(w, http.StatusInternalServerError, "failed to commit authority attestation")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to commit authority attestation")
		return
	}

	writeJSON(w, http.StatusOK, att)
}
