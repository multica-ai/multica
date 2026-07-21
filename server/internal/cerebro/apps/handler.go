// Package apps owns the Multica app catalog. Registry data calls remain direct:
// this package stores app metadata, immutable versions, and workflow definitions.
package apps

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
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
	Forget(identity tokens.Identity) int
}

type connectionCaller interface {
	CallForApp(ctx context.Context, workspaceID, memberID, connectionID uuid.UUID, tool string, arguments map[string]any) (string, error)
}

type Handler struct {
	pool              *pgxpool.Pool
	tokens            tokenIssuer
	dispatcher        workflowDispatcher
	workerIngestKey   string
	dataEventKey      string
	enabled           bool
	connections       connectionCaller
	runtime           runtimeDeployer
	runtimeServiceKey string
}

type runtimeDeployer interface {
	Deploy(ctx context.Context, deployment RuntimeDeploymentRequest) error
	Invoke(ctx context.Context, appID, version string, input json.RawMessage) (json.RawMessage, error)
	Lifecycle(ctx context.Context, action, serviceID string) error
}

type lifecycleRuntime interface {
	Lifecycle(ctx context.Context, action, serviceID string) error
}

func applyRuntimeLifecycle(ctx context.Context, runtime lifecycleRuntime, action string, serviceIDs []string) error {
	if runtime == nil && len(serviceIDs) > 0 {
		return errRuntimeUnavailable
	}
	for _, serviceID := range serviceIDs {
		if err := runtime.Lifecycle(ctx, action, serviceID); err != nil {
			return err
		}
	}
	return nil
}

func (h *Handler) WithConnectionCaller(caller connectionCaller) *Handler {
	h.connections = caller
	return h
}

func NewHandler(pool *pgxpool.Pool) *Handler {
	handler := &Handler{
		pool:              pool,
		tokens:            tokens.NewBroker(tokens.ConfigFromEnv(), nil),
		dispatcher:        hatchetWorkflowDispatcher{},
		workerIngestKey:   strings.TrimSpace(os.Getenv("CEREBRO_APP_WORKFLOW_INGEST_KEY")),
		dataEventKey:      strings.TrimSpace(os.Getenv("CEREBRO_APP_DATA_EVENT_KEY")),
		enabled:           envFlagEnabled("CEREBRO_MINI_APPS_ENABLED"),
		runtimeServiceKey: strings.TrimSpace(os.Getenv("CEREBRO_APPS_RUNTIME_SERVICE_KEY")),
	}
	runtimeURL := strings.TrimSpace(os.Getenv("CEREBRO_APPS_RUNTIME_URL"))
	runtimeKey := strings.TrimSpace(os.Getenv("CEREBRO_APPS_RUNTIME_SERVICE_KEY"))
	if runtimeURL != "" && runtimeKey != "" {
		handler.runtime = NewRuntimeClient(runtimeURL, runtimeKey)
	}
	return handler
}

func (h *Handler) authenticateRuntimeRequest(r *http.Request, body []byte) bool {
	if h == nil || h.runtimeServiceKey == "" {
		return false
	}
	return verifyRuntimeSignature(
		h.runtimeServiceKey,
		r.Method,
		r.URL.EscapedPath(),
		body,
		r.Header.Get("X-Multica-Timestamp"),
		r.Header.Get("X-Multica-Signature"),
		time.Now().UTC(),
	) == nil
}

