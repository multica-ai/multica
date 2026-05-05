package middleware

// CEREBRO-PATCH(middleware-scope): cerebro modification of upstream file

import (
	"context"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// chiURLParam wraps chi.URLParam so this file does not depend on chi
// at the type level (keeps the test surface small).
func chiURLParam(r *http.Request, key string) string { return chi.URLParam(r, key) }

// AuthScope identifies how a request was authenticated. User-scoped
// requests have full member rights within their workspace; task-scoped
// requests come from a per-task token (mtt_) and may only touch the
// resources tied to that one task.
type AuthScope string

const (
	ScopeUser AuthScope = "user"
	ScopeTask AuthScope = "task"
)

type scopeContextKey int

const (
	ctxKeyAuthScope scopeContextKey = iota
	ctxKeyTaskID
	ctxKeyTaskIssueID
	ctxKeyTaskAgentID
	ctxKeyTaskWorkspaceID
)

// AuthScopeFromContext returns the scope set by the auth middleware,
// defaulting to ScopeUser for backwards compatibility (callers that did
// not pass through the scope-aware Auth middleware are treated as user
// scope, matching the old behavior).
func AuthScopeFromContext(ctx context.Context) AuthScope {
	v, ok := ctx.Value(ctxKeyAuthScope).(AuthScope)
	if !ok {
		return ScopeUser
	}
	return v
}

// TaskScopeContext bundles the request-scoped data attached when a
// request is authenticated with a per-task token. All values are empty
// strings when the scope is not "task".
type TaskScopeContext struct {
	TaskID      string
	IssueID     string
	AgentID     string
	WorkspaceID string
}

// TaskScopeFromContext returns the task-scope data set by the auth
// middleware. Callers should check `Scope == ScopeTask` first.
func TaskScopeFromContext(ctx context.Context) TaskScopeContext {
	t, _ := ctx.Value(ctxKeyTaskID).(string)
	i, _ := ctx.Value(ctxKeyTaskIssueID).(string)
	a, _ := ctx.Value(ctxKeyTaskAgentID).(string)
	w, _ := ctx.Value(ctxKeyTaskWorkspaceID).(string)
	return TaskScopeContext{TaskID: t, IssueID: i, AgentID: a, WorkspaceID: w}
}

func withTaskScope(ctx context.Context, t TaskScopeContext) context.Context {
	ctx = context.WithValue(ctx, ctxKeyAuthScope, ScopeTask)
	ctx = context.WithValue(ctx, ctxKeyTaskID, t.TaskID)
	ctx = context.WithValue(ctx, ctxKeyTaskIssueID, t.IssueID)
	ctx = context.WithValue(ctx, ctxKeyTaskAgentID, t.AgentID)
	ctx = context.WithValue(ctx, ctxKeyTaskWorkspaceID, t.WorkspaceID)
	return ctx
}

func withUserScope(ctx context.Context) context.Context {
	return context.WithValue(ctx, ctxKeyAuthScope, ScopeUser)
}

// RequireUserScope rejects requests authenticated via a per-task token.
// It is the default policy for member-facing routes — a compromised
// agent must not be able to use the task token for general access.
func RequireUserScope(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if AuthScopeFromContext(r.Context()) == ScopeTask {
			http.Error(w, `{"error":"task-scoped tokens cannot access this route"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// AllowTaskScopeForIssue lets a route be reached by a task-scoped token
// only when the {param} URL parameter equals the task's bound issue ID.
// User-scoped requests pass through unchanged.
func AllowTaskScopeForIssue(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if AuthScopeFromContext(r.Context()) == ScopeTask {
				ts := TaskScopeFromContext(r.Context())
				urlID := chiURLParam(r, param)
				if urlID == "" || urlID != ts.IssueID {
					http.Error(w, `{"error":"task token cannot access this issue"}`, http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AllowTaskScopeForAgent lets a route be reached by a task-scoped token
// only when the {param} URL parameter equals the task's agent ID.
func AllowTaskScopeForAgent(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if AuthScopeFromContext(r.Context()) == ScopeTask {
				ts := TaskScopeFromContext(r.Context())
				urlID := chiURLParam(r, param)
				if urlID == "" || urlID != ts.AgentID {
					http.Error(w, `{"error":"task token cannot access this agent"}`, http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AllowTaskScopeForWorkspace lets a route be reached by a task-scoped
// token only when the {param} URL parameter resolves to the task's
// workspace (matched directly by UUID or via slug → UUID lookup).
// Slug acceptance keeps the behavior parallel with the rest of the
// workspace middleware which prefers slugs.
func AllowTaskScopeForWorkspace(param string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if AuthScopeFromContext(r.Context()) == ScopeTask {
				ts := TaskScopeFromContext(r.Context())
				urlID := chiURLParam(r, param)
				resolved := WorkspaceIDFromContext(r.Context())
				if (urlID == "" || urlID != ts.WorkspaceID) && resolved != ts.WorkspaceID {
					http.Error(w, `{"error":"task token cannot access this workspace"}`, http.StatusForbidden)
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}

// AllowTaskScope is the no-op opt-in: a route that is safe to access
// for either scope (e.g. listing workspace members for mention lookup,
// reading own task messages).
func AllowTaskScope(next http.Handler) http.Handler { return next }
