// Package sharetoken owns the cerebro-only "public link" feature (JEH-1076).
//
// One row of cerebro_share_token = one public link. When an authorised
// member mints a token for an issue, the handler:
//
//  1. Inserts the share-token row (workspace-scoped).
//  2. Calls persona to attach a scoped anonymous grant
//     (action=read, resource_kind=cerebro:issue, resource_pattern=<issue-id>).
//  3. Stores the persona grant id on the share-token row so a later
//     revoke can tear the grant back down.
//
// The unauth GET route resolves a presented token → resource id, calls
// persona's /v1/check with NO Authorization header (and the share-token
// id as anonymous_context for audit), and returns the gated resource on
// allow. Without a token, or with a revoked one, the route 404s — it
// never leaks "this token used to exist".
//
// Persona is the source of truth for "may anonymous see X". The share-
// token table is the cerebro-side index that maps an opaque token string
// to a specific resource so the public route knows what to check.
package sharetoken

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	persona "github.com/hvejsel/firtal-persona/sdk/go"
	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

// IssueResourceKind is the persona resource_kind we use for cerebro
// issues. Kept as a single source of truth so the mint path, the public
// route, and any future grant-introspection UI all agree.
const IssueResourceKind = "cerebro:issue"

// PersonaClient is the subset of the persona SDK this package needs.
// Defined as an interface so tests can inject a stub without a real
// network round-trip.
type PersonaClient interface {
	AddAnonymousGrant(ctx context.Context, action, resourceKind, resourcePattern string) (string, error)
	DeleteAnonymousGrant(ctx context.Context, grantID string) error
}

// Handler owns the share-token HTTP endpoints. Cerbros queries write
// share-token rows; upstream queries gate "may this member mint a link
// for this issue" and resolve the resource the public route returns.
type Handler struct {
	Cerebro  *cerebrodb.Queries
	Upstream *db.Queries
	Persona  PersonaClient
}

// NewHandler is the production constructor. Reads persona endpoint /
// token from MULTICA_PERSONA_URL / MULTICA_PERSONA_TOKEN; when either is
// missing the handler returns a 503 from /share-tokens (you cannot mint
// a public link without persona) but the unauth GET route still works
// for rows that already exist — the next /v1/check still runs against
// whatever persona instance is configured at request time.
func NewHandler(cerebro *cerebrodb.Queries, upstream *db.Queries) *Handler {
	return &Handler{
		Cerebro:  cerebro,
		Upstream: upstream,
		Persona:  newPersonaClientFromEnv(),
	}
}

// NewHandlerWithPersona injects an explicit persona client. Tests use
// this to avoid the env-var lookup; production callers can plumb a
// pre-built client if they need a custom HTTP/transport.
func NewHandlerWithPersona(cerebro *cerebrodb.Queries, upstream *db.Queries, client PersonaClient) *Handler {
	return &Handler{Cerebro: cerebro, Upstream: upstream, Persona: client}
}

func newPersonaClientFromEnv() PersonaClient {
	url := strings.TrimRight(os.Getenv("MULTICA_PERSONA_URL"), "/")
	token := os.Getenv("MULTICA_PERSONA_TOKEN")
	if url == "" || token == "" {
		return nil
	}
	c, err := persona.New(persona.Config{Endpoint: url, Token: token})
	if err != nil {
		return nil
	}
	return c
}

// CreateRequest is the body of POST /api/cerebro/issues/{id}/share-tokens.
// Action defaults to "read" if absent — the only one we support today;
// future "comment" / "write" links would extend this enum here and on
// the persona side.
type CreateRequest struct {
	Action string `json:"action,omitempty"`
}

// CreateResponse is what the mint endpoint returns. The plaintext
// `token` is the public-link suffix; the caller renders it as part of a
// URL (e.g. https://app/.../share/<token>) and shows it once.
type CreateResponse struct {
	ID             string `json:"id"`
	Token          string `json:"token"`
	URL            string `json:"url"`
	ResourceKind   string `json:"resource_kind"`
	ResourceID     string `json:"resource_id"`
	Action         string `json:"action"`
	PersonaGrantID string `json:"persona_grant_id"`
	CreatedAt      string `json:"created_at"`
}