func (h *Handler) BundleDownload(w http.ResponseWriter, r *http.Request) {
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	version := chi.URLParam(r, "version")
	if err != nil || !semverPattern.MatchString(version) {
		writeError(w, http.StatusBadRequest, "invalid app version")
		return
	}
	bearer := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	signed := h.authenticateRuntimeRequest(r, nil)
	granted := h != nil && h.runtimeServiceKey != "" && verifyBundleToken(h.runtimeServiceKey, bearer, appID.String(), version, time.Now().UTC()) == nil
	if !signed && !granted {
		writeError(w, http.StatusUnauthorized, "runtime authentication failed")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT path,media_type,content,sha256 FROM cerebro_app_version_file WHERE app_id=$1 AND version=$2 ORDER BY path`, appID, version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app bundle")
		return
	}
	defer rows.Close()
	files := make([]BundleFile, 0)
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.Path, &file.MediaType, &file.Content, &file.SHA256); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load app bundle")
			return
		}
		files = append(files, file)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app bundle")
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "app bundle not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": appID, "version": version, "files": files})
}

func (h *Handler) PendingDeployments(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateRuntimeRequest(r, nil) {
		writeError(w, http.StatusUnauthorized, "runtime authentication failed")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT d.app_id,a.name,d.version,d.bundle_sha256 FROM cerebro_app_deployment d JOIN cerebro_app a ON a.id=d.app_id WHERE d.status IN ('pending','provisioning') ORDER BY d.created_at`)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app deployments")
		return
	}
	defer rows.Close()
	result := make([]map[string]string, 0)
	for rows.Next() {
		var appID uuid.UUID
		var appName, version, bundleSHA string
		if err := rows.Scan(&appID, &appName, &version, &bundleSHA); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load app deployments")
			return
		}
		result = append(result, map[string]string{"app_id": appID.String(), "app_name": appName, "version": version, "bundle_sha256": bundleSHA})
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app deployments")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (h *Handler) DeploymentInfo(w http.ResponseWriter, r *http.Request) {
	if !h.authenticateRuntimeRequest(r, nil) {
		writeError(w, http.StatusUnauthorized, "runtime authentication failed")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	version := chi.URLParam(r, "version")
	if err != nil || !semverPattern.MatchString(version) {
		writeError(w, http.StatusBadRequest, "invalid app version")
		return
	}
	var serviceID, internalDomain string
	err = h.pool.QueryRow(r.Context(), `SELECT external_service_id,internal_domain FROM cerebro_app_deployment WHERE app_id=$1 AND version=$2 AND status='ready'`, appID, version).Scan(&serviceID, &internalDomain)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "ready app deployment not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app deployment")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"external_service_id": serviceID, "internal_domain": internalDomain})
}

func (h *Handler) RuntimeCallback(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil || !h.authenticateRuntimeRequest(r, body) {
		writeError(w, http.StatusUnauthorized, "runtime authentication failed")
		return
	}
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	version := chi.URLParam(r, "version")
	if err != nil || !semverPattern.MatchString(version) {
		writeError(w, http.StatusBadRequest, "invalid app version")
		return
	}
	var callback deploymentCallback
	if err := json.Unmarshal(body, &callback); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update app deployment")
		return
	}
	defer tx.Rollback(r.Context())
	if err := updateDeploymentState(r.Context(), tx, appID, version, callback); err != nil {
		writeError(w, http.StatusBadRequest, "invalid deployment update")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update app deployment")
		return
	}
	if callback.Status == "failed" && callback.Error != "" {
		slog.Error("mini app runtime reported deployment failure", "app_id", appID, "version", version, "detail", callback.Error)
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": callback.Status})
}

func envFlagEnabled(name string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(name))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}

