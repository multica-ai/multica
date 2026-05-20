package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/issueguard"
	"github.com/multica-ai/multica/server/internal/logger"
	"github.com/multica-ai/multica/server/internal/util"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Upper bound on free-text fields. `cloudWaitlistReasonMaxLen` is a
// product cap ("we don't need an essay for a waitlist"); the body-size
// cap further down is defense in depth against arbitrary storage
// abuse via the JSON body.
const (
	cloudWaitlistReasonMaxLen = 500

	// PatchOnboarding body is a tiny JSON with at most a 3-question
	// questionnaire. 16 KiB is ~10x the realistic ceiling — it's the
	// minimum that keeps the door open for future fields without
	// letting a malicious user stuff the JSONB column.
	patchOnboardingBodyLimit = 16 * 1024

	// Runtime bootstrap is just workspace_id + runtime_id, but keep a
	// separate small cap so this endpoint cannot be used as bulk storage.
	runtimeBootstrapBodyLimit = 8 * 1024

	// Import payload contains the full starter-content template. Each
	// sub-issue's markdown description is ~2 KiB; with ~8 sub-issues,
	// a welcome issue (~3 KiB), and a project description, 64 KiB is
	// comfortably above realistic and still bounded.
	importStarterContentBodyLimit = 64 * 1024
)

const (
	onboardingAssistantName = "Multica Helper"
	onboardingIssueTitle    = "Start here: learn Multica with Multica Helper"
	onboardingAgentTemplate = "multica_helper"
)

const onboardingAssistantDescription = "Default guide for your first Multica workspace."

const onboardingAssistantAvatarURL = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 128 128'%3E%3Crect width='128' height='128' rx='30' fill='%23111217'/%3E%3Cpath d='M28 76c8-22 22-33 42-33 15 0 26 7 32 20' fill='none' stroke='%23ffffff' stroke-width='10' stroke-linecap='round'/%3E%3Cpath d='M38 88c13 13 39 17 58 1' fill='none' stroke='%238EE3C8' stroke-width='8' stroke-linecap='round'/%3E%3Ccircle cx='48' cy='56' r='7' fill='%23ffffff'/%3E%3Ccircle cx='78' cy='56' r='7' fill='%23ffffff'/%3E%3Cpath d='M64 20v14' stroke='%238EE3C8' stroke-width='8' stroke-linecap='round'/%3E%3Ccircle cx='64' cy='16' r='6' fill='%238EE3C8'/%3E%3C/svg%3E"

const onboardingAssistantInstructions = `You are Multica Helper, the user's first Multica teammate. Your job is to onboard them inside the first issue.

When the onboarding issue starts, leave a concise first comment that:
1. Explains that issues are where work happens in Multica.
2. Tells the user they can reply in the thread or @mention you to continue.
3. Asks for one concrete task they want help with.
4. Mentions that they can create more agents and connect more runtimes later.

Keep the tone practical. Do not create extra issues or projects unless the user asks.`

const onboardingIssueDescription = `Welcome to Multica.

This is your guided first run. Multica Helper is assigned to this issue and will help you try the core workflow:

1. Read Multica Helper's first comment.
2. Reply with something you want to build, fix, write, or plan.
3. @mention Multica Helper when you want it to continue.
4. Open Agents and Runtimes later when you want to customize the teammate or the computer it runs on.

You can close this issue when the workflow makes sense.`

// completeOnboardingRequest carries the client's view of which exit the
// user took from the flow. The client is the only place that knows
// whether Step 3's runtime connect was skipped, whether the cloud
// waitlist form was submitted, or whether Welcome's "I've done this
// before" path was used. Unknown/missing → OnboardingPathUnknown so
// legacy clients still complete the flow cleanly, just without a
// funnel-ready label.
type completeOnboardingRequest struct {
	CompletionPath string `json:"completion_path,omitempty"`
	WorkspaceID    string `json:"workspace_id,omitempty"`
}

var validCompletionPaths = map[string]struct{}{
	analytics.OnboardingPathFull:           {},
	analytics.OnboardingPathRuntimeSkipped: {},
	analytics.OnboardingPathCloudWaitlist:  {},
	analytics.OnboardingPathSkipExisting:   {},
	analytics.OnboardingPathInviteAccept:   {},
}

