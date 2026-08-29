package main

// MULTICA-LOCAL: Rewritten for SQLite — removed pgx, S3, CloudFront, email service.

import (
	"context"
	"database/sql"
	"net/http"
	"os"
	"strings"

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"

	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/storage"
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
func NewRouter(sqlDB *sql.DB, hub *realtime.Hub, bus *events.Bus) chi.Router {
	queries := db.New(sqlDB)
	stor := storage.NewLocalStorage()
	h := handler.New(queries, sqlDB, hub, bus, stor)

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

	// MULTICA-LOCAL: Local auto-login endpoint (no email/code needed).
	r.Post("/auth/local-login", h.LocalLogin)
	// Keep send-code/verify-code for backwards compatibility.
	r.Post("/auth/send-code", h.SendCode)
	r.Post("/auth/verify-code", h.VerifyCode)

	// Daemon API routes
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.Auth(queries))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/usage", h.ReportRuntimeUsage)
		r.Post("/runtimes/{runtimeId}/ping/{pingId}/result", h.ReportPingResult)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
		r.Post("/tasks/{taskId}/messages", h.ReportTaskMessages)
		r.Get("/tasks/{taskId}/messages", h.ListTaskMessages)

		// Channel endpoints for agent execution context
		r.Get("/channels/{channelId}/messages", h.ListChannelMessages)
		r.Post("/channels/{channelId}/messages", h.CreateChannelMessage)
		r.Post("/channels/{channelId}/suggestions", h.CreateSuggestion)
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries))

		// --- User-scoped routes ---
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		r.Post("/api/upload-file", h.UploadFile)

		// MULTICA-LOCAL: Serve locally stored files.
		r.Get("/api/files/{id}", h.ServeFile)

		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)
					r.Get("/members", h.ListMembersWithUser)
					r.Post("/leave", h.LeaveWorkspace)
				})
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/members", h.CreateMember)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
					})
				})
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)
			})
		})

		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// --- Workspace-scoped routes ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			// Labels
			r.Route("/api/labels", func(r chi.Router) {
				r.Get("/", h.ListLabels)
				r.Post("/", h.CreateLabel)
				r.Route("/{labelId}", func(r chi.Router) {
					r.Put("/", h.UpdateLabel)
					r.Delete("/", h.DeleteLabel)
				})
			})

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
					r.Get("/active-task", h.GetActiveTaskForIssue)
					r.Post("/tasks/{taskId}/cancel", h.CancelTask)
					r.Get("/task-runs", h.ListTasksByIssue)
					r.Post("/reactions", h.AddIssueReaction)
					r.Delete("/reactions", h.RemoveIssueReaction)
					r.Get("/attachments", h.ListAttachments)
				// MULTICA-LOCAL: Stage 4 — Direct agent execution
				r.Post("/run-agent", h.RunAgentOnIssue)
				r.Get("/agent-diff", h.GetIssueDiff)
				r.Post("/agent-commit", h.CommitAgentChanges)
				r.Get("/labels", h.ListIssueLabels)
				r.Post("/labels", h.AddIssueLabel)
				r.Delete("/labels", h.RemoveIssueLabel)
				r.Get("/dependencies", h.ListIssueDependencies)
				r.Post("/dependencies", h.CreateIssueDependency)
				r.Delete("/dependencies/{depId}", h.DeleteIssueDependency))
				})
			})

			r.Get("/api/attachments/{id}", h.GetAttachmentByID)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/", h.CreateAgent)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAgent)
					r.Put("/", h.UpdateAgent)
					r.Post("/archive", h.ArchiveAgent)
					r.Post("/restore", h.RestoreAgent)
					r.Get("/tasks", h.ListAgentTasks)
					r.Get("/skills", h.ListAgentSkills)
					r.Put("/skills", h.SetAgentSkills)
				})
			})

			r.Route("/api/skills", func(r chi.Router) {
				r.Get("/", h.ListSkills)
				r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/", h.CreateSkill)
				r.With(middleware.RequireWorkspaceRole(queries, "owner", "admin")).Post("/import", h.ImportSkill)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetSkill)
					r.Put("/", h.UpdateSkill)
					r.Delete("/", h.DeleteSkill)
					r.Get("/files", h.ListSkillFiles)
					r.Put("/files", h.UpsertSkillFile)
					r.Delete("/files/{fileId}", h.DeleteSkillFile)
				})
			})

			// MULTICA-LOCAL: Stage 4 — Local agent runtime management & skills
			r.Route("/api/local", func(r chi.Router) {
				r.Route("/agents", func(r chi.Router) {
					r.Get("/", h.ListLocalAgents)
					r.Post("/detect", h.DetectLocalAgents)
					r.Post("/health-check", h.HealthCheckLocalAgents)
					r.Put("/{provider}/path", h.SetLocalAgentPath)
				})
				r.Route("/skills", func(r chi.Router) {
					r.Get("/", h.ListLocalSkills)
					r.Post("/", h.CreateLocalSkill)
					r.Put("/{id}", h.UpdateLocalSkill)
					r.Delete("/{id}", h.DeleteLocalSkill)
				})
			})

			// Runtimes
			r.Route("/api/runtimes", func(r chi.Router) {
				r.Get("/", h.ListAgentRuntimes)
				r.Get("/{runtimeId}/usage", h.GetRuntimeUsage)
				r.Get("/{runtimeId}/activity", h.GetRuntimeTaskActivity)
				r.Post("/{runtimeId}/ping", h.InitiatePing)
				r.Get("/{runtimeId}/ping/{pingId}", h.GetPing)
				r.Post("/{runtimeId}/update", h.InitiateUpdate)
				r.Get("/{runtimeId}/update/{updateId}", h.GetUpdate)
			})

			// Projects
			r.Route("/api/projects", func(r chi.Router) {
				r.Get("/", h.ListProjects)
				r.Post("/", h.CreateProject)
				r.Route("/{projectId}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Put("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					r.Get("/commits", h.GetProjectCommits)
					r.Get("/commits/{sha}", h.GetProjectCommitDetail)
					r.Post("/commits", h.CreateProjectCommit)
					r.Get("/status", h.GetProjectStatus)
					r.Get("/branches", h.GetProjectBranches)
					r.Post("/branches", h.CreateProjectBranch)
					r.Post("/checkout", h.CheckoutProjectBranch)
					r.Get("/diff", h.GetProjectDiff)
					r.Post("/git-init", h.InitProjectGit)
				})
			})

			// Channels (Slock)
			r.Route("/api/channels", func(r chi.Router) {
				r.Get("/", h.ListChannels)
				r.Post("/", h.CreateChannel)
				r.Route("/{channelId}", func(r chi.Router) {
					r.Get("/", h.GetChannel)
					r.Put("/", h.UpdateChannel)
					r.Delete("/", h.DeleteChannel)
					r.Get("/members", h.ListChannelMembers)
					r.Post("/members", h.AddChannelMember)
					r.Delete("/members", h.RemoveChannelMember)
					r.Get("/messages", h.ListChannelMessages)
					r.Post("/messages", h.CreateChannelMessage)
					r.Post("/create-issue", h.CreateIssueFromChannelMessage)
					r.Get("/suggestions", h.ListSuggestions)
					r.Post("/suggestions", h.CreateSuggestion)
					r.Post("/suggestions/{suggestionId}/approve", h.ApproveSuggestion)
					r.Post("/suggestions/{suggestionId}/dismiss", h.DismissSuggestion)
				})
			})

			r.Route("/api/channel-messages/{messageId}", func(r chi.Router) {
				r.Put("/", h.UpdateChannelMessage)
				r.Delete("/", h.DeleteChannelMessage)
			})

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
		})
	})

	return r
}

type membershipChecker struct {
	queries *db.Queries
}

func (mc *membershipChecker) IsMember(ctx context.Context, userID, workspaceID string) bool {
	_, err := mc.queries.GetMemberByUserAndWorkspace(ctx, db.GetMemberByUserAndWorkspaceParams{
		UserID:      userID,
		WorkspaceID: workspaceID,
	})
	return err == nil
}