// RequireEnabled keeps the entire server surface default-off alongside the UI
// feature flag. Disabled routes look unavailable and cannot mutate app state.
func (h *Handler) RequireEnabled(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if h == nil || !h.enabled {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type createRequest struct {
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	FolderID    *uuid.UUID `json:"folder_id"`
}

type publishRequest struct {
	Version      string       `json:"version"`
	ReleaseNotes string       `json:"release_notes"`
	Files        []BundleFile `json:"files"`
}

type rollbackRequest struct {
	Version string `json:"version"`
}

type deploymentCallback struct {
	Status            string `json:"status"`
	ExternalServiceID string `json:"external_service_id"`
	InternalDomain    string `json:"internal_domain"`
	Error             string `json:"error"`
}

type tokenRequest struct {
	Version string `json:"version"`
}

type approveScopesRequest struct {
	Version string         `json:"version"`
	Scopes  []tokens.Scope `json:"scopes"`
}

type storageRequest struct {
	Value json.RawMessage `json:"value"`
}

type viewSubmissionRequest struct {
	RequestID string          `json:"request_id"`
	Version   string          `json:"version"`
	Value     json.RawMessage `json:"value"`
}

type connectionCallRequest struct {
	AppID     string         `json:"app_id"`
	Version   string         `json:"version"`
	Tool      string         `json:"tool"`
	Arguments map[string]any `json:"arguments"`
}

type appResponse struct {
	ID                string    `json:"id"`
	WorkspaceID       string    `json:"workspace_id"`
	Slug              string    `json:"slug"`
	Name              string    `json:"name"`
	Description       string    `json:"description"`
	Icon              string    `json:"icon"`
	Folder            string    `json:"folder"`
	OwnerID           *string   `json:"owner_id,omitempty"`
	CurrentVersion    *string   `json:"current_version,omitempty"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
	Owner             string    `json:"owner,omitempty"`
	Deployment        string    `json:"deployment_status,omitempty"`
	DeploymentVersion string    `json:"deployment_version,omitempty"`
	Health            string    `json:"health,omitempty"`
	DeploymentError   string    `json:"deployment_error,omitempty"`
}

type appVersionResponse struct {
	Version      string          `json:"version"`
	ReleaseNotes string          `json:"release_notes"`
	GrantStatus  string          `json:"grant_status"`
	Scopes       json.RawMessage `json:"scopes"`
	CreatedAt    time.Time       `json:"created_at"`
}

type appDetailResponse struct {
	appResponse
	Versions []appVersionResponse `json:"versions"`
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
	if len(req.Files) == 0 {
		return errors.New("files are required")
	}
	return nil
}

func validateViewSubmission(req viewSubmissionRequest) error {
	if _, err := uuid.Parse(req.RequestID); err != nil {
		return errors.New("request_id must be a UUID")
	}
	if !semverPattern.MatchString(req.Version) {
		return errors.New("version must be semantic versioning")
	}
	if len(req.Value) == 0 || !json.Valid(req.Value) {
		return errors.New("value must be valid JSON")
	}
	return nil
}

func updateDeploymentState(ctx context.Context, tx bundleExec, appID uuid.UUID, version string, callback deploymentCallback) error {
	switch callback.Status {
	case "ready":
		if strings.TrimSpace(callback.ExternalServiceID) == "" || strings.TrimSpace(callback.InternalDomain) == "" {
			return errors.New("ready deployment requires service identity")
		}
		if _, err := tx.Exec(ctx, `UPDATE cerebro_app_deployment SET status='ready',external_service_id=$3,internal_domain=$4,last_error='',updated_at=now() WHERE app_id=$1 AND version=$2`, appID, version, callback.ExternalServiceID, callback.InternalDomain); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE cerebro_app SET current_version=$2,status='published',updated_at=now()
			WHERE id=$1 AND (
				NOT EXISTS (SELECT 1 FROM cerebro_app_grant WHERE app_id=$1 AND version=$2)
				OR EXISTS (SELECT 1 FROM cerebro_app_grant WHERE app_id=$1 AND version=$2 AND status='approved')
			)`, appID, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO cerebro_app_audit_log (workspace_id,app_id,actor_type,actor_id,action,metadata)
			SELECT workspace_id,id,'system','apps-runtime','app.version.published',jsonb_build_object('version',$2) FROM cerebro_app WHERE id=$1`, appID, version)
		return err
	case "failed":
		if _, err := tx.Exec(ctx, `UPDATE cerebro_app_deployment SET status='failed',last_error='App runtime failed',updated_at=now() WHERE app_id=$1 AND version=$2`, appID, version); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO cerebro_app_audit_log (workspace_id,app_id,actor_type,actor_id,action,metadata)
			SELECT workspace_id,id,'system','apps-runtime','app.runtime.failed',jsonb_build_object('version',$2) FROM cerebro_app WHERE id=$1`, appID, version)
		return err
	default:
		return errors.New("unsupported deployment status")
	}
}

