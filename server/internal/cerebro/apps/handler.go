// Package apps owns the Multica app catalog. Registry data calls remain direct:
// this package stores app metadata, immutable versions, and workflow definitions.
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/multica-ai/multica/server/internal/cerebro/apps/tokens"
	"github.com/multica-ai/multica/server/internal/middleware"
)

var slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
var semverPattern = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(?:-[0-9A-Za-z.-]+)?$`)

type tokenIssuer interface {
	PersonalKey(rctx context.Context, identity tokens.Identity) (tokens.Token, error)
}

type Handler struct {
	pool   *pgxpool.Pool
	tokens tokenIssuer
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	return &Handler{pool: pool, tokens: tokens.NewBroker(tokens.ConfigFromEnv(), nil)}
}

type createRequest struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Folder      string `json:"folder"`
}

type publishRequest struct {
	Version      string          `json:"version"`
	ReleaseNotes string          `json:"release_notes"`
	Snapshot     json.RawMessage `json:"snapshot"`
}

type rollbackRequest struct {
	Version string `json:"version"`
}

type tokenRequest struct {
	Version string `json:"version"`
}

type appResponse struct {
	ID             string    `json:"id"`
	WorkspaceID    string    `json:"workspace_id"`
	Slug           string    `json:"slug"`
	Name           string    `json:"name"`
	Description    string    `json:"description"`
	Icon           string    `json:"icon"`
	Folder         string    `json:"folder"`
	OwnerID        *string   `json:"owner_id,omitempty"`
	CurrentVersion *string   `json:"current_version,omitempty"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func validateCreateRequest(req createRequest) error {
	if strings.TrimSpace(req.Name) == "" {
		return errors.New("name is required")
	}
	if !slugPattern.MatchString(req.Slug) {
		return errors.New("slug must contain lowercase letters, numbers, and single hyphens")
	}
	return nil
}

func validatePublishRequest(req publishRequest) error {
	if !semverPattern.MatchString(req.Version) {
		return errors.New("version must be semantic versioning, for example 1.0.0")
	}
	if strings.TrimSpace(req.ReleaseNotes) == "" {
		return errors.New("release_notes is required")
	}
	return validateSnapshot(req.Snapshot)
}

func validateSnapshot(raw json.RawMessage) error {
	var snapshot struct {
		Manifest struct {
			SchemaVersion string `json:"schema_version"`
			Name          string `json:"name"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return fmt.Errorf("snapshot: %w", err)
	}
	if snapshot.Manifest.SchemaVersion == "" || snapshot.Manifest.Name == "" {
		return errors.New("snapshot.manifest requires schema_version and name")
	}
	return nil
}

func validateWorkflowDefinition(raw json.RawMessage) error {
	var definition struct {
		SchemaVersion string `json:"schema_version"`
		Trigger       *struct {
			ID, Type string
			Config   json.RawMessage
		} `json:"trigger"`
		Steps []struct {
			ID, Type string
			Config   json.RawMessage
		} `json:"steps"`
	}
	if err := json.Unmarshal(raw, &definition); err != nil {
		return fmt.Errorf("definition: %w", err)
	}
	if definition.SchemaVersion != "1" {
		return errors.New("schema_version must be 1")
	}
	if definition.Trigger == nil || definition.Trigger.ID == "" || definition.Trigger.Type == "" {
		return errors.New("one trigger is required")
	}
	seen := map[string]bool{definition.Trigger.ID: true}
	for _, step := range definition.Steps {
		if step.ID == "" || step.Type == "" {
			return errors.New("every step requires id and type")
		}
		if seen[step.ID] {
			return fmt.Errorf("duplicate node id %q", step.ID)
		}
		seen[step.ID] = true
	}
	return nil
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT id, workspace_id, slug, name, description, icon, folder, owner_id,
		       current_version, status, created_at, updated_at
		FROM cerebro_app WHERE workspace_id=$1 ORDER BY folder, name`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list apps")
		return
	}
	defer rows.Close()
	apps := make([]appResponse, 0)
	for rows.Next() {
		app, err := scanApp(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read apps")
			return
		}
		apps = append(apps, app)
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps})
}

func (h *Handler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	userID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	var req createRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateCreateRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Icon == "" {
		req.Icon = "blocks"
	}
	row := h.pool.QueryRow(r.Context(), `
		INSERT INTO cerebro_app (workspace_id, slug, name, description, icon, folder, owner_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, workspace_id, slug, name, description, icon, folder, owner_id,
		          current_version, status, created_at, updated_at`, workspaceID, req.Slug, strings.TrimSpace(req.Name), req.Description, req.Icon, req.Folder, userID)
	app, err := scanApp(row)
	if err != nil {
		writeError(w, http.StatusConflict, "app slug already exists")
		return
	}
	writeJSON(w, http.StatusCreated, app)
}

func (h *Handler) Preview(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.loadApp(w, r); !ok {
		return
	}
	var req json.RawMessage
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateSnapshot(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"preview": true, "snapshot": req})
}

