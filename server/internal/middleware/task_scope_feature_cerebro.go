package middleware

// CEREBRO-PATCH(task-scope-feature-flag): FIR-4076 narrow workspace kill switch.

import (
	"context"
	"net/http"

	"github.com/jackc/pgx/v5/pgtype"

	cerebrodb "github.com/multica-ai/multica/server/internal/cerebro/db/generated"
	"github.com/multica-ai/multica/server/internal/util"
)

const taskScopeEnforcementFlagKey = "cerebro_task_scope_enforcement"

type taskScopeFlagReader interface {
	ListCerebroWorkspaceFeatureFlags(context.Context, pgtype.UUID) ([]cerebrodb.ListCerebroWorkspaceFeatureFlagsRow, error)
}

type taskScopeEnforcementContextKey struct{}

func withTaskScopeEnforcement(ctx context.Context, enabled bool) context.Context {
	return context.WithValue(ctx, taskScopeEnforcementContextKey{}, enabled)
}

func taskScopeEnforcementEnabled(ctx context.Context) bool {
	enabled, present := ctx.Value(taskScopeEnforcementContextKey{}).(bool)
	return !present || enabled
}

// ResolveTaskScopeEnforcement resolves the default-on workspace flag once for
// task-token requests. Missing state and lookup failures retain enforcement;
// only an explicit workspace-level off value opens the three route matchers.
func ResolveTaskScopeEnforcement(flags taskScopeFlagReader) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if AuthScopeFromContext(r.Context()) != ScopeTask {
				next.ServeHTTP(w, r)
				return
			}

			enabled := true
			workspaceID, err := util.ParseUUID(TaskScopeFromContext(r.Context()).WorkspaceID)
			if err == nil && flags != nil {
				if rows, readErr := flags.ListCerebroWorkspaceFeatureFlags(r.Context(), workspaceID); readErr == nil {
					for _, row := range rows {
						if row.FlagKey == taskScopeEnforcementFlagKey {
							enabled = row.Enabled
							break
						}
					}
				}
			}
			next.ServeHTTP(w, r.WithContext(withTaskScopeEnforcement(r.Context(), enabled)))
		})
	}
}