func approvedConnectionScope(scopes []tokens.Scope, connectionID string) bool {
	for _, scope := range scopes {
		if scope.ResourceType == "integration" && scope.ResourceID == connectionID && (scope.Access == "write" || scope.Access == "read_write") {
			return true
		}
	}
	return false
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

func validateScopes(scopes []tokens.Scope) error {
	resourceTypes := map[string]bool{"data_source": true, "data_destination": true, "app": true, "function": true, "integration": true, "bigquery_credential": true}
	accessTypes := map[string]bool{"read": true, "write": true, "read_write": true}
	if len(scopes) == 0 {
		return errors.New("at least one scope is required")
	}
	for _, scope := range scopes {
		if !resourceTypes[scope.ResourceType] || strings.TrimSpace(scope.ResourceID) == "" || !accessTypes[scope.Access] {
			return errors.New("scope requires a supported resource_type, resource_id, and access")
		}
	}
	return nil
}

func snapshotScopes(raw json.RawMessage) ([]tokens.Scope, error) {
	var snapshot struct {
		Manifest struct {
			Scopes []tokens.Scope `json:"scopes"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, err
	}
	return snapshot.Manifest.Scopes, nil
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
	if !supportedWorkflowTrigger(definition.Trigger.Type) {
		return errors.New("trigger type is not supported")
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

func supportedWorkflowTrigger(value string) bool {
	switch value {
	case "schedule", "webhook", "data_event", "manual", "chat":
		return true
	default:
		return false
	}
}

func (h *Handler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	userID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT a.id, a.workspace_id, a.slug, a.name, a.description, a.icon, COALESCE(f.name,a.folder), a.owner_id,
		       a.current_version, a.status, a.created_at, a.updated_at, COALESCE(u.name,''),
		       COALESCE(d.status,'not_deployed'), COALESCE(d.version,''), COALESCE(d.last_error,'')
		FROM cerebro_app a LEFT JOIN cerebro_app_folder f ON f.id=a.folder_id
		LEFT JOIN "user" u ON u.id=a.owner_id
		LEFT JOIN LATERAL (
		  SELECT status,version,last_error FROM cerebro_app_deployment
		  WHERE app_id=a.id ORDER BY updated_at DESC LIMIT 1
		) d ON true
		WHERE a.workspace_id=$1
		  AND (
		    a.owner_id=$2
		    OR EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=$1 AND m.user_id=$2 AND m.role IN ('owner','admin'))
		    OR cerebro_app_folder_grant_visible(a.folder_id,$2)
		  )
		ORDER BY COALESCE(f.name,a.folder), a.name`, workspaceID, userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list apps")
		return
	}
	defer rows.Close()
	apps := make([]appResponse, 0)
	for rows.Next() {
		app, err := scanCatalogApp(rows)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read apps")
			return
		}
		apps = append(apps, app)
	}
	var canManage bool
	err = h.pool.QueryRow(r.Context(), `SELECT EXISTS (
		SELECT 1 FROM member m WHERE m.workspace_id=$1 AND m.user_id=$2 AND m.role IN ('owner','admin')
		UNION ALL
		SELECT 1 FROM cerebro_group_capability c JOIN cerebro_group g ON g.id=c.group_id JOIN cerebro_group_member gm ON gm.group_id=g.id
		WHERE g.workspace_id=$1 AND gm.user_id=$2 AND c.capability='apps.manage'
	)`, workspaceID, userID).Scan(&canManage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read app permissions")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": apps, "can_manage": canManage})
}

func (h *Handler) Get(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	versionRows, err := h.pool.Query(r.Context(), `
		SELECT v.version,v.release_notes,v.created_at,
		       COALESCE(g.status,'not_requested'),COALESCE(g.scopes,'[]'::jsonb)
		FROM cerebro_app_version v
		LEFT JOIN cerebro_app_grant g ON g.app_id=v.app_id AND g.version=v.version
		WHERE v.app_id=$1 ORDER BY v.created_at DESC`, app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read app versions")
		return
	}
	defer versionRows.Close()
	versions := make([]appVersionResponse, 0)
	for versionRows.Next() {
		var version appVersionResponse
		if err := versionRows.Scan(&version.Version, &version.ReleaseNotes, &version.CreatedAt, &version.GrantStatus, &version.Scopes); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to read app versions")
			return
		}
		versions = append(versions, version)
	}
	writeJSON(w, http.StatusOK, appDetailResponse{appResponse: app, Versions: versions})
}

