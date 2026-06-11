package handler

import (
	"net/http"
	"strings"
	"time"

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/storage"
)

// ServeLocalUpload returns an http.HandlerFunc that authorizes a request for
// /uploads/<key> on the local-storage backend.
//
// Two acceptable auth paths:
//
//  1. Signed query string (?exp=&sig=) — minted by attachmentToResponse for
//     LocalStorage and bound to one specific key with a short TTL.
//     This is the only path that works for token-auth clients (Desktop,
//     legacy-token Web sessions, native mobile) because browsers do not
//     attach Authorization headers to native <img>/<video>/<iframe>
//     resource loads. Mirror of the S3 + CloudFront presigned-URL flow.
//     When valid, the request is served straight away — no further
//     workspace membership lookup, the signature itself is the authority
//     (the original metadata request that minted it already enforced
//     membership).
//
//  2. Bearer / cookie via middleware.Auth — used for direct fetches from
//     authenticated clients (server-side rendering, CLI, cookie-mode Web).
//     middleware.Auth must be applied to the chain before this handler;
//     here we rely on X-User-ID being set, then run the workspace /
//     user-prefix membership check.
//
// Path layout follows handler.UploadFile:
//
//   - workspaces/{workspaceID}/{filename}  → membership-gated read
//   - users/{userID}/{filename}            → any authenticated user
//
// Anything else is rejected with 404. The disclosure
// (security-findings-2026-06-02) called out that /uploads/* being
// unauthenticated was one of the layers that made the SVG-XSS chain
// weaponizable end-to-end; this handler closes that gap while preserving
// inline image rendering for token-auth clients via the signed-query path.
func (h *Handler) ServeLocalUpload(local *storage.LocalStorage) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.URL.Path, "/uploads/")
		// Reject empty / directory-style paths up front. The storage
		// layer will also catch this; rejecting here keeps the 404
		// shape identical to the unrelated 404s on non-existent
		// keys, denying the directory-existence oracle.
		if key == "" || strings.HasSuffix(key, "/") {
			http.NotFound(w, r)
			return
		}

		// (1) Signed-query auth path. If the client included exp / sig
		// query params, treat that as their chosen authentication
		// method and fail closed if either is missing or invalid.
		// We deliberately do NOT fall through to Bearer / cookie on a
		// broken signature: a leaked URL with an expired sig should
		// not silently start working again because the leaker also
		// happens to be authenticated. The frontend should re-fetch
		// the attachment metadata and get a fresh URL.
		if exp, sig := storage.LocalUploadSignatureFromQuery(r.URL.Query()); exp != "" || sig != "" {
			if exp == "" || sig == "" {
				http.Error(w, `{"error":"missing exp or sig"}`, http.StatusUnauthorized)
				return
			}
			if !storage.VerifyLocalUploadSignature(key, exp, sig, auth.JWTSecret(), time.Now()) {
				http.Error(w, `{"error":"signed URL expired or invalid"}`, http.StatusUnauthorized)
				return
			}
			local.ServeFile(w, r, key)
			return
		}

		// (2) Bearer / cookie auth path. middleware.Auth has already
		// stamped X-User-ID by the time we get here.
		userID, ok := requireUserID(w, r)
		if !ok {
			return
		}

		switch {
		case strings.HasPrefix(key, "workspaces/"):
			rest := strings.TrimPrefix(key, "workspaces/")
			slash := strings.Index(rest, "/")
			if slash <= 0 {
				// "workspaces/" or "workspaces/<id>" with no file —
				// directory listing in disguise.
				http.NotFound(w, r)
				return
			}
			workspaceID := rest[:slash]
			if !h.canReadWorkspaceUpload(r, userID, workspaceID) {
				// 404 rather than 403 so the absence of a workspace
				// and the lack of membership look identical from the
				// outside — denies the IDOR oracle.
				http.NotFound(w, r)
				return
			}

		case strings.HasPrefix(key, "users/"):
			// Avatars and similar user-scoped assets. Any
			// authenticated user can read these — they are
			// routinely embedded in cross-workspace surfaces (member
			// lists, inbox items, mention chips). The auth gate
			// above is the access boundary; we don't gate on a
			// userID match.

		default:
			// Unknown prefix — don't serve. New upload key shapes
			// must opt in here explicitly so they can't inherit a
			// relaxed policy by accident.
			http.NotFound(w, r)
			return
		}

		local.ServeFile(w, r, key)
	}
}

// canReadWorkspaceUpload returns true when the user is a member of the
// workspace whose ID is embedded in a /uploads/workspaces/{id}/* path.
// Uses the membership cache when available so every image fetch on a
// busy issue page doesn't hit Postgres. The cache itself nil-handles,
// so the explicit checks below are only for the empty-string inputs.
func (h *Handler) canReadWorkspaceUpload(r *http.Request, userID, workspaceID string) bool {
	if workspaceID == "" || userID == "" {
		return false
	}
	if h.MembershipCache.Get(r.Context(), userID, workspaceID) {
		return true
	}
	if _, err := h.getWorkspaceMember(r.Context(), userID, workspaceID); err != nil {
		return false
	}
	h.MembershipCache.Set(r.Context(), userID, workspaceID)
	return true
}