func (h *Handler) Publish(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	userID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	var req publishRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validatePublishRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to publish app")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_version (app_id,version,content_snapshot,release_notes,created_by) VALUES ($1,$2,$3,$4,$5)`, app.ID, req.Version, req.Snapshot, strings.TrimSpace(req.ReleaseNotes), userID); err != nil {
		writeError(w, http.StatusConflict, "version already exists")
		return
	}
	if _, err = tx.Exec(r.Context(), `UPDATE cerebro_app SET current_version=$2,status='published',updated_at=now() WHERE id=$1`, app.ID, req.Version); err != nil {
		writeError(w, 500, "failed to publish app")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to publish app")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"app_id": app.ID, "version": req.Version, "release_notes": strings.TrimSpace(req.ReleaseNotes)})
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	var req rollbackRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !semverPattern.MatchString(req.Version) {
		writeError(w, 400, "version must be semantic versioning")
		return
	}
	result, err := h.pool.Exec(r.Context(), `UPDATE cerebro_app SET current_version=$2,status='published',updated_at=now() WHERE id=$1 AND EXISTS (SELECT 1 FROM cerebro_app_version WHERE app_id=$1 AND version=$2)`, app.ID, req.Version)
	if err != nil {
		writeError(w, 500, "failed to roll back app")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, 404, "app version not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": app.ID, "version": req.Version})
}

// IssueToken exchanges Cerebro's registry credential for a short-lived key
// bounded by both the current human and the app's approved grant ceiling.
func (h *Handler) IssueToken(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	var req tokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if app.CurrentVersion == nil {
		writeError(w, http.StatusConflict, "app has no published version")
		return
	}
	version := *app.CurrentVersion
	if req.Version != "" && req.Version != version {
		writeError(w, http.StatusConflict, "only the current published app version can request a token")
		return
	}
	var rawScopes []byte
	err := h.pool.QueryRow(r.Context(), `
		SELECT scopes FROM cerebro_app_grant
		WHERE app_id=$1 AND version=$2 AND status='approved'`, app.ID, version).Scan(&rawScopes)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusForbidden, "app scopes have not been approved")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load approved app scopes")
		return
	}
	var scopes []tokens.Scope
	if err := json.Unmarshal(rawScopes, &scopes); err != nil || len(scopes) == 0 {
		writeError(w, http.StatusInternalServerError, "approved app scopes are invalid")
		return
	}
	token, err := h.tokens.PersonalKey(r.Context(), tokens.Identity{
		MemberID: memberID.String(),
		App:      tokens.AppGrant{ID: app.ID, Version: version, Scopes: scopes},
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to issue app token")
		return
	}
	writeJSON(w, http.StatusCreated, token)
}

// IssueWorkflowToken resolves the immutable identity envelope captured when a
// run was created. Long-running workers therefore renew as the original human
// and app version instead of retaining an expired key.
func (h *Handler) IssueWorkflowToken(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	runID, err := uuid.Parse(chi.URLParam(r, "runId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid workflow run id")
		return
	}
	var rawEnvelope []byte
	err = h.pool.QueryRow(r.Context(), `
		SELECT r.identity_envelope
		FROM cerebro_app_workflow_run r
		JOIN cerebro_app_workflow_def d ON d.id=r.workflow_id
		WHERE r.id=$1 AND d.workspace_id=$2`, runID, workspaceID).Scan(&rawEnvelope)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "workflow run not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load workflow identity")
		return
	}
	identity, err := tokens.ParseWorkflowIdentityEnvelope(rawEnvelope)
	if err != nil {
		writeError(w, http.StatusConflict, "workflow identity envelope is invalid")
		return
	}
	token, err := h.tokens.PersonalKey(r.Context(), identity)
	if err != nil {
		writeError(w, http.StatusBadGateway, "failed to issue workflow token")
		return
	}
	writeJSON(w, http.StatusCreated, token)
}

func (h *Handler) loadApp(w http.ResponseWriter, r *http.Request) (appResponse, bool) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return appResponse{}, false
	}
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid app id")
		return appResponse{}, false
	}
	row := h.pool.QueryRow(r.Context(), `SELECT id, workspace_id, slug, name, description, icon, folder, owner_id, current_version, status, created_at, updated_at FROM cerebro_app WHERE id=$1 AND workspace_id=$2`, appID, workspaceID)
	app, err := scanApp(row)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, 404, "app not found")
		return appResponse{}, false
	}
	if err != nil {
		writeError(w, 500, "failed to load app")
		return appResponse{}, false
	}
	return app, true
}

type rowScanner interface{ Scan(...any) error }

func scanApp(row rowScanner) (appResponse, error) {
	var app appResponse
	var id, workspaceID uuid.UUID
	var ownerID *uuid.UUID
	if err := row.Scan(&id, &workspaceID, &app.Slug, &app.Name, &app.Description, &app.Icon, &app.Folder, &ownerID, &app.CurrentVersion, &app.Status, &app.CreatedAt, &app.UpdatedAt); err != nil {
		return app, err
	}
	app.ID, app.WorkspaceID = id.String(), workspaceID.String()
	if ownerID != nil {
		value := ownerID.String()
		app.OwnerID = &value
	}
	return app, nil
}

func requestWorkspaceID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(middleware.WorkspaceIDFromContext(r.Context()))
	if err != nil {
		writeError(w, 400, "invalid workspace id")
		return uuid.Nil, false
	}
	return id, true
}

func requestUserID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(r.Header.Get("X-User-ID"))
	if err != nil {
		writeError(w, 400, "invalid user id")
		return uuid.Nil, false
	}
	return id, true
}

func decodeJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(out); err != nil {
		writeError(w, 400, "invalid JSON body")
		return false
	}
	return true
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