func (h *Handler) VersionFiles(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	version := chi.URLParam(r, "version")
	if !semverPattern.MatchString(version) {
		writeError(w, http.StatusBadRequest, "version must be semantic versioning")
		return
	}
	rows, err := h.pool.Query(r.Context(), `SELECT path,media_type,content,sha256 FROM cerebro_app_version_file WHERE app_id=$1 AND version=$2 ORDER BY path`, app.ID, version)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app files")
		return
	}
	defer rows.Close()
	files := make([]BundleFile, 0)
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.Path, &file.MediaType, &file.Content, &file.SHA256); err != nil {
			writeError(w, http.StatusInternalServerError, "failed to load app files")
			return
		}
		files = append(files, file)
	}
	if rows.Err() != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app files")
		return
	}
	if len(files) == 0 {
		writeError(w, http.StatusNotFound, "app version files not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": app.ID, "version": version, "files": files})
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
		WITH inserted AS (
			INSERT INTO cerebro_app (workspace_id, slug, name, description, icon, folder, folder_id, owner_id)
			SELECT $1,$2,$3,$4,$5,'',$6,$7
			WHERE $6::uuid IS NULL OR EXISTS (
				SELECT 1 FROM cerebro_app_folder WHERE id=$6 AND workspace_id=$1
			)
			RETURNING *
		)
		SELECT a.id, a.workspace_id, a.slug, a.name, a.description, a.icon,
		       COALESCE(f.name, a.folder), a.owner_id, a.current_version, a.status,
		       a.created_at, a.updated_at
		FROM inserted a LEFT JOIN cerebro_app_folder f ON f.id=a.folder_id`, workspaceID, req.Slug, strings.TrimSpace(req.Name), req.Description, req.Icon, req.FolderID, userID)
	app, err := scanApp(row)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusBadRequest, "collection not found")
		return
	}
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
	if !decodeJSONLimit(w, r, &req, 8<<20) {
		return
	}
	if err := validatePublishRequest(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	bundle, err := ValidateBundle(app.Name, req.Version, req.Files)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var snapshot json.RawMessage
	for _, file := range bundle.Files {
		if file.Path == "app.json" {
			snapshot = json.RawMessage(file.Content)
			break
		}
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to publish app")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_version (app_id,version,content_snapshot,release_notes,created_by) VALUES ($1,$2,$3,$4,$5)`, app.ID, req.Version, snapshot, strings.TrimSpace(req.ReleaseNotes), userID); err != nil {
		writeError(w, http.StatusConflict, "version already exists")
		return
	}
	appID, err := uuid.Parse(app.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to publish app")
		return
	}
	if err = StoreVersionBundle(r.Context(), tx, appID, req.Version, bundle); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to store app bundle")
		return
	}
	scopes, err := snapshotScopes(snapshot)
	if err != nil {
		writeError(w, 400, "snapshot manifest scopes are invalid")
		return
	}
	if len(scopes) > 0 {
		rawScopes, _ := json.Marshal(scopes)
		if _, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_grant (app_id,version,scopes,status,requested_by) VALUES ($1,$2,$3,'pending',$4) ON CONFLICT (app_id,version) DO NOTHING`, app.ID, req.Version, rawScopes, userID); err != nil {
			writeError(w, 500, "failed to request app scopes")
			return
		}
	}
	provider := strings.TrimSpace(os.Getenv("CEREBRO_APPS_RUNTIME_PROVIDER"))
	if provider == "" {
		provider = "docker"
	}
	if provider != "docker" && provider != "sliplane" {
		writeError(w, http.StatusInternalServerError, "app runtime is not configured")
		return
	}
	if _, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_deployment (app_id,version,provider,status,bundle_sha256) VALUES ($1,$2,$3,'pending',$4)`, app.ID, req.Version, provider, bundle.SHA256); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to create app deployment")
		return
	}
	if err = tx.Commit(r.Context()); err != nil {
		writeError(w, 500, "failed to publish app")
		return
	}
	deployment := RuntimeDeploymentRequest{AppID: app.ID, AppName: app.Name, Version: req.Version, BundleSHA256: bundle.SHA256}
	if h.runtime == nil || h.runtime.Deploy(r.Context(), deployment) != nil {
		_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='failed',last_error='App runtime is unavailable',updated_at=now() WHERE app_id=$1 AND version=$2`, app.ID, req.Version)
		slog.Error("mini app runtime deployment failed", "app_id", app.ID, "version", req.Version)
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='provisioning',updated_at=now() WHERE app_id=$1 AND version=$2`, app.ID, req.Version)
	writeJSON(w, http.StatusAccepted, map[string]any{"app_id": app.ID, "version": req.Version, "release_notes": strings.TrimSpace(req.ReleaseNotes), "deployment_status": "provisioning"})
}

