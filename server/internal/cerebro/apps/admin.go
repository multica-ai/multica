package apps

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

var allergenFormatterSnapshot = json.RawMessage(`{"manifest":{"schema_version":"1","name":"Allergen Formatter","version":"1.0.0","scopes":[{"resource_type":"integration","resource_id":"ai_gateway","access":"write"}],"frontend":{"entry":"frontend/index.html"},"backend":{"entry":"backend/index.mjs"},"views":[{"id":"formatter","type":"form","title":"Format allergens"}]}}`)

func allergenFormatterIDForWorkspace(workspaceID uuid.UUID) uuid.UUID {
	return uuid.NewSHA1(workspaceID, []byte("cerebro-app:allergen-formatter"))
}

func isKnownAppCapability(value string) bool {
	return value == "apps.create" || value == "apps.manage" || value == "apps.delete"
}

func (h *Handler) RequireCapability(capability string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !isKnownAppCapability(capability) {
				writeError(w, http.StatusInternalServerError, "unknown app capability")
				return
			}
			workspaceID, ok := requestWorkspaceID(w, r)
			if !ok {
				return
			}
			memberID, ok := requestUserID(w, r)
			if !ok {
				return
			}
			var allowed bool
			err := h.pool.QueryRow(r.Context(), `SELECT EXISTS (
				SELECT 1 FROM member m WHERE m.workspace_id=$1 AND m.user_id=$2 AND m.role IN ('owner','admin')
				UNION ALL
				SELECT 1 FROM cerebro_group_capability c JOIN cerebro_group g ON g.id=c.group_id JOIN cerebro_group_member gm ON gm.group_id=g.id
				WHERE g.workspace_id=$1 AND gm.user_id=$2 AND c.capability=$3
			)`, workspaceID, memberID, capability).Scan(&allowed)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "permission check failed")
				return
			}
			if !allowed {
				writeError(w, http.StatusForbidden, "app permission denied")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func (h *Handler) Delete(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	actorID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	appID := uuid.MustParse(app.ID)
	if err := markDeploymentsDeleting(r.Context(), h.pool, appID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to prepare app deletion")
		return
	}
	serviceIDs, err := h.deploymentServiceIDs(r, app.ID, "")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app deployments")
		return
	}
	if err := applyRuntimeLifecycle(r.Context(), h.runtime, "delete", serviceIDs); err != nil {
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete app")
		return
	}
	defer tx.Rollback(r.Context())
	if err := recordAppLifecycleAudit(r.Context(), tx, appID, actorID, "app.deleted"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record app deletion")
		return
	}
	result, err := tx.Exec(r.Context(), `DELETE FROM cerebro_app WHERE id=$1 AND NOT EXISTS (
		SELECT 1 FROM cerebro_app_deployment WHERE app_id=$1 AND status<>'deleting'
	)`, app.ID)
	if err != nil || result.RowsAffected() != 1 || tx.Commit(r.Context()) != nil {
		writeError(w, http.StatusInternalServerError, "failed to delete app")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) Disable(w http.ResponseWriter, r *http.Request) {
	app, ok := h.loadApp(w, r)
	if !ok {
		return
	}
	actorID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	serviceIDs := []string{}
	if app.CurrentVersion != nil {
		var serviceID string
		err := h.pool.QueryRow(r.Context(), `SELECT external_service_id FROM cerebro_app_deployment WHERE app_id=$1 AND version=$2 AND status='ready' AND external_service_id IS NOT NULL`, app.ID, *app.CurrentVersion).Scan(&serviceID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			writeError(w, http.StatusInternalServerError, "failed to load app deployment")
			return
		}
		if err == nil {
			serviceIDs = append(serviceIDs, serviceID)
		}
	}
	if err := applyRuntimeLifecycle(r.Context(), h.runtime, "pause", serviceIDs); err != nil {
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable app")
		return
	}
	defer tx.Rollback(r.Context())
	if _, err := tx.Exec(r.Context(), `UPDATE cerebro_app SET status='disabled',updated_at=now() WHERE id=$1`, app.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable app")
		return
	}
	if _, err := tx.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='paused',updated_at=now() WHERE app_id=$1 AND version=$2 AND status='ready'`, app.ID, app.CurrentVersion); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable app")
		return
	}
	if err := recordAppLifecycleAudit(r.Context(), tx, uuid.MustParse(app.ID), actorID, "app.disabled"); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record app disable")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to disable app")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"app_id": app.ID, "status": "disabled"})
}

func markDeploymentsDeleting(ctx context.Context, exec bundleExec, appID uuid.UUID) error {
	_, err := exec.Exec(ctx, `UPDATE cerebro_app_deployment SET status='deleting',updated_at=now() WHERE app_id=$1 AND status<>'deleting'`, appID)
	return err
}

func recordAppLifecycleAudit(ctx context.Context, exec bundleExec, appID, actorID uuid.UUID, action string) error {
	_, err := exec.Exec(ctx, `INSERT INTO cerebro_app_audit_log (workspace_id,app_id,actor_type,actor_id,action)
		SELECT workspace_id,id,'user',$2,$3 FROM cerebro_app WHERE id=$1`, appID, actorID.String(), action)
	return err
}

func (h *Handler) deploymentServiceIDs(r *http.Request, appID, version string) ([]string, error) {
	query := `SELECT external_service_id FROM cerebro_app_deployment WHERE app_id=$1 AND external_service_id IS NOT NULL`
	args := []any{appID}
	if version != "" {
		query += ` AND version=$2`
		args = append(args, version)
	}
	rows, err := h.pool.Query(r.Context(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	serviceIDs := []string{}
	for rows.Next() {
		var serviceID string
		if err := rows.Scan(&serviceID); err != nil {
			return nil, err
		}
		serviceIDs = append(serviceIDs, serviceID)
	}
	return serviceIDs, rows.Err()
}

func (h *Handler) InstallAllergenFormatter(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	appID := allergenFormatterIDForWorkspace(workspaceID)
	memberID, ok := requestUserID(w, r)
	if !ok {
		return
	}
	tx, err := h.pool.Begin(r.Context())
	if err != nil {
		writeError(w, 500, "failed to install Allergen Formatter")
		return
	}
	defer tx.Rollback(r.Context())
	bundle, err := ValidateBundle("Allergen Formatter", "1.0.0", allergenFormatterBundleFiles())
	if err != nil {
		writeError(w, 500, "built-in app bundle is invalid")
		return
	}
	result, err := tx.Exec(r.Context(), `INSERT INTO cerebro_app(id,workspace_id,slug,name,description,icon,folder,owner_id,status) VALUES($1,$2,'allergen-formatter','Allergen Formatter','Format ingredients and return regulated allergens','blocks','Operations',$3,'draft') ON CONFLICT (workspace_id,slug) DO NOTHING`, appID, workspaceID, memberID)
	if err != nil {
		writeError(w, 500, "failed to install Allergen Formatter")
		return
	}
	if result.RowsAffected() == 0 {
		var version, status string
		err = h.pool.QueryRow(r.Context(), `SELECT d.version,d.status FROM cerebro_app a
			JOIN cerebro_app_deployment d ON d.app_id=a.id
			WHERE a.id=$1 AND a.workspace_id=$2 ORDER BY d.updated_at DESC LIMIT 1`, appID, workspaceID).Scan(&version, &status)
		if err != nil {
			writeError(w, http.StatusConflict, "Allergen Formatter conflicts with an existing app")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"id": appID, "name": "Allergen Formatter", "version": version, "status": status})
		return
	}
	if result.RowsAffected() != 1 {
		writeError(w, 409, "Allergen Formatter is already installed")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_version(app_id,version,content_snapshot,release_notes,created_by) VALUES($1,'1.0.0',$2,'Initial FIR-154 release',$3)`, appID, allergenFormatterSnapshot, memberID)
	if err == nil {
		err = StoreVersionBundle(r.Context(), tx, appID, "1.0.0", bundle)
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_grant(app_id,version,scopes,status,requested_by,approved_by,approved_at) VALUES($1,'1.0.0',$2::jsonb->'manifest'->'scopes','approved',$3,$3,now())`, appID, allergenFormatterSnapshot, memberID)
	}
	provider := strings.TrimSpace(os.Getenv("CEREBRO_APPS_RUNTIME_PROVIDER"))
	if provider == "" {
		provider = "docker"
	}
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_deployment(app_id,version,provider,status,bundle_sha256) VALUES($1,'1.0.0',$2,'pending',$3)`, appID, provider, bundle.SHA256)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to install Allergen Formatter")
		return
	}
	deployment := RuntimeDeploymentRequest{AppID: appID.String(), AppName: "Allergen Formatter", Version: "1.0.0", BundleSHA256: bundle.SHA256}
	if h.runtime == nil || h.runtime.Deploy(r.Context(), deployment) != nil {
		_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='failed',last_error='App runtime is unavailable',updated_at=now() WHERE app_id=$1 AND version='1.0.0'`, appID)
		slog.Error("built-in mini app runtime deployment failed", "app_id", appID)
		writeError(w, http.StatusBadGateway, "app runtime is unavailable")
		return
	}
	_, _ = h.pool.Exec(r.Context(), `UPDATE cerebro_app_deployment SET status='provisioning',updated_at=now() WHERE app_id=$1 AND version='1.0.0'`, appID)
	writeJSON(w, http.StatusAccepted, map[string]any{"id": appID, "name": "Allergen Formatter", "version": "1.0.0", "status": "provisioning"})
}

func (h *Handler) AdminOverview(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
	rows, err := h.pool.Query(r.Context(), `
		SELECT a.id,a.name,COALESCE(u.name,''),COALESCE(a.current_version,''),a.status,
		       COALESCE(g.scopes,'[]'::jsonb),
		       COUNT(DISTINCT wr.id),COUNT(DISTINCT wr.id) FILTER (WHERE wr.status='failed'),
		       COALESCE(SUM((al.metadata->>'cost_cents')::numeric) FILTER (WHERE al.metadata ? 'cost_cents'),0),
		       COALESCE(array_agg(DISTINCT al.action) FILTER (WHERE al.action IS NOT NULL),'{}')
		FROM cerebro_app a
		LEFT JOIN "user" u ON u.id=a.owner_id
		LEFT JOIN cerebro_app_grant g ON g.app_id=a.id AND g.version=a.current_version AND g.status='approved'
		LEFT JOIN cerebro_app_workflow_def wd ON wd.app_id=a.id
		LEFT JOIN cerebro_app_workflow_run wr ON wr.workflow_id=wd.id
		LEFT JOIN cerebro_app_audit_log al ON al.app_id=a.id
		WHERE a.workspace_id=$1 GROUP BY a.id,u.name,g.scopes ORDER BY a.name`, workspaceID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load app administration overview")
		return
	}
	defer rows.Close()
	items := make([]map[string]any, 0)
	for rows.Next() {
		var id, name, owner, version, status string
		var scopes json.RawMessage
		var touched []string
		var runs, failed int64
		var spend float64
		if rows.Scan(&id, &name, &owner, &version, &status, &scopes, &runs, &failed, &spend, &touched) != nil {
			writeError(w, http.StatusInternalServerError, "failed to read app administration overview")
			return
		}
		health := "healthy"
		if status == "disabled" {
			health = "disabled"
		} else if failed > 0 {
			health = "attention"
		}
		items = append(items, map[string]any{"id": id, "name": name, "owner": owner, "version": version, "status": status, "approved_scopes": scopes, "spend_cents": spend, "runs": runs, "failed_runs": failed, "health": health, "touched": touched})
	}
	writeJSON(w, http.StatusOK, map[string]any{"apps": items})
}
