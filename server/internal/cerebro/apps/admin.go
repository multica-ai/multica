package apps

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"
)

var allergenFormatterID = uuid.MustParse("f1540000-0000-4154-8154-000000000001")
var allergenFormatterSnapshot = json.RawMessage(`{"manifest":{"schema_version":"1","name":"Allergen Formatter","version":"1.0.0","scopes":[{"resource_type":"integration","resource_id":"ai_gateway","access":"write"}],"frontend":{"entry":"frontend/index.html"},"backend":{"entry":"backend/index.mjs"},"views":[{"id":"formatter","type":"form","title":"Format allergens"}]}}`)

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
	result, err := h.pool.Exec(r.Context(), `DELETE FROM cerebro_app WHERE id=$1`, app.ID)
	if err != nil || result.RowsAffected() != 1 {
		writeError(w, http.StatusInternalServerError, "failed to delete app")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) InstallAllergenFormatter(w http.ResponseWriter, r *http.Request) {
	workspaceID, ok := requestWorkspaceID(w, r)
	if !ok {
		return
	}
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
	result, err := tx.Exec(r.Context(), `INSERT INTO cerebro_app(id,workspace_id,slug,name,description,icon,folder,owner_id,current_version,status) VALUES($1,$2,'allergen-formatter','Allergen Formatter','Format ingredients and return regulated allergens','blocks','Operations',$3,'1.0.0','published') ON CONFLICT (id) DO NOTHING`, allergenFormatterID, workspaceID, memberID)
	if err != nil || result.RowsAffected() != 1 {
		writeError(w, 409, "Allergen Formatter is already installed")
		return
	}
	_, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_version(app_id,version,content_snapshot,release_notes,created_by) VALUES($1,'1.0.0',$2,'Initial FIR-154 release',$3)`, allergenFormatterID, allergenFormatterSnapshot, memberID)
	if err == nil {
		_, err = tx.Exec(r.Context(), `INSERT INTO cerebro_app_grant(app_id,version,scopes,status,requested_by,approved_by,approved_at) VALUES($1,'1.0.0',$2::jsonb->'manifest'->'scopes','approved',$3,$3,now())`, allergenFormatterID, allergenFormatterSnapshot, memberID)
	}
	if err != nil || tx.Commit(r.Context()) != nil {
		writeError(w, 500, "failed to install Allergen Formatter")
		return
	}
	writeJSON(w, 201, map[string]any{"id": allergenFormatterID, "name": "Allergen Formatter", "version": "1.0.0", "status": "published"})
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
