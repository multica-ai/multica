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
	"github.com/multica-ai/multica/server/internal/middleware"
	"github.com/multica-ai/multica/server/internal/realtime"
	"github.com/multica-ai/multica/server/internal/service"
	"github.com/multica-ai/multica/server/internal/storage"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
)

var defaultOrigins = []string{
	"http://localhost:3000", // Next.js dev
	"http://localhost:5173", // electron-vite dev
	"http://localhost:5174", // electron-vite dev (fallback port)
}

func allowedOrigins() []string {
	raw := strings.TrimSpace(os.Getenv("CORS_ALLOWED_ORIGINS"))
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("FRONTEND_ORIGIN"))
	}
	if raw == "" {
		return defaultOrigins
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
		return defaultOrigins
	}
	return origins
}

// NewRouter creates the fully-configured Chi router with all middleware and routes.
func NewRouter(pool *pgxpool.Pool, hub *realtime.Hub, bus *events.Bus) chi.Router {
	queries := db.New(pool)
	emailSvc := service.NewEmailService()

	// Initialize storage with S3 as primary, fallback to local
	var store storage.Storage
	s3 := storage.NewS3StorageFromEnv()
	if s3 != nil {
		store = s3
	} else {
		local := storage.NewLocalStorageFromEnv()
		if local != nil {
			store = local
		}
	}

	cfSigner := auth.NewCloudFrontSignerFromEnv()
	
	signupConfig := handler.Config{
		AllowSignup:         os.Getenv("ALLOW_SIGNUP") != "false",
		AllowedEmails:       splitAndTrim(os.Getenv("ALLOWED_EMAILS")),
		AllowedEmailDomains: splitAndTrim(os.Getenv("ALLOWED_EMAIL_DOMAINS")),
	}
	h := handler.New(queries, pool, hub, bus, emailSvc, store, cfSigner, signupConfig)

	r := chi.NewRouter()

	// Global middleware
	r.Use(chimw.RequestID)
	r.Use(middleware.RequestLogger)
	r.Use(chimw.Recoverer)
	r.Use(middleware.ContentSecurityPolicy)
	origins := allowedOrigins()

	// Share allowed origins with WebSocket origin checker.
	realtime.SetAllowedOrigins(origins)

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   origins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Workspace-ID", "X-Workspace-Slug", "X-Request-ID", "X-Agent-ID", "X-Task-ID", "X-CSRF-Token"},
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
	pr := &patResolver{queries: queries}
	slugResolver := realtime.SlugResolver(func(ctx context.Context, slug string) (string, error) {
		ws, err := queries.GetWorkspaceBySlug(ctx, slug)
		if err != nil {
			return "", err
		}
		return util.UUIDToString(ws.ID), nil
	})
	r.Get("/ws", func(w http.ResponseWriter, r *http.Request) {
		realtime.HandleWebSocket(hub, mc, pr, slugResolver, w, r)
	})

	// Local file serving (when using local storage)
	if local, ok := store.(*storage.LocalStorage); ok {
		r.Get("/uploads/*", func(w http.ResponseWriter, r *http.Request) {
			file := strings.TrimPrefix(r.URL.Path, "/uploads/")
			local.ServeFile(w, r, file)
		})
	}

	// Auth (public)
	r.Post("/auth/send-code", h.SendCode)
	r.Post("/auth/verify-code", h.VerifyCode)
	r.Post("/auth/google", h.GoogleLogin)
	r.Post("/auth/logout", h.Logout)

	// Runtime setup (public exchange + script — token-gated)
	r.Get("/install-runtime.sh", h.ServeInstallRuntimeScript)
	r.Post("/api/runtime-setup/exchange", h.ExchangeRuntimeSetupToken)

	// Daemon API routes (require daemon token or valid user token)
	r.Route("/api/daemon", func(r chi.Router) {
		r.Use(middleware.DaemonAuth(queries))

		r.Post("/register", h.DaemonRegister)
		r.Post("/deregister", h.DaemonDeregister)
		r.Post("/heartbeat", h.DaemonHeartbeat)
		r.Get("/workspaces/{workspaceId}/repos", h.GetDaemonWorkspaceRepos)

		r.Post("/runtimes/{runtimeId}/tasks/claim", h.ClaimTaskByRuntime)
		r.Get("/runtimes/{runtimeId}/tasks/pending", h.ListPendingTasksByRuntime)
		r.Post("/runtimes/{runtimeId}/ping/{pingId}/result", h.ReportPingResult)
		r.Post("/runtimes/{runtimeId}/update/{updateId}/result", h.ReportUpdateResult)

		r.Get("/tasks/{taskId}/status", h.GetTaskStatus)
		r.Post("/tasks/{taskId}/start", h.StartTask)
		r.Post("/tasks/{taskId}/progress", h.ReportTaskProgress)
		r.Post("/tasks/{taskId}/complete", h.CompleteTask)
		r.Post("/tasks/{taskId}/fail", h.FailTask)
		r.Post("/tasks/{taskId}/usage", h.ReportTaskUsage)
		r.Post("/tasks/{taskId}/messages", h.ReportTaskMessages)
		r.Get("/tasks/{taskId}/messages", h.ListTaskMessages)

		r.Get("/issues/{issueId}/gc-check", h.GetIssueGCCheck)
	})

	// === Task-scoped allowlist ===
	// Per-task tokens (mtt_) authenticate agents while they execute one
	// task and may only reach the small set of routes the agent needs:
	// read/update its own issue, post comments, read its own agent
	// config, look up workspace members for mentions, upload
	// attachments. These routes also accept normal user auth.
	//
	// Registered before the user-only protected group below so each
	// path lives in exactly one place — chi panics on duplicates.
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))

		// Attachment upload — agents drop screenshots/logs onto their
		// issue. AllowTaskScope is a no-op (the file resolves which
		// issue/comment via request body, not URL).
		r.With(middleware.AllowTaskScope).Post("/api/upload-file", h.UploadFile)

		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			r.With(middleware.AllowTaskScopeForAgent("id")).Get("/api/agents/{id}", h.GetAgent)

			// Issue routes registered flat (not via r.Route) so they
			// share the chi routing tree with the user-only sibling
			// group's nested r.Route("/api/issues/{id}", ...). Mixing
			// flat and nested registrations of the same prefix causes
			// chi to dispatch only one of the two trees and return 405
			// for methods registered in the other.
			//
			// Static GET siblings of /{id} (e.g. /search,
			// /child-progress) MUST also be registered here, before the
			// {id} line — once a flat /{id} GET exists in this tree,
			// chi greedily routes /search → GetIssue with id="search"
			// and the user-only nested registration becomes
			// unreachable. RequireUserScope keeps them user-only so a
			// task token still gets 403, not 200.
			r.With(middleware.RequireUserScope).Get("/api/issues/search", h.SearchIssues)
			r.With(middleware.RequireUserScope).Get("/api/issues/child-progress", h.ChildIssueProgress)
			issueScope := middleware.AllowTaskScopeForIssue("id")
			r.With(issueScope).Get("/api/issues/{id}", h.GetIssue)
			r.With(issueScope).Put("/api/issues/{id}", h.UpdateIssue)
			r.With(issueScope).Get("/api/issues/{id}/comments", h.ListComments)
			r.With(issueScope).Post("/api/issues/{id}/comments", h.CreateComment)
		})

		// Workspace member listing for mention lookup. The wider
		// /api/workspaces/{id}/* tree is user-only because it carries
		// admin actions (invitations, member updates); we register
		// just /members here under AllowTaskScopeForWorkspace.
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
			r.With(middleware.AllowTaskScopeForWorkspace("id")).Get("/api/workspaces/{id}/members", h.ListMembersWithUser)
		})
	})

	// Protected API routes
	r.Group(func(r chi.Router) {
		r.Use(middleware.Auth(queries))
		r.Use(middleware.RefreshCloudFrontCookies(cfSigner))
		// Everything below this line is user-only. Task-scoped tokens
		// are rejected with 403; the allowlist group above is the
		// only place they may operate.
		r.Use(middleware.RequireUserScope)

		// --- User-scoped routes (no workspace context required) ---
		r.Get("/api/config", h.GetConfig)
		r.Get("/api/me", h.GetMe)
		r.Patch("/api/me", h.UpdateMe)
		r.Get("/api/me/profile", h.GetMyProfile)
		r.Put("/api/me/profile", h.UpsertMyProfile)
		r.Delete("/api/me/profile", h.DeleteMyProfile)
		r.Patch("/api/me/preferences", h.UpdateMyPreferences)
		r.Post("/api/cli-token", h.IssueCliToken)
		// /api/upload-file is registered in the task-allowlist group above
		// so agents can upload attachments while running a task.

		r.Route("/api/workspaces", func(r chi.Router) {
			r.Get("/", h.ListWorkspaces)
			r.Post("/", h.CreateWorkspace)
			r.Route("/{id}", func(r chi.Router) {
				// Member-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceMemberFromURL(queries, "id"))
					r.Get("/", h.GetWorkspace)
					// /members is registered in the task-allowlist group
					// above so agents can resolve mention targets.
					r.Post("/leave", h.LeaveWorkspace)
					r.Get("/invitations", h.ListWorkspaceInvitations)
				})
				// Admin-level access
				r.Group(func(r chi.Router) {
					r.Use(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner", "admin"))
					r.Put("/", h.UpdateWorkspace)
					r.Patch("/", h.UpdateWorkspace)
					r.Post("/pause-tasks", h.PauseWorkspaceTasks)
					r.Post("/members", h.CreateInvitation)
					r.Route("/members/{memberId}", func(r chi.Router) {
						r.Patch("/", h.UpdateMember)
						r.Delete("/", h.DeleteMember)
						// Per-member budget toggle + spend roll-up
						// powering the member detail page.
						r.Patch("/budget-enforcement", h.PatchMemberBudgetEnforcement)
						r.Get("/usage", h.GetMemberUsage)
						// Restricted projects this member belongs to —
						// powers the Projects tab on the detail page.
						r.Get("/projects", h.ListMemberProjects)
					})
					r.Delete("/invitations/{invitationId}", h.RevokeInvitation)
				})
				// Owner-only access
				r.With(middleware.RequireWorkspaceRoleFromURL(queries, "id", "owner")).Delete("/", h.DeleteWorkspace)
			})
		})

		// User-scoped invitation routes (no workspace context required)
		r.Get("/api/invitations", h.ListMyInvitations)
		r.Get("/api/invitations/{id}", h.GetMyInvitation)
		r.Post("/api/invitations/{id}/accept", h.AcceptInvitation)
		r.Post("/api/invitations/{id}/decline", h.DeclineInvitation)

		r.Route("/api/tokens", func(r chi.Router) {
			r.Get("/", h.ListPersonalAccessTokens)
			r.Post("/", h.CreatePersonalAccessToken)
			r.Delete("/{id}", h.RevokePersonalAccessToken)
		})

		// --- Workspace-scoped routes (all require workspace membership) ---
		r.Group(func(r chi.Router) {
			r.Use(middleware.RequireWorkspaceMember(queries))

			// Assignee frequency
			r.Get("/api/assignee-frequency", h.GetAssigneeFrequency)

			// Issues
			r.Route("/api/issues", func(r chi.Router) {
				// /search and /child-progress are registered flat in the
				// task-allowlist group above (with RequireUserScope) so
				// they share chi's routing tree with /{id} — see the
				// comment there.
				r.Get("/", h.ListIssues)
				r.Post("/", h.CreateIssue)
				r.Post("/batch-update", h.BatchUpdateIssues)
				r.Post("/batch-delete", h.BatchDeleteIssues)
				r.Route("/{id}", func(r chi.Router) {
					// GET, PUT, /comments are registered in the task-allowlist
					// group above so agents can read and update their own
					// issue while running a task.
					r.Delete("/", h.DeleteIssue)
					r.Get("/timeline", h.ListTimeline)
					r.Get("/subscribers", h.ListIssueSubscribers)
					r.Post("/subscribe", h.SubscribeToIssue)
					r.Post("/unsubscribe", h.UnsubscribeFromIssue)
					r.Get("/active-task", h.GetActiveTaskForIssue)
					r.Post("/tasks/{taskId}/cancel", h.CancelTask)
					r.Get("/task-runs", h.ListTasksByIssue)
					r.Get("/usage", h.GetIssueUsage)
					r.Post("/reactions", h.AddIssueReaction)
					r.Delete("/reactions", h.RemoveIssueReaction)
					r.Get("/attachments", h.ListAttachments)
					r.Get("/artifacts", h.ListArtifactsForIssue)
					r.Get("/children", h.ListChildIssues)
					r.Get("/work-sessions", h.ListWorkSessions)
				})
			})

			// Task messages (user-facing, not daemon auth)
			r.Get("/api/tasks/{taskId}/messages", h.ListTaskMessagesByUser)

			// Work sessions (Claude Code MCP integration)
			r.Route("/api/work-sessions", func(r chi.Router) {
				r.Post("/", h.CreateWorkSession)
				r.Get("/active", h.GetActiveWorkSession)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/messages", h.GetWorkSessionMessages)
					r.Put("/complete", h.CompleteWorkSession)
					r.Put("/name", h.UpdateWorkSessionName)
					r.Post("/messages", h.ReportWorkSessionMessages)
					r.Post("/resume", h.ResumeWorkSession)
					r.Post("/fork", h.ForkWorkSession)
				})
			})

			// Projects
			r.Route("/api/projects", func(r chi.Router) {
				r.Get("/search", h.SearchProjects)
				r.Get("/by-repo", h.GetProjectByRepo)
				r.Get("/", h.ListProjects)
				r.Post("/", h.CreateProject)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetProject)
					r.Put("/", h.UpdateProject)
					r.Delete("/", h.DeleteProject)
					r.Patch("/access", h.UpdateProjectAccess)
					r.Get("/members", h.ListProjectMembers)
					r.Post("/members", h.AddProjectMember)
					r.Delete("/members/{userId}", h.RemoveProjectMember)
					r.Get("/artifacts", h.ListArtifactsForProject)
				})
			})

			// Autopilots
			r.Route("/api/autopilots", func(r chi.Router) {
				r.Get("/", h.ListAutopilots)
				r.Post("/", h.CreateAutopilot)
				r.Route("/{id}", func(r chi.Router) {
					r.Get("/", h.GetAutopilot)
					r.Patch("/", h.UpdateAutopilot)
					r.Delete("/", h.DeleteAutopilot)
					r.Post("/trigger", h.TriggerAutopilot)
					r.Get("/runs", h.ListAutopilotRuns)
					r.Post("/triggers", h.CreateAutopilotTrigger)
					r.Route("/triggers/{triggerId}", func(r chi.Router) {
						r.Patch("/", h.UpdateAutopilotTrigger)
						r.Delete("/", h.DeleteAutopilotTrigger)
					})
				})
			})

			// Pins
			r.Route("/api/pins", func(r chi.Router) {
				r.Get("/", h.ListPins)
				r.Post("/", h.CreatePin)
				r.Put("/reorder", h.ReorderPins)
				r.Delete("/{itemType}/{itemId}", h.DeletePin)
			})

			// Attachments
			r.Get("/api/attachments/{id}", h.GetAttachmentByID)
			r.Delete("/api/attachments/{id}", h.DeleteAttachment)

			// Comments
			r.Route("/api/comments/{commentId}", func(r chi.Router) {
				r.Put("/", h.UpdateComment)
				r.Delete("/", h.DeleteComment)
				r.Post("/reactions", h.AddReaction)
				r.Delete("/reactions", h.RemoveReaction)
			})

			// Agents
			r.Route("/api/agents", func(r chi.Router) {
				r.Get("/", h.ListAgents)
				r.Post("/", h.CreateAgent)
				r.Route("/{id}", func(r chi.Router) {
					// GET is registered in the task-allowlist group above
					// so agents can read their own configuration.
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

			// Usage
			r.Route("/api/usage", func(r chi.Router) {
				r.Get("/daily", h.GetWorkspaceUsageByDay)
				r.Get("/summary", h.GetWorkspaceUsageSummary)
			})

			// Runtime setup token (workspace-scoped, generates one-line install command)
			r.Post("/api/runtime-setup/tokens", h.CreateRuntimeSetupToken)

			// Runtimes
			r.Route("/api/runtimes", func(r chi.Router) {
				r.Get("/", h.ListAgentRuntimes)
				r.Route("/{runtimeId}", func(r chi.Router) {
					r.Get("/usage", h.GetRuntimeUsage)
					r.Get("/activity", h.GetRuntimeTaskActivity)
					r.Post("/ping", h.InitiatePing)
					r.Get("/ping/{pingId}", h.GetPing)
					r.Post("/update", h.InitiateUpdate)
					r.Get("/update/{updateId}", h.GetUpdate)
					r.Patch("/sandbox", h.UpdateAgentRuntimeSandbox)
					r.Delete("/", h.DeleteAgentRuntime)
				})
			})

			// Tasks (user-facing, with ownership check)
			r.Post("/api/tasks/{taskId}/cancel", h.CancelTaskByUser)

			r.Route("/api/chat/sessions", func(r chi.Router) {
				r.Post("/", h.CreateChatSession)
				r.Get("/", h.ListChatSessions)
				r.Route("/{sessionId}", func(r chi.Router) {
					r.Get("/", h.GetChatSession)
					r.Delete("/", h.ArchiveChatSession)
					r.Post("/messages", h.SendChatMessage)
					r.Get("/messages", h.ListChatMessages)
					r.Get("/pending-task", h.GetPendingChatTask)
					r.Post("/read", h.MarkChatSessionRead)
				})
			})
			r.Get("/api/chat/messages/{messageId}/attachments", h.ListChatMessageAttachments)
			r.Get("/api/chat/pending-tasks", h.ListPendingChatTasks)

			// Artifacts
			r.Route("/api/artifacts", func(r chi.Router) {
				r.Get("/", h.SearchArtifacts)
				r.Post("/", h.CreateArtifact)
				r.Get("/{id}", h.GetArtifact)
				r.Put("/{id}", h.UpdateArtifact)
				r.Put("/{id}/scope", h.UpdateArtifactScope)
				r.Put("/{id}/folder", h.MoveArtifactToFolder)
				r.Delete("/{id}", h.DeleteArtifact)
			})
			r.Route("/api/artifact-folders", func(r chi.Router) {
				r.Get("/", h.ListArtifactFolders)
				r.Post("/", h.CreateArtifactFolder)
				r.Put("/{id}", h.UpdateArtifactFolder)
				r.Delete("/{id}", h.DeleteArtifactFolder)
			})
			r.Post("/api/artifact-uploads", h.UploadArtifactFile)

			// Inbox
			r.Route("/api/inbox", func(r chi.Router) {
				r.Get("/", h.ListInbox)
				r.Get("/unread-count", h.CountUnreadInbox)
				r.Get("/active-issue-tasks", h.ListActiveIssueTasks)
				r.Post("/mark-all-read", h.MarkAllInboxRead)
				r.Post("/archive-all", h.ArchiveAllInbox)
				r.Post("/archive-all-read", h.ArchiveAllReadInbox)
				r.Post("/archive-completed", h.ArchiveCompletedInbox)
				r.Post("/{id}/read", h.MarkInboxRead)
				r.Post("/{id}/archive", h.ArchiveInboxItem)

				// Notifications (route='notifications'). Single-item operations
				// (mark-read, archive) reuse the inbox endpoints above; only the
				// list/count/bulk endpoints are split.
				r.Get("/notifications", h.ListNotifications)
				r.Get("/notifications/unread-count", h.CountUnreadNotifications)
				r.Post("/notifications/mark-all-read", h.MarkAllNotificationsRead)
				r.Post("/notifications/archive-all", h.ArchiveAllNotifications)
				// Folders
				r.Get("/folders", h.ListInboxFolders)
				r.Post("/folders", h.CreateInboxFolder)
				r.Get("/folder-memberships", h.ListInboxFolderMemberships)
				r.Patch("/folders/{folderId}", h.UpdateInboxFolder)
				r.Put("/folders/{folderId}/parent", h.SetInboxFolderParent)
				r.Delete("/folders/{folderId}", h.DeleteInboxFolder)
				r.Post("/folders/{folderId}/items", h.AddInboxFolderItem)
				r.Delete("/folders/{folderId}/items/{itemType}/{itemId}", h.RemoveInboxFolderItem)
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

// patResolver implements realtime.PATResolver using database queries.
type patResolver struct {
	queries *db.Queries
}

func (pr *patResolver) ResolveToken(ctx context.Context, token string) (string, bool) {
	hash := auth.HashToken(token)
	pat, err := pr.queries.GetPersonalAccessTokenByHash(ctx, hash)
	if err != nil {
		return "", false
	}
	// Best-effort: update last_used_at
	go pr.queries.UpdatePersonalAccessTokenLastUsed(context.Background(), pat.ID)
	return util.UUIDToString(pat.UserID), true
}

func parseUUID(s string) pgtype.UUID {
	var u pgtype.UUID
	if err := u.Scan(s); err != nil {
		return pgtype.UUID{}
	}
	return u
}

func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	res := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			res = append(res, trimmed)
		}
	}
	return res
}