// CompleteOnboarding marks the authenticated user as having completed
// onboarding. Idempotent: the underlying query uses COALESCE so the
// original timestamp is preserved if called more than once.
//
// Emits `onboarding_completed` exactly once — the first call that
// actually flips `onboarded_at` from NULL. Subsequent calls are still
// 200 OK (for client-side retries) but skip the event so the funnel
// counts honest first-completion.
//
// When the client supplies workspace_id and the workspace has no runtime
// yet, this also seeds the "install a runtime" issue (idempotent), so the
// "I've done this before" / Skip exits land on a concrete next step.
func (h *Handler) CompleteOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Body is optional — an empty body is a legal legacy call.
	var req completeOnboardingRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	// Resolve workspace_id (if any) up front so a malformed value short-
	// circuits with 400 before we touch the DB.
	var wsUUID pgtype.UUID
	hasWorkspace := false
	if req.WorkspaceID != "" {
		parsed, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
		if !ok {
			return
		}
		wsUUID = parsed
		req.WorkspaceID = uuidToString(wsUUID)
		hasWorkspace = true
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Read the prior state so we can detect "was this call the one that
	// actually completed onboarding?" — MarkUserOnboarded uses COALESCE
	// and returns the preserved timestamp on repeat calls, which is not
	// the signal we need for the funnel.
	before, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid

	user, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}

	var seededIssue db.Issue
	seeded := false
	if hasWorkspace {
		if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
			UserID:      parseUUID(userID),
			WorkspaceID: wsUUID,
		}); err == nil {
			seededIssue, seeded, err = ensureNoRuntimeOnboardingIssue(r.Context(), qtx, wsUUID, parseUUID(userID), before.Language)
			if err != nil {
				slog.Warn("complete onboarding: ensure install-runtime issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
				writeError(w, http.StatusInternalServerError, "failed to seed onboarding issue")
				return
			}
			if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), user.StarterContentState); err != nil {
				writeError(w, http.StatusInternalServerError, "failed to record starter content state")
				return
			}
		}
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to complete onboarding")
		return
	}

	if seeded {
		prefix := h.getIssuePrefix(r.Context(), seededIssue.WorkspaceID)
		resp := issueToResponse(seededIssue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		h.Analytics.Capture(analytics.IssueCreated(
			userID,
			req.WorkspaceID,
			uuidToString(seededIssue.ID),
			"",
			"",
			"",
			analytics.SourceOnboarding,
		))
	}

	if firstCompletion {
		path := req.CompletionPath
		if _, ok := validCompletionPaths[path]; !ok {
			path = analytics.OnboardingPathUnknown
		}
		onboardedAt := ""
		if user.OnboardedAt.Valid {
			onboardedAt = user.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		h.Analytics.Capture(analytics.OnboardingCompleted(
			userID,
			req.WorkspaceID,
			path,
			onboardedAt,
			user.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type bootstrapOnboardingRuntimeRequest struct {
	WorkspaceID string `json:"workspace_id"`
	RuntimeID   string `json:"runtime_id"`
}

type bootstrapOnboardingRuntimeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	AgentID     string `json:"agent_id"`
	IssueID     string `json:"issue_id"`
}

type bootstrapOnboardingNoRuntimeRequest struct {
	WorkspaceID string `json:"workspace_id"`
}

type bootstrapOnboardingNoRuntimeResponse struct {
	WorkspaceID string `json:"workspace_id"`
	IssueID     string `json:"issue_id"`
}

// BootstrapOnboardingRuntime is the runtime-connected onboarding exit:
// create or reuse one default helper agent, create or reuse one onboarding
// issue assigned to it, and mark onboarding complete. The flow is
// deliberately one issue, not a seeded project with many tasks.
func (h *Handler) BootstrapOnboardingRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, runtimeBootstrapBodyLimit)
	var req bootstrapOnboardingRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	if req.RuntimeID == "" {
		writeError(w, http.StatusBadRequest, "runtime_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	runtimeUUID, ok := parseUUIDOrBadRequest(w, req.RuntimeID, "runtime_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	userBefore, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !userBefore.OnboardedAt.Valid

	member, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	runtime, err := qtx.GetAgentRuntimeForWorkspace(r.Context(), db.GetAgentRuntimeForWorkspaceParams{
		ID:          runtimeUUID,
		WorkspaceID: wsUUID,
	})
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid runtime_id")
		return
	}
	if !canUseRuntimeForAgent(member, runtime) {
		writeError(w, http.StatusForbidden, "this runtime is private; only its owner or a workspace admin can create agents on it")
		return
	}

	agents, err := qtx.ListAgents(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	isFirstAgent := len(agents) == 0

	var assistant db.Agent
	assistantCreated := false
	// Only reuse helpers this flow could have created: name match AND
	// workspace-visible. Skipping private agents is the access-control
	// gate — a private "Multica Helper" owned by another member must not
	// be auto-assigned to the bootstrap issue, which would bypass
	// canAccessPrivateAgent and trigger a task as that private agent.
	for _, existing := range agents {
		if existing.Name == onboardingAssistantName && existing.Visibility == "workspace" {
			assistant = existing
			break
		}
	}
	if !assistant.ID.Valid {
		assistant, err = qtx.CreateAgent(r.Context(), db.CreateAgentParams{
			WorkspaceID:        wsUUID,
			Name:               onboardingAssistantName,
			Description:        onboardingAssistantDescription,
			AvatarUrl:          pgtype.Text{String: onboardingAssistantAvatarURL, Valid: true},
			RuntimeMode:        runtime.RuntimeMode,
			RuntimeConfig:      []byte("{}"),
			RuntimeID:          runtime.ID,
			Visibility:         "workspace",
			MaxConcurrentTasks: 6,
			OwnerID:            parseUUID(userID),
			Instructions:       onboardingAssistantInstructions,
			CustomEnv:          []byte("{}"),
			CustomArgs:         []byte("[]"),
			McpConfig:          nil,
			Model:              pgtype.Text{},
		})
		if err != nil {
			slog.Warn("bootstrap onboarding: create assistant failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding assistant")
			return
		}
		assistantCreated = true
	}

	var emptyUUID pgtype.UUID
	issue, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(),
		qtx,
		wsUUID,
		emptyUUID,
		emptyUUID,
		onboardingIssueTitle,
		false,
	)
	if err != nil {
		slog.Warn("bootstrap onboarding: duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}
	issueCreated := false
	if !foundIssue {
		issueNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         onboardingIssueTitle,
			Description:   strOrNullText(onboardingIssueDescription),
			Status:        "todo",
			Priority:      "high",
			AssigneeType:  pgtype.Text{String: "agent", Valid: true},
			AssigneeID:    assistant.ID,
			CreatorType:   "member",
			CreatorID:     parseUUID(userID),
			ParentIssueID: emptyUUID,
			Position:      0,
			Number:        issueNumber,
			ProjectID:     emptyUUID,
		})
		if err != nil {
			slog.Warn("bootstrap onboarding: create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}
	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), updatedUser.StarterContentState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finish onboarding")
		return
	}

	if assistantCreated {
		resp := agentToResponse(assistant)
		h.publish(protocol.EventAgentCreated, req.WorkspaceID, "member", userID, map[string]any{"agent": resp})
		h.Analytics.Capture(analytics.AgentCreated(
			userID,
			req.WorkspaceID,
			uuidToString(assistant.ID),
			runtime.Provider,
			runtime.RuntimeMode,
			onboardingAgentTemplate,
			isFirstAgent,
		))
	}
	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		h.Analytics.Capture(analytics.IssueCreated(
			userID,
			req.WorkspaceID,
			uuidToString(issue.ID),
			uuidToString(assistant.ID),
			"",
			"",
			analytics.SourceOnboarding,
		))
		if h.shouldEnqueueAgentTask(r.Context(), issue) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), issue)
		}
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		h.Analytics.Capture(analytics.OnboardingCompleted(
			userID,
			req.WorkspaceID,
			analytics.OnboardingPathFull,
			onboardedAt,
			updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		AgentID:     uuidToString(assistant.ID),
		IssueID:     uuidToString(issue.ID),
	})
}

// BootstrapOnboardingNoRuntime is the runtime-skipped onboarding exit:
// create or reuse one self-serve onboarding issue and mark onboarding
// complete. This keeps the no-runtime path focused on the single real
// blocker instead of seeding a project full of follow-up tasks.
func (h *Handler) BootstrapOnboardingNoRuntime(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, runtimeBootstrapBodyLimit)
	var req bootstrapOnboardingNoRuntimeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to start onboarding")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	userBefore, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !userBefore.OnboardedAt.Valid

	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	// The user explicitly skipped the runtime step, so seed the install-
	// runtime issue regardless of any pre-existing runtime on the workspace
	// — the user's intent was "I have nothing to connect right now".
	issue, issueCreated, err := seedInstallRuntimeIssue(
		r.Context(), qtx, wsUUID, parseUUID(userID), userBefore.Language,
	)
	if err != nil {
		slog.Warn("bootstrap no-runtime onboarding: seed install-runtime issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}

	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}
	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), updatedUser.StarterContentState); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to finish onboarding")
		return
	}

	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		h.Analytics.Capture(analytics.IssueCreated(
			userID,
			req.WorkspaceID,
			uuidToString(issue.ID),
			"",
			"",
			"",
			analytics.SourceOnboarding,
		))
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		h.Analytics.Capture(analytics.OnboardingCompleted(
			userID,
			req.WorkspaceID,
			analytics.OnboardingPathRuntimeSkipped,
			onboardedAt,
			updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingNoRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		IssueID:     uuidToString(issue.ID),
	})
}

