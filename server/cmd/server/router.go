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

	"github.com/multica-ai/multica/server/internal/auth"
	"github.com/multica-ai/multica/server/internal/events"
	"github.com/multica-ai/multica/server/internal/handler"
	"github.com/multica-ai/multica/server/internal/llm"
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
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

// RouterDeps holds services created during router initialization that the server
// needs for background jobs (scheduler, sweeper, etc.).
type RouterDeps struct {
	Queries    *db.Queries
	ReviewSvc  *service.ReviewService
	PlanSvc    *service.DailyPlanService
	StandupSvc *service.StandupService
}

// NewRouter creates the fully-configured Chi router with all middleware and routes.
// It also returns RouterDeps so main can wire up the scheduler and other background jobs.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus) (chi.Router, RouterDeps) {
	queries := db.New(pool)
	emailSvc := service.NewEmailService()
	cfSigner := auth.NewCloudFrontSignerFromEnv()

	// Choose storage backend: S3 if configured, local filesystem otherwise.
	var fileStorage storage.Storage
	var localUploadDir string
	if s3 := storage.NewS3StorageFromEnv(); s3 != nil {
		fileStorage = s3
	} else {
		local := storage.NewLocalStorage()
		fileStorage = local
		localUploadDir = local.Dir()
	}

	h := handler.New(queries, pool, hub, bus, emailSvc, fileStorage, cfSigner)

	// Initialize daily review, plan, and standup handlers.
	llmClient := llm.NewClient()
	reviewSvc := service.NewReviewService(queries, llmClient)
	planSvc := service.NewDailyPlanService(queries, llmClient)
	standupSvc := service.NewStandupService(queries, llmClient)
	reviewHandler := handler.NewDailyReviewHandler(reviewSvc)
	planHandler := handler.NewDailyPlanHandler(planSvc)
	automationHandler := handler.NewAutomationHandler(queries, standupSvc)

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

	// Serve locally uploaded files when running without S3 (development mode).
	if localUploadDir != "" {
		r.Handle("/uploads/*", http.StripPrefix("/uploads/", http.FileServer(http.Dir(localUploadDir))))
	}

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

	// Public invite info (no auth required)
	r.Get("/api/invite/{token}", h.GetInviteInfo)

	// Daemon API routes (all require a valid token)
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
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// --- User-scoped routes (no workspace context required) ---
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		r.Post("/api/upload-file", h.UploadFile)

		// Join workspace via invite link (requires auth, no workspace membership)
		r.Post("/api/invite/{token}/join", h.JoinByInviteToken)

		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				// Member-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)
					r.Get("/members", h.ListMembersWithUser)
					r.Post("/leave", h.LeaveWorkspace)
					// AI features
					r.Get("/ai/settings", h.GetAISettings)
					r.Post("/ai/settings", h.UpdateAISettings)
					r.Post("/ai/label", h.SuggestLabelsHandler)
					r.Post("/ai/schedule", h.SuggestScheduleHandler)
				})
				// Admin-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/members", h.CreateMember)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
					})
					// Invite link management (admin/owner only)
					r.Get("/invite-link", h.GetWorkspaceWithInviteToken)
					r.Post("/invite-link/reset", h.ResetInviteLink)
					r.Delete("/invite-link", h.DisableInviteLink)
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)
			})
		})

		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// --- Workspace-scoped routes (all require workspace membership) ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			r.Route("/api/data", func(r chi.Router) {
				r.Get("/export", h.ExportWorkspaceData)
				r.Post("/import/dry-run", h.DryRunWorkspaceImport)
				r.Post("/import/apply", h.ApplyWorkspaceImport)
			})

			r.Post("/api/transcriptions", h.TranscribeAudio)

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				r.Get("/", h.ListIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/bulk", h.BulkCreateIssues)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-archive", h.BatchArchiveIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetIssue)
					r.Put("/", h.UpdateIssue)
					r.Delete("/", h.DeleteIssue)
					r.Post("/archive", h.ArchiveIssue)
					r.Post("/restore", h.RestoreIssue)
					r.Post("/worklogs", h.CreateWorklog)
					r.Get("/worklogs", h.ListWorklogs)
					r.Post("/labels", h.AddIssueLabel)
					r.Delete("/labels/{labelId}", h.RemoveIssueLabel)
					r.Post("/dependencies", h.AddIssueDependency)
					r.Delete("/dependencies/{dependencyId}", h.RemoveIssueDependency)
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
					r.Post("/attachments/link", h.LinkIssueAttachments)
					// Time entries linked to this issue.
					r.Post("/time-entries", h.CreateTimeEntry)
					r.Get("/time-entries", h.ListIssueTimeEntries)
				})
			})

			// Time entries (standalone, current user)
			r.Route("/api/time-entries", func(r chi.Router) {
				r.Post("/", h.CreateTimeEntry)
				r.Post("/switch", h.SwitchTimeEntry)
				r.Get("/", h.ListTimeEntries)
				r.Get("/current", h.GetCurrentTimeEntry)
				// Workspace-level aggregation for team time review.
				r.Get("/team-stats", h.GetTeamTimeStats)
				r.Route("/{entry_id}", func(r chi.Router) {
					r.Patch("/", h.UpdateTimeEntry)
					r.Delete("/", h.DeleteTimeEntry)
					r.Patch("/stop", h.StopTimeEntry)
					r.Post("/labels", h.AddLabelToTimeEntry)
					r.Put("/labels", h.SetTimeEntryLabels)
					r.Delete("/labels/{labelId}", h.RemoveLabelFromTimeEntry)
				})
			})

			r.Route("/api/time-entry-labels", func(r chi.Router) {
				r.Get("/", h.ListTimeEntryLabels)
				r.Post("/", h.CreateTimeEntryLabel)
				r.Patch("/{id}", h.UpdateTimeEntryLabel)
				r.Delete("/{id}", h.DeleteTimeEntryLabel)
			})

			r.Route("/api/focus", func(r chi.Router) {
				r.Get("/current", h.GetCurrentFocus)
				r.Patch("/current", h.UpdateCurrentFocus)
				r.Get("/events", h.ListFocusEvents)
				r.Post("/start", h.StartFocus)
				r.Post("/pause", h.PauseFocus)
				r.Post("/resume", h.ResumeFocus)
				r.Post("/complete", h.CompleteFocus)
				r.Post("/abandon", h.AbandonFocus)
				r.Post("/break/start", h.StartFocusBreak)
				r.Post("/break/skip", h.SkipFocusBreak)
				r.Post("/break/complete", h.CompleteFocusBreak)
			})

			// Projects
			r.Route("/api/projects", func(r chi.Router) {
				r.Get("/", h.ListProjects)
				r.Post("/", h.CreateProject)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Put("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					// Time spent aggregated for this project.
					r.Get("/time-stats", h.GetProjectTimeStats)
				})
			})

			r.Route("/api/labels", func(r chi.Router) {
				r.Get("/", h.ListLabels)
				r.Post("/", h.CreateLabel)
				r.Patch("/{id}", h.UpdateLabel)
				r.Delete("/{id}", h.DeleteLabel)
			})

			// Attachments
			r.Get("/api/attachments/{id}", h.GetAttachmentByID)
			r.Get("/api/attachments/{id}/download", h.DownloadAttachment)
			r.Patch("/api/attachments/{id}", h.UpdateAttachment)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			// Comments
			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			r.Route("/api/worklogs/{id}", func(r chi.Router) {
				r.Patch("/", h.UpdateWorklog)
				r.Delete("/", h.DeleteWorklog)
			})

			// Agents
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

			// Skills
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

			// Runtimes
			r.Route("/api/runtimes", func(r chi.Router) {
				r.Post("/", h.CreateAgentRuntime)
				r.Get("/", h.ListAgentRuntimes)
				r.Get("/{runtimeId}/usage", h.GetRuntimeUsage)
				r.Get("/{runtimeId}/activity", h.GetRuntimeTaskActivity)
				r.Post("/{runtimeId}/ping", h.InitiatePing)
				r.Get("/{runtimeId}/ping/{pingId}", h.GetPing)
				r.Post("/{runtimeId}/update", h.InitiateUpdate)
				r.Get("/{runtimeId}/update/{updateId}", h.GetUpdate)
			})

			// Inbox
			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)
				r.Get("/unread-count", h.CountUnreadInbox)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/handle-completed", h.HandleCompletedInbox)
				r.Post("/batch-handle", h.BatchHandleInbox)
				r.Post("/batch-dismiss", h.BatchDismissInbox)
				r.Post("/batch-snooze", h.BatchSnoozeInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/archive", h.ArchiveInboxItem)
				r.Post("/{id}/handle", h.HandleInboxItem)
				r.Post("/{id}/dismiss", h.DismissInboxItem)
				r.Post("/{id}/snooze", h.SnoozeInboxItem)
			})

			// Notification preferences
			r.Route("/api/notification-preferences", func(r chi.Router) {
				r.Get("/", h.GetNotificationPreference)
				r.Put("/", h.UpsertNotificationPreference)
				r.Post("/test", h.TestNotificationPreference)
			})

			// Daily Reviews
			r.Route("/api/daily-reviews", func(r chi.Router) {
				r.Post("/generate", reviewHandler.GenerateReview)
				r.Get("/today", reviewHandler.GetTodayReview)
				r.Get("/", reviewHandler.ListReviews)
				r.Post("/{id}/confirm", reviewHandler.ConfirmReview)
			})

			// Daily Plans
			r.Route("/api/daily-plans", func(r chi.Router) {
				r.Post("/generate", planHandler.GeneratePlan)
				r.Get("/tomorrow", planHandler.GetTomorrowPlan)
				r.Get("/", planHandler.ListPlans)
				r.Post("/{id}/confirm", planHandler.ConfirmPlan)
			})

			// Automation templates and rules
			r.Route("/api/automation", func(r chi.Router) {
				r.Get("/templates", automationHandler.ListTemplates)
				r.Post("/rules", automationHandler.EnableRule)
				r.Delete("/rules/{templateId}", automationHandler.DisableRule)
				r.Post("/rules/{templateId}/run", automationHandler.RunRule)
			})

			// Pomodoro session persistence
			ph := handler.NewPomodoroHandler(queries)
			r.Get("/api/pomodoro/current", ph.GetCurrentPomodoro)
			r.Get("/api/pomodoro/history", ph.GetPomodoroHistory)
			r.Post("/api/pomodoro/start", ph.StartPomodoro)
			r.Post("/api/pomodoro/pause", ph.PausePomodoro)
			r.Post("/api/pomodoro/complete", ph.CompletePomodoro)
			r.Post("/api/pomodoro/reset", ph.ResetPomodoro)
		})
	})

	if err := registerSwaggerRoutes(r); err != nil {
		panic(err)
	}

	r.NotFound(newFrontendHandler().ServeHTTP)

	deps := RouterDeps{
		Queries:    queries,
		ReviewSvc:  reviewSvc,
		PlanSvc:    planSvc,
		StandupSvc: standupSvc,
	}
	return r, deps
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