func (h *Handler) Rollback(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	actorID, ok := requestUserID(w, r)
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
	var bundleSHA string
	err := h.pool.QueryRow(r.Context(), `SELECT bundle_sha256 FROM cerebro_app_deployment WHERE app_id=$1 AND version=$2 AND status IN ('ready','paused')`, app.ID, req.Version).Scan(&bundleSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusConflict, "app version is not available for rollback")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load rollback version")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare app rollback")
		return
	}
	defer tx.Rollback(r.Context())
	appID := uuid.MustParse(app.ID)
	if err := markRollbackProvisioning(r.Context(), tx, appID, actorID, req.Version); err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare app rollback")
		return
	}
	if h.runtime == nil || h.runtime.Deploy(r.Context(), RuntimeDeploymentRequest{AppID: app.ID, AppName: app.Name, Version: req.Version, BundleSHA256: bundleSHA}) != nil {
		_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='paused',last_error='App runtime is unavailable',updated_at=now() WHERE app_id=$1 AND version=$2`, app.ID, req.Version)
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"app_id": app.ID, "version": req.Version, "deployment_status": "provisioning"})
}

func markRollbackProvisioning(ctx context.Context, exec bundleExec, appID, actorID uuid.UUID, version string) error {
	result, err := exec.Exec(ctx, `UPDATE cerebro_app_deployment SET status='provisioning',last_error='',updated_at=now()
		WHERE app_id=$1 AND version=$2 AND status IN ('ready','paused')`, appID, version)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("rollback version is not available")
	}
	_, err = exec.Exec(ctx, `INSERT INTO cerebro_app_audit_log (workspace_id,app_id,actor_type,actor_id,action,metadata)
		SELECT workspace_id,id,'user',$2,$3,jsonb_build_object('version',$4,'status','provisioning') FROM cerebro_app WHERE id=$1`, appID, actorID.String(), "app.version.rollback", version)
	return err
}

func markDeploymentRetrying(ctx context.Context, exec bundleExec, appID uuid.UUID, version, bundleSHA256 string) error {
	result, err := exec.Exec(ctx, `UPDATE cerebro_app_deployment SET status='provisioning',last_error='',updated_at=now()
		WHERE app_id=$1 AND version=$2 AND bundle_sha256=$3 AND status='failed'`, appID, version, bundleSHA256)
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errors.New("failed deployment is not available for retry")
	}
	return nil
}

func (h *Handler) RetryDeployment(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	var req rollbackRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !semverPattern.MatchString(req.Version) {
		writeError(w, http.StatusBadRequest, "version must be semantic versioning")
		return
	}
	appID := uuid.MustParse(app.ID)
	bundle, err := h.loadValidatedStoredBundle(r.Context(), appID, app.Name, req.Version)
	if err != nil {
		writeError(w, http.StatusConflict, "stored app bundle does not match this version")
		return
	}
	if err := markDeploymentRetrying(r.Context(), h.pool, appID, req.Version, bundle.SHA256); err != nil {
		writeError(w, http.StatusConflict, "failed deployment is not available for retry")
		return
	}
	if h.runtime == nil || h.runtime.Deploy(r.Context(), RuntimeDeploymentRequest{AppID: app.ID, AppName: app.Name, Version: req.Version, BundleSHA256: bundle.SHA256}) != nil {
		_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='failed',last_error='App runtime is unavailable',updated_at=now() WHERE app_id=$1 AND version=$2 AND status='provisioning'`, appID, req.Version)
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"app_id": app.ID, "version": req.Version, "deployment_status": "provisioning"})
}

func (h *Handler) loadValidatedStoredBundle(ctx context.Context, appID uuid.UUID, appName, version string) (ValidatedBundle, error) {
	rows, err := h.pool.Query(ctx, `SELECT path,media_type,content,sha256 FROM cerebro_app_version_file WHERE app_id=$1 AND version=$2 ORDER BY path`, appID, version)
	if err != nil {
		return ValidatedBundle{}, err
	}
	defer rows.Close()
	files := make([]BundleFile, 0)
	for rows.Next() {
		var file BundleFile
		if err := rows.Scan(&file.Path, &file.MediaType, &file.Content, &file.SHA256); err != nil {
			return ValidatedBundle{}, err
		}
		files = append(files, file)
	}
	if err := rows.Err(); err != nil {
		return ValidatedBundle{}, err
	}
	bundle, err := ValidateBundle(appName, version, files)
	if err != nil {
		return ValidatedBundle{}, err
	}
	var storedSHA string
	if err := h.pool.QueryRow(ctx, `SELECT bundle_sha256 FROM cerebro_app_deployment WHERE app_id=$1 AND version=$2 AND status='failed'`, appID, version).Scan(&storedSHA); err != nil || storedSHA != bundle.SHA256 {
		return ValidatedBundle{}, errors.New("stored bundle hash mismatch")
	}
	return bundle, nil
}