// -----------------------------------------------------------------------------
// Starter content (post-onboarding opt-in)
// -----------------------------------------------------------------------------
//
// Users land in their workspace with starter_content_state=NULL and see
// a one-time dialog offering to seed example content. Two terminal
// transitions:
//
//   ImportStarterContent  NULL -> 'imported'  (also creates project, welcome
//                                              issue if agent-based, sub-issues,
//                                              pins — all in one transaction)
//   DismissStarterContent NULL -> 'dismissed'
//
// Content generation lives in TypeScript; the client POSTs the fully-rendered
// payload here, and the server gates on state and writes the content transactionally.

type importIssueSpec struct {
	Title        string `json:"title"`
	Description  string `json:"description"`
	Status       string `json:"status"`
	Priority     string `json:"priority"`
	AssignToSelf bool   `json:"assign_to_self"`
}

type welcomeIssueTemplate struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Priority    string `json:"priority"`
}

type importStarterContentRequest struct {
	WorkspaceID string `json:"workspace_id"`

	Project struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"project"`

	WelcomeIssueTemplate welcomeIssueTemplate `json:"welcome_issue_template"`
	AgentGuidedSubIssues []importIssueSpec    `json:"agent_guided_sub_issues"`
	SelfServeSubIssues   []importIssueSpec    `json:"self_serve_sub_issues"`
}

type importStarterContentResponse struct {
	User           UserResponse `json:"user"`
	ProjectID      string       `json:"project_id"`
	WelcomeIssueID *string      `json:"welcome_issue_id"`
}

func (h *Handler) ImportStarterContent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, importStarterContentBodyLimit)
	var req importStarterContentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.WorkspaceID == "" {
		writeError(w, http.StatusBadRequest, "workspace_id is required")
		return
	}
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)
	if req.Project.Title == "" {
		writeError(w, http.StatusBadRequest, "project.title is required")
		return
	}

	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	user, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user.StarterContentState.Valid {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "starter content already decided",
			"state": user.StarterContentState.String,
		})
		return
	}

	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}
	actorID := parseUUID(userID)

	agents, err := qtx.ListAgents(r.Context(), wsUUID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to list agents")
		return
	}
	hasAgent := len(agents) > 0
	var welcomeAgentID pgtype.UUID
	if hasAgent {
		welcomeAgentID = agents[0].ID
	}
	subSpecs := req.SelfServeSubIssues
	if hasAgent {
		subSpecs = req.AgentGuidedSubIssues
	}

	project, err := qtx.CreateProject(r.Context(), db.CreateProjectParams{
		WorkspaceID: wsUUID,
		Title:       req.Project.Title,
		Description: strOrNullText(req.Project.Description),
		Icon:        strOrNullText(req.Project.Icon),
		Status:      "planned",
		Priority:    "none",
	})
	if err != nil {
		slog.Warn("import starter content: create project failed", append(logger.RequestAttrs(r), "error", err)...)
		writeError(w, http.StatusInternalServerError, "failed to create project")
		return
	}

	var welcomeIssueID *string
	var welcomeIssueForEvent *db.Issue
	if hasAgent && req.WelcomeIssueTemplate.Title != "" {
		welcomeNumber, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		priority := req.WelcomeIssueTemplate.Priority
		if priority == "" {
			priority = "high"
		}
		welcome, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:  wsUUID,
			Title:        req.WelcomeIssueTemplate.Title,
			Description:  strOrNullText(req.WelcomeIssueTemplate.Description),
			Status:       "todo",
			Priority:     priority,
			AssigneeType: pgtype.Text{String: "agent", Valid: true},
			AssigneeID:   welcomeAgentID,
			CreatorType:  "member",
			CreatorID:    actorID,
			Number:       welcomeNumber,
		})
		if err != nil {
			slog.Warn("import starter content: create welcome issue failed", append(logger.RequestAttrs(r), "error", err)...)
			writeError(w, http.StatusInternalServerError, "failed to create welcome issue")
			return
		}
		id := uuidToString(welcome.ID)
		welcomeIssueID = &id
		copy := welcome
		welcomeIssueForEvent = &copy
	}

	subIssuesCreated := make([]db.Issue, 0, len(subSpecs))
	for _, sub := range subSpecs {
		if sub.Title == "" {
			continue
		}
		number, err := qtx.IncrementIssueCounter(r.Context(), wsUUID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to allocate issue number")
			return
		}
		var assigneeType pgtype.Text
		var assigneeID pgtype.UUID
		if sub.AssignToSelf {
			assigneeType = pgtype.Text{String: "member", Valid: true}
			assigneeID = actorID
		}
		status := sub.Status
		if status == "" {
			status = "backlog"
		}
		priority := sub.Priority
		if priority == "" {
			priority = "none"
		}
		issue, err := qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:  wsUUID,
			Title:        sub.Title,
			Description:  strOrNullText(sub.Description),
			Status:       status,
			Priority:     priority,
			AssigneeType: assigneeType,
			AssigneeID:   assigneeID,
			CreatorType:  "member",
			CreatorID:    actorID,
			Number:       number,
			ProjectID:    project.ID,
		})
		if err != nil {
			slog.Warn("import starter content: create sub-issue failed", append(logger.RequestAttrs(r), "error", err, "title", sub.Title)...)
			writeError(w, http.StatusInternalServerError, "failed to create sub-issues")
			return
		}
		subIssuesCreated = append(subIssuesCreated, issue)
	}

	pinnedProjectPos := float64(1)
	var pinProjectForEvent *db.PinnedItem
	pinProject, err := qtx.CreatePinnedItem(r.Context(), db.CreatePinnedItemParams{
		WorkspaceID: wsUUID,
		UserID:      parseUUID(userID),
		ItemType:    "project",
		ItemID:      project.ID,
		Position:    pinnedProjectPos,
	})
	if err != nil {
		slog.Warn("import starter content: pin project failed", append(logger.RequestAttrs(r), "error", err)...)
	} else {
		pinProjectForEvent = &pinProject
	}
	var pinWelcomeIssueForEvent *db.PinnedItem
	if welcomeIssueForEvent != nil {
		pinWelcome, err := qtx.CreatePinnedItem(r.Context(), db.CreatePinnedItemParams{
			WorkspaceID: wsUUID,
			UserID:      parseUUID(userID),
			ItemType:    "issue",
			ItemID:      welcomeIssueForEvent.ID,
			Position:    pinnedProjectPos + 1,
		})
		if err != nil {
			slog.Warn("import starter content: pin welcome issue failed", append(logger.RequestAttrs(r), "error", err)...)
		} else {
			pinWelcomeIssueForEvent = &pinWelcome
		}
	}

	updatedUser, err := qtx.SetStarterContentState(r.Context(), db.SetStarterContentStateParams{
		ID:                  parseUUID(userID),
		StarterContentState: pgtype.Text{String: "imported", Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record starter content state")
		return
	}

	if err := tx.Commit(r.Context()); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to commit starter content")
		return
	}

	projectResp := projectToResponse(project)
	h.publish(protocol.EventProjectCreated, req.WorkspaceID, "member", userID, map[string]any{"project": projectResp})

	workspacePrefix := h.getIssuePrefix(r.Context(), wsUUID)
	if welcomeIssueForEvent != nil {
		welcomeResp := issueToResponse(*welcomeIssueForEvent, workspacePrefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": welcomeResp})
		if h.shouldEnqueueAgentTask(r.Context(), *welcomeIssueForEvent) {
			h.TaskService.EnqueueTaskForIssue(r.Context(), *welcomeIssueForEvent)
		}
	}
	for _, sub := range subIssuesCreated {
		subResp := issueToResponse(sub, workspacePrefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": subResp})
	}
	if pinProjectForEvent != nil {
		h.publish(protocol.EventPinCreated, req.WorkspaceID, "member", userID, map[string]any{"pin": pinnedItemToResponse(*pinProjectForEvent)})
	}
	if pinWelcomeIssueForEvent != nil {
		h.publish(protocol.EventPinCreated, req.WorkspaceID, "member", userID, map[string]any{"pin": pinnedItemToResponse(*pinWelcomeIssueForEvent)})
	}

	starterBranch := analytics.StarterContentBranchSelfServe
	if hasAgent {
		starterBranch = analytics.StarterContentBranchAgentGuided
	}
	h.Analytics.Capture(analytics.StarterContentDecided(userID, req.WorkspaceID, "imported", starterBranch))

	writeJSON(w, http.StatusOK, importStarterContentResponse{
		User:           userToResponse(updatedUser),
		ProjectID:      uuidToString(project.ID),
		WelcomeIssueID: welcomeIssueID,
	})
}

type dismissStarterContentRequest struct {
	WorkspaceID string `json:"workspace_id,omitempty"`
}

func (h *Handler) DismissStarterContent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	var req dismissStarterContentRequest
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && err.Error() != "EOF" {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}

	user, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	if user.StarterContentState.Valid {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": "starter content already decided",
			"state": user.StarterContentState.String,
		})
		return
	}

	branch := analytics.StarterContentBranchSelfServe
	if req.WorkspaceID != "" {
		if wsUUID, err := util.ParseUUID(req.WorkspaceID); err == nil {
			req.WorkspaceID = uuidToString(wsUUID)
			if _, err := h.Queries.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
				UserID:      parseUUID(userID),
				WorkspaceID: wsUUID,
			}); err == nil {
				agents, err := h.Queries.ListAgents(r.Context(), wsUUID)
				if err == nil && len(agents) > 0 {
					branch = analytics.StarterContentBranchAgentGuided
				}
			}
		}
	}

	updated, err := h.Queries.SetStarterContentState(r.Context(), db.SetStarterContentStateParams{
		ID:                  parseUUID(userID),
		StarterContentState: pgtype.Text{String: "dismissed", Valid: true},
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to record dismiss")
		return
	}

	h.Analytics.Capture(analytics.StarterContentDecided(userID, req.WorkspaceID, "dismissed", branch))

	writeJSON(w, http.StatusOK, userToResponse(updated))
}

type patchOnboardingRequest struct {
	Questionnaire *json.RawMessage `json:"questionnaire,omitempty"`
}

// questionnaireAnswers mirrors the frontend's `QuestionnaireAnswers`
// shape. Only the first-time submission — every slot filled — is a
// funnel signal; partial saves are allowed but never emit.
type questionnaireAnswers struct {
	TeamSize      string `json:"team_size"`
	TeamSizeOther string `json:"team_size_other"`
	Role          string `json:"role"`
	RoleOther     string `json:"role_other"`
	UseCase       string `json:"use_case"`
	UseCaseOther  string `json:"use_case_other"`
}

func (q questionnaireAnswers) complete() bool {
	return q.TeamSize != "" && q.Role != "" && q.UseCase != ""
}

// PatchOnboarding persists the user's questionnaire answers. The
// field is optional; an omitted questionnaire is preserved. Which
// step the user is on is deliberately not persisted — every
// onboarding entry starts at Welcome.
//
// Emits `onboarding_questionnaire_submitted` exactly once per user:
// the first PATCH that transitions the answers from "at least one
// slot empty" to "all three filled". Revisions past that point don't
// re-emit — the funnel counts users, not edits.
func (h *Handler) PatchOnboarding(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	// Bound the body so the JSONB column can't be weaponized as bulk
	// storage — otherwise every subsequent `/api/me` read would have
	// to return the bloat.
	r.Body = http.MaxBytesReader(w, r.Body, patchOnboardingBodyLimit)
	var req patchOnboardingRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Read prior answers so we can detect the NULL/partial → complete
	// transition after the update. An errored decode on the prior row
	// is treated as "incomplete" — worst case we emit once more than
	// we should, never twice for the same transition.
	var before questionnaireAnswers
	if beforeUser, err := h.Queries.GetUser(r.Context(), parseUUID(userID)); err == nil {
		_ = json.Unmarshal(beforeUser.OnboardingQuestionnaire, &before)
	}

	params := db.PatchUserOnboardingParams{ID: parseUUID(userID)}
	if req.Questionnaire != nil {
		params.Questionnaire = []byte(*req.Questionnaire)
	}
	user, err := h.Queries.PatchUserOnboarding(r.Context(), params)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to update onboarding")
		return
	}

	var after questionnaireAnswers
	_ = json.Unmarshal(user.OnboardingQuestionnaire, &after)
	if after.complete() && !before.complete() {
		h.Analytics.Capture(analytics.OnboardingQuestionnaireSubmitted(
			userID,
			after.TeamSize,
			after.Role,
			after.UseCase,
			after.TeamSizeOther != "",
			after.RoleOther != "",
			after.UseCaseOther != "",
		))
	}

	writeJSON(w, http.StatusOK, userToResponse(user))
}

type joinCloudWaitlistRequest struct {
	Email  string `json:"email"`
	Reason string `json:"reason"`
}

// JoinCloudWaitlist records a user's interest in cloud runtimes.
// Pure side effect — does NOT complete onboarding. The user still
// has to pick a real Step 3 path (CLI with a detected runtime) or
// Skip to move on. Repeating the call overwrites email + reason.
func (h *Handler) JoinCloudWaitlist(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}
	var req joinCloudWaitlistRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// RFC 5321 caps email at 254 chars; the column is VARCHAR(254) and
	// the format check below rejects anything net/mail can't parse.
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if email == "" {
		writeError(w, http.StatusBadRequest, "email is required")
		return
	}
	if len(email) > 254 {
		writeError(w, http.StatusBadRequest, "email is too long")
		return
	}
	if _, err := mail.ParseAddress(email); err != nil {
		writeError(w, http.StatusBadRequest, "email is invalid")
		return
	}

	reason := strings.TrimSpace(req.Reason)
	if len(reason) > cloudWaitlistReasonMaxLen {
		writeError(w, http.StatusBadRequest, "reason is too long")
		return
	}

	reasonParam := pgtype.Text{}
	if reason != "" {
		reasonParam = pgtype.Text{String: reason, Valid: true}
	}

	user, err := h.Queries.JoinCloudWaitlist(r.Context(), db.JoinCloudWaitlistParams{
		ID:                  parseUUID(userID),
		CloudWaitlistEmail:  pgtype.Text{String: email, Valid: true},
		CloudWaitlistReason: reasonParam,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to join waitlist")
		return
	}

	h.Analytics.Capture(analytics.CloudWaitlistJoined(userID, reason != ""))

	writeJSON(w, http.StatusOK, userToResponse(user))
}

// strOrNullText converts an empty-meaning-absent string into a
// nullable pgtype.Text. Empty -> SQL NULL; non-empty -> Valid.
func strOrNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
