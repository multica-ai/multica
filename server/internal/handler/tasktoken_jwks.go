package handler

import (
	"log/slog"
	"net/http"

	"github.com/multica-ai/multica/server/internal/logger"
)

// jwksMaxAge is how long a verifier may cache the key set. Short enough that a
// rotated key propagates on its own, long enough that a busy internal system
// is not refetching it on every incoming request.
const jwksMaxAge = "public, max-age=300"

// GetTaskTokenJWKS serves the public half of the task-token signing key as a
// JWK Set (RFC 7517), so a system that receives these tokens can verify them
// without anyone hand-copying a public key into its configuration.
//
// Unauthenticated on purpose. The response is public key material and nothing
// else — that is what makes it usable by an internal system that has no
// Multica credentials of its own, which is the entire point. Note that the
// key ids configured in the catalog do appear here, so they should not be
// named after anything confidential.
//
// A deployment with task tokens switched off returns 404 rather than an empty
// set: "this server does not issue task tokens" is a more useful answer to an
// operator wiring up a verifier than a document containing no keys.
func (h *Handler) GetTaskTokenJWKS(w http.ResponseWriter, r *http.Request) {
	if h.TaskTokenIssuer == nil {
		writeError(w, http.StatusNotFound, "task tokens are not configured")
		return
	}

	set, err := h.TaskTokenIssuer.JWKS()
	if err != nil {
		slog.Error("task token jwks: cannot publish signing key",
			append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "cannot publish signing key")
		return
	}

	w.Header().Set("Cache-Control", jwksMaxAge)
	writeJSON(w, http.StatusOK, set)
}