func (h *Handler) ApproveScopes(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	approverID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	var req approveScopesRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if !semverPattern.MatchString(req.Version) {
		writeError(w, http.StatusBadRequest, "version must be semantic versioning")
		return
	}
	if err := validateScopes(req.Scopes); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rawScopes, _ := json.Marshal(req.Scopes)
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve app scopes")
		return
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), `
		INSERT INTO cerebro_app_grant (app_id,version,scopes,registry_profile_ref,status,approved_by,approved_at)
		SELECT $1,$2,$3,$4,'approved',$5,now()
		WHERE EXISTS (SELECT 1 FROM cerebro_app_version WHERE app_id=$1 AND version=$2)
		ON CONFLICT (app_id,version) DO UPDATE SET scopes=EXCLUDED.scopes,
		registry_profile_ref=EXCLUDED.registry_profile_ref,status='approved',approved_by=EXCLUDED.approved_by,
		approved_at=now(),updated_at=now()`, app.ID, req.Version, rawScopes, "via_app:"+app.ID+"@"+req.Version, approverID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve app scopes")
		return
	}
	if result.RowsAffected() == 0 {
		writeError(w, http.StatusNotFound, "app version not found")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE cerebro_app SET current_version=$2,status='published',updated_at=now() WHERE id=$1 AND EXISTS (SELECT 1 FROM cerebro_app_deployment WHERE app_id=$1 AND version=$2 AND status='ready')`, app.ID, req.Version); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to activate app version")
		return
	}
	if _, err := tx.Exec(r.Context(), `INSERT INTO cerebro_app_audit_log (workspace_id,app_id,actor_type,actor_id,action,metadata) VALUES ($1,$2,'user',$3,'app.scopes.approved',jsonb_build_object('version',$4))`, app.WorkspaceID, app.ID, approverID.String(), req.Version); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record scope approval")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to approve app scopes")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": app.ID, "version": req.Version, "status": "approved", "scopes": req.Scopes})
}

func (h *Handler) GetStorage(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	key, ok := storageKey(w, r)
	if !ok {
		return
	}
	var value json.RawMessage
	err := h.pool.QueryRow(r.Context(), `SELECT value FROM cerebro_app_kv WHERE app_id=$1 AND key=$2`, app.ID, key).Scan(&value)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "app storage key not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to read app storage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": value})
}

func (h *Handler) PutStorage(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	key, ok := storageKey(w, r)
	if !ok {
		return
	}
	var req storageRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.Value) == 0 || !json.Valid(req.Value) {
		writeError(w, http.StatusBadRequest, "value must be valid JSON")
		return
	}
	_, err := h.pool.Exec(r.Context(), `INSERT INTO cerebro_app_kv (app_id,key,value,updated_by) VALUES ($1,$2,$3,$4) ON CONFLICT (app_id,key) DO UPDATE SET value=EXCLUDED.value,updated_by=EXCLUDED.updated_by,updated_at=now()`, app.ID, key, req.Value, memberID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to write app storage")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"key": key, "value": req.Value})
}

func (h *Handler) DeleteStorage(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	key, ok := storageKey(w, r)
	if !ok {
		return
	}
	_, err := h.pool.Exec(r.Context(), `DELETE FROM cerebro_app_kv WHERE app_id=$1 AND key=$2`, app.ID, key)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete app storage")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) SubmitView(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	viewID := strings.TrimSpace(chi.URLParam(r, "viewId"))
	if viewID == "" || len(viewID) > 100 {
		writeError(w, http.StatusBadRequest, "invalid view id")
		return
	}
	var req viewSubmissionRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := validateViewSubmission(req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if app.CurrentVersion == nil || req.Version != *app.CurrentVersion {
		writeError(w, http.StatusConflict, "view submission must target the current published version")
		return
	}
	requestID, _ := uuid.Parse(req.RequestID)
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit app view")
		return
	}
	defer tx.Rollback(r.Context())
	result, err := tx.Exec(r.Context(), `UPDATE cerebro_app_view_request SET output=$2,status='submitted',submitted_by=$3,submitted_at=now() WHERE id=$1 AND app_id=$4 AND app_version=$5 AND view_id=$6 AND status='waiting'`, requestID, req.Value, memberID, app.ID, req.Version, viewID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit app view")
		return
	}
	if result.RowsAffected() != 1 {
		writeError(w, http.StatusConflict, "app view is no longer waiting for a response")
		return
	}
	var submissionID uuid.UUID
	err = tx.QueryRow(r.Context(), `INSERT INTO cerebro_app_view_submission (app_id,app_version,view_id,submitted_by,value) VALUES ($1,$2,$3,$4,$5) RETURNING id`, app.ID, req.Version, viewID, memberID, req.Value).Scan(&submissionID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit app view")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to submit app view")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": submissionID, "status": "submitted"})
}

func (h *Handler) GetViewRequest(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	if _, ok := requestUserID(w, r); !ok {
		return
	}
	requestID, err := uuid.Parse(chi.URLParam(r, "requestId"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid view request id")
		return
	}
	var appID uuid.UUID
	var appName, version, viewID, status string
	var input json.RawMessage
	err = h.pool.QueryRow(r.Context(), `
		SELECT vr.app_id,a.name,vr.app_version,vr.view_id,vr.input,vr.status
		FROM cerebro_app_view_request vr
		JOIN cerebro_app_workflow_run wr ON wr.id=vr.workflow_run_id
		JOIN cerebro_app_workflow_def wd ON wd.id=wr.workflow_id AND wd.workspace_id=$2
		JOIN cerebro_app a ON a.id=vr.app_id AND a.workspace_id=$2
		WHERE vr.id=$1`, requestID, workspaceID).Scan(&appID, &appName, &version, &viewID, &input, &status)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "app view request not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app view request")
		return
	}
	runtimeURL := fmt.Sprintf("/api/cerebro/apps-runtime/apps/%s/%s/?view=%s&request=%s", appID, version, url.QueryEscape(viewID), requestID)
	writeJSON(w, http.StatusOK, map[string]any{"id": requestID, "app_id": appID, "app_name": appName, "app_version": version, "view_id": viewID, "input": input, "status": status, "runtime_url": runtimeURL})
}

func storageKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	key := strings.TrimSpace(chi.URLParam(r, "key"))
	if key == "" || len(key) > 200 || strings.Contains(key, "/") {
		writeError(w, http.StatusBadRequest, "invalid app storage key")
		return "", false
	}
	return key, true
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
		App:      tokens.AppGrant{ID: app.ID, Version: version, RunID: uuid.NewString(), Scopes: scopes},
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
	identity.App.RunID = runID.String()
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
	userID, ok := requestUserID(w, r)
	if !ok {
		return appResponse{}, false
	}
	appID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 400, "invalid app id")
		return appResponse{}, false
	}
	row := h.pool.QueryRow(r.Context(), `
		SELECT a.id, a.workspace_id, a.slug, a.name, a.description, a.icon,
		       COALESCE(f.name,a.folder), a.owner_id, a.current_version, a.status,
		       a.created_at, a.updated_at
		FROM cerebro_app a
		LEFT JOIN cerebro_app_folder f ON f.id=a.folder_id
		WHERE a.id=$1 AND a.workspace_id=$2
		  AND (
		    a.owner_id=$3
		    OR EXISTS (SELECT 1 FROM member m WHERE m.workspace_id=$2 AND m.user_id=$3 AND m.role IN ('owner','admin'))
		    OR cerebro_app_folder_grant_visible(a.folder_id,$3)
		  )`, appID, workspaceID, userID)
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

func scanCatalogApp(row rowScanner) (appResponse, error) {
	var app appResponse
	var id, workspaceID uuid.UUID
	var ownerID *uuid.UUID
	if err := row.Scan(&id, &workspaceID, &app.Slug, &app.Name, &app.Description, &app.Icon, &app.Folder, &ownerID, &app.CurrentVersion, &app.Status, &app.CreatedAt, &app.UpdatedAt, &app.Owner, &app.Deployment, &app.DeploymentVersion, &app.DeploymentError); err != nil {
		return app, err
	}
	app.ID, app.WorkspaceID = id.String(), workspaceID.String()
	if ownerID != nil {
		value := ownerID.String()
		app.OwnerID = &value
	}
	switch {
	case app.Status == "disabled":
		app.Health = "disabled"
	case app.Deployment == "ready":
		app.Health = "healthy"
	case app.Deployment == "failed":
		app.Health = "failed"
	case app.Deployment == "pending" || app.Deployment == "provisioning":
		app.Health = "provisioning"
	default:
		app.Health = "not_deployed"
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
	return decodeJSONLimit(w, r, out, 4<<20)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
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
