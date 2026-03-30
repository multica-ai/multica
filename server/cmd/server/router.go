package main

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

func allowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return []string{"http://localhost:3000"}
	}

	parts := strings.Split(raw, ",")
	origins := make([]string, 0, len(parts))
	for _, part := range parts {
		origin := strings.TrimSpace(part)
		if origin != "" {
			origins = append(origins, origin)
		}
	}
	if len(origins) == 0 {
		return []string{"http://localhost:3000"}
	}
	return origins
}

// NewRouter creates the fully-configured Chi router with all middleware and routes.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus) chi.Router {
	queries := db.New(pool)
	emailSvc := service.NewEmailService()
	h := handler.New(queries, pool, hub, bus, emailSvc)

	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(middleware.RequestLogger)
	r.Use(chimw.Recoverer)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   allowedOrigins(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Workspace-ID", "X-Request-ID", "X-Agent-ID", "X-Task-ID"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// WebSocket
	mc := &membershipChecker{queries: queries}
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, mc, w, r)
	})

	// Auth (public)
	r.Post("/auth/send-code", h.SendCode)
	r.Post("/auth/verify-code", h.VerifyCode)

	// Daemon API routes (no user auth; daemon auth deferred to later)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Post("/pairing-sessions", h.CreateDaemonPairingSession)
		r.Get("/pairing-sessions/{token}", h.GetDaemonPairingSession)
		r.Post("/pairing-sessions/{token}/claim", h.ClaimDaemonPairingSession)

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/usage", h.ReportRuntimeUsage)
		r.Post("/runtimes/{runtimeId}/ping/{pingId}/result", h.ReportPingResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries))

		// Auth
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)

		// Issues
		r.Route("/api/issues", func(r chi.Router) {
			r.Get("/", h.ListIssues)
			r.Post("/", h.CreateIssue)
			r.Post("/batch-update", h.BatchUpdateIssues)
			r.Post("/batch-delete", h.BatchDeleteIssues)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetIssue)
				r.Put("/", h.UpdateIssue)
				r.Delete("/", h.DeleteIssue)
				r.Post("/comments", h.CreateComment)
				r.Get("/comments", h.ListComments)
				r.Get("/timeline", h.ListTimeline)
				r.Get("/subscribers", h.ListIssueSubscribers)
				r.Post("/subscribe", h.SubscribeToIssue)
				r.Post("/unsubscribe", h.UnsubscribeFromIssue)
			})
		})

		// Comments
		r.Route("/api/comments/{commentId}", func(r chi.Router) {
			r.Put("/", h.UpdateComment)
			r.Delete("/", h.DeleteComment)
		})

		// Agents
		r.Route("/api/agents", func(r chi.Router) {
			r.Get("/", h.ListAgents)
			r.Post("/", h.CreateAgent)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetAgent)
				r.Put("/", h.UpdateAgent)
				r.Delete("/", h.DeleteAgent)
				r.Get("/tasks", h.ListAgentTasks)
				r.Get("/skills", h.ListAgentSkills)
				r.Put("/skills", h.SetAgentSkills)
			})
		})

		// Skills
		r.Route("/api/skills", func(r chi.Router) {
			r.Get("/", h.ListSkills)
			r.Post("/", h.CreateSkill)
			r.Post("/import", h.ImportSkill)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetSkill)
				r.Put("/", h.UpdateSkill)
				r.Delete("/", h.DeleteSkill)
				r.Get("/files", h.ListSkillFiles)
				r.Put("/files", h.UpsertSkillFile)
				r.Delete("/files/{fileId}", h.DeleteSkillFile)
			})
		})

		r.Route("/api/runtimes", func(r chi.Router) {
			r.Get("/", h.ListAgentRuntimes)
			r.Get("/{runtimeId}/usage", h.GetRuntimeUsage)
			r.Get("/{runtimeId}/activity", h.GetRuntimeTaskActivity)
			r.Post("/{runtimeId}/ping", h.InitiatePing)
			r.Get("/{runtimeId}/ping/{pingId}", h.GetPing)
		})

		r.Post("/api/daemon/pairing-sessions/{token}/approve", h.ApproveDaemonPairingSession)

		// Personal Access Tokens
		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// Inbox
		r.Route("/api/inbox", func(r chi.Router) {
			r.Get("/", h.ListInbox)
			r.Get("/unread-count", h.CountUnreadInbox)
			r.Post("/mark-all-read", h.MarkAllInboxRead)
			r.Post("/archive-all", h.ArchiveAllInbox)
			r.Post("/archive-all-read", h.ArchiveAllReadInbox)
			r.Post("/archive-completed", h.ArchiveCompletedInbox)
			r.Post("/{id}/read", h.MarkInboxRead)
			r.Post("/{id}/archive", h.ArchiveInboxItem)
		})

		// Workspaces
		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", h.GetWorkspace)
				r.Put("/", h.UpdateWorkspace)
				r.Patch("/", h.UpdateWorkspace)
				r.Delete("/", h.DeleteWorkspace)
				r.Get("/members", h.ListMembersWithUser)
				r.Post("/members", h.CreateMember)
				r.Post("/leave", h.LeaveWorkspace)
				r.Route("/members/{memberId}", func(r chi.Router) {
					r.Patch("/", h.UpdateMember)
					r.Delete("/", h.DeleteMember)
				})
			})
		})
	})

	return r
}

// membershipChecker implements realtime.MembershipChecker using database queries.
type membershipChecker struct {
	queries *db.Queries
}

func (mc *membershipChecker) IsMember(ctx context.Context, userID, workspaceID string) bool {
	_, err := mc.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: parseUUID(workspaceID),
	})
	return err == nil
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}
	}
	return u
}