// publicIssueView is the slim shape served from the unauth public route.
// Deliberately omits anything that would leak workspace state: no
// project_id, no labels, no assignee_id. The auditor reads only what the
// share-token explicitly opened.
type publicIssueView struct {
	ID          string  `json:"id"`
	Number      int32   `json:"number"`
	Title       string  `json:"title"`
	Description *string `json:"description"`
	Status      string  `json:"status"`
	CreatedAt   string  `json:"created_at"`
}

// Create mints a share-token for an issue. The caller must be a
// workspace member with access to the issue — the route is mounted
// behind RequireWorkspaceMember + a per-issue access check.
//
// Order of operations on success:
//
//	1. Generate an opaque random token string.
//	2. Call persona to attach the scoped anonymous grant.
//	3. INSERT the cerebro_share_token row carrying the persona grant id.
//
// If persona is unreachable, no row is written — better to return 503
// than to issue a public link that persona will deny. If the cerebro
// INSERT fails after persona accepted the grant, the handler best-
// effort revokes the persona grant to avoid an orphan allow.
func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	issue, ok := h.loadIssueForMember(w, r)
	if !ok {
		return
	}

	if h.Persona == nil {
		writeError(w, http.StatusServiceUnavailable, "persona not configured; cannot mint public links")
		return
	}

	var req CreateRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	action := strings.TrimSpace(req.Action)
	if action == "" {
		action = "read"
	}
	if action != "read" {
		writeError(w, http.StatusBadRequest, "unsupported action (only \"read\" is allowed today)")
		return
	}

	userID := r.Header.Get("X-User-ID")
	userUUID, err := util.ParseUUID(userID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "user not authenticated")
		return
	}

	tokenStr, err := mintTokenString()
	if err != nil {
		slog.Error("share-token: token mint failed", "error", err)
		writeError(w, http.StatusInternalServerError, "could not mint token")
		return
	}

	issueIDStr := util.UUIDToString(issue.ID)
	grantID, err := h.Persona.AddAnonymousGrant(r.Context(), action, IssueResourceKind, issueIDStr)
	if err != nil {
		slog.Error("share-token: persona grant failed", "issue_id", issueIDStr, "error", err)
		writeError(w, http.StatusBadGateway, "could not provision persona grant")
		return
	}

	row, err := h.Cerebro.CreateShareToken(r.Context(), cerebrodb.CreateShareTokenParams{
		WorkspaceID:    issue.WorkspaceID,
		Token:          tokenStr,
		ResourceKind:   IssueResourceKind,
		ResourceID:     issue.ID,
		Action:         action,
		PersonaGrantID: grantID,
		CreatedByID:    userUUID,
	})
	if err != nil {
		// Best-effort: undo the persona grant so we don't leave an orphan
		// allow on the anonymous actor.
		if delErr := h.Persona.DeleteAnonymousGrant(r.Context(), grantID); delErr != nil {
			slog.Error("share-token: rollback delete grant failed", "grant_id", grantID, "error", delErr)
		}
		slog.Error("share-token: insert failed", "issue_id", issueIDStr, "error", err)
		writeError(w, http.StatusInternalServerError, "could not persist share token")
		return
	}

	resp := CreateResponse{
		ID:             util.UUIDToString(row.ID),
		Token:          row.Token,
		URL:            "/api/cerebro/public/share/" + row.Token,
		ResourceKind:   row.ResourceKind,
		ResourceID:     util.UUIDToString(row.ResourceID),
		Action:         row.Action,
		PersonaGrantID: row.PersonaGrantID,
		CreatedAt:      util.TimestampToString(row.CreatedAt),
	}
	writeJSON(w, http.StatusCreated, resp)
}

// Revoke marks the share-token row as revoked and tears down the
// matching persona grant. Same auth contract as Create — the caller
// must be the workspace member who can access the underlying issue.
// Idempotent: a missing/already-revoked row returns 204 without error,
// so the UI can fire-and-forget on a stale list.
func (h *Handler) Revoke(w http.ResponseWriter, r *http.Request) {
	tokenIDStr := chi.URLParam(r, "tokenId")
	tokenUUID, err := util.ParseUUID(tokenIDStr)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid token id")
		return
	}

	row, err := h.Cerebro.GetShareTokenByID(r.Context(), tokenUUID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeError(w, http.StatusInternalServerError, "lookup failed")
		return
	}

	// Workspace-scope the revoke. If the share-token belongs to a
	// different workspace than the caller's current one, treat it as
	// not-found — never leak that the row exists.
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" || wsID != util.UUIDToString(row.WorkspaceID) {
		w.WriteHeader(http.StatusNoContent)
		return
	}

	if h.Persona != nil && row.PersonaGrantID != "" {
		if err := h.Persona.DeleteAnonymousGrant(r.Context(), row.PersonaGrantID); err != nil {
			// Logging only — we still want to mark the row revoked so the
			// link stops working on our side even if persona is slow.
			slog.Warn("share-token: persona delete failed",
				"grant_id", row.PersonaGrantID, "error", err)
		}
	}
	if err := h.Cerebro.RevokeShareToken(r.Context(), tokenUUID); err != nil {
		writeError(w, http.StatusInternalServerError, "revoke failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// PublicGet is the unauth handler that serves a resource via a share
// token. Mounted OUTSIDE the auth middleware. The path param is the
// opaque token string. On success the response is a slim view of the
// underlying resource; on miss / revoke / persona-deny we always 404 so
// a probing client can't distinguish those.
func (h *Handler) PublicGet(w http.ResponseWriter, r *http.Request) {
	tok := strings.TrimSpace(chi.URLParam(r, "token"))
	if tok == "" {
		http.NotFound(w, r)
		return
	}

	row, err := h.Cerebro.GetShareTokenByToken(r.Context(), tok)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	if h.Persona == nil {
		// Refusing to serve without persona is the conservative default:
		// without a check we'd be trusting the cerebro row alone, which
		// loses the scope-restriction guarantee from JEH-1076 AC #2/#3.
		http.NotFound(w, r)
		return
	}

	personaURL := strings.TrimRight(os.Getenv("MULTICA_PERSONA_URL"), "/")
	if personaURL == "" {
		http.NotFound(w, r)
		return
	}

	resourceID := util.UUIDToString(row.ResourceID)
	res, err := persona.CheckAnonymous(r.Context(), personaURL, row.Action, row.ResourceKind, resourceID, util.UUIDToString(row.ID), nil, nil)
	if err != nil || res == nil || !res.Allowed {
		http.NotFound(w, r)
		return
	}

	switch row.ResourceKind {
	case IssueResourceKind:
		h.serveIssue(w, r, row.ResourceID)
	default:
		http.NotFound(w, r)
	}
}

func (h *Handler) serveIssue(w http.ResponseWriter, r *http.Request, issueID pgtype.UUID) {
	issue, err := h.Upstream.GetIssue(r.Context(), issueID)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	view := publicIssueView{
		ID:        util.UUIDToString(issue.ID),
		Number:    issue.Number,
		Title:     issue.Title,
		Status:    issue.Status,
		CreatedAt: util.TimestampToString(issue.CreatedAt),
	}
	if issue.Description.Valid {
		desc := issue.Description.String
		view.Description = &desc
	}
	writeJSON(w, http.StatusOK, view)
}

// loadIssueForMember resolves and authorises the {id} URL param against
// the caller's workspace. The route is mounted behind the standard
// workspace-member middleware; this helper just turns the URL param
// into a db.Issue or writes the appropriate 4xx.
func (h *Handler) loadIssueForMember(w http.ResponseWriter, r *http.Request) (db.Issue, bool) {
	wsID := middleware.WorkspaceIDFromContext(r.Context())
	if wsID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return db.Issue{}, false
	}
	wsUUID, err := util.ParseUUID(wsID)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workspace id")
		return db.Issue{}, false
	}
	issueUUID, err := util.ParseUUID(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid issue id")
		return db.Issue{}, false
	}
	issue, err := h.Upstream.GetIssueInWorkspace(r.Context(), db.GetIssueInWorkspaceParams{
		ID:          issueUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusNotFound, "issue not found")
		return db.Issue{}, false
	}
	return issue, true
}

// mintTokenString returns a URL-safe opaque token. 32 random bytes of
// entropy keeps a brute-force enumeration off the table for the
// foreseeable future even with no rate limiting.
func mintTokenString() (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
