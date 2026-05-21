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

type patchOnboardingRequest struct {
	Questionnaire *json.RawMessage `json:"questionnaire,omitempty"`
}

// questionnaireAnswers mirrors the frontend's v2 `QuestionnaireAnswers`
// shape. Each of source / role / use_case has a value, an optional
// free-text "other" override, and a skip marker. The questionnaire is
// "resolved" once every slot has either an answer or a skip marker;
// the funnel event fires on the transition into that state.
type questionnaireAnswers struct {
	Source         string `json:"source"`
	SourceOther    string `json:"source_other"`
	SourceSkipped  bool   `json:"source_skipped"`
	Role           string `json:"role"`
	RoleOther      string `json:"role_other"`
	RoleSkipped    bool   `json:"role_skipped"`
	UseCase        string `json:"use_case"`
	UseCaseOther   string `json:"use_case_other"`
	UseCaseSkipped bool   `json:"use_case_skipped"`
	Version        int    `json:"version"`
}

func (q questionnaireAnswers) sourceResolved() bool {
	return q.Source != "" || q.SourceSkipped
}
func (q questionnaireAnswers) roleResolved() bool {
	return q.Role != "" || q.RoleSkipped
}
func (q questionnaireAnswers) useCaseResolved() bool {
	return q.UseCase != "" || q.UseCaseSkipped
}

// questionnaireSchemaVersion is the schema this handler understands.
// `complete()` and the funnel event are scoped to this version so a
// future v3 row can't be silently mis-counted against v2 semantics.
const questionnaireSchemaVersion = 2

func (q questionnaireAnswers) complete() bool {
	if q.Version != questionnaireSchemaVersion {
		return false
	}
	return q.sourceResolved() && q.roleResolved() && q.useCaseResolved()
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
			after.Source,
			after.Role,
			after.UseCase,
			after.SourceSkipped,
			after.RoleSkipped,
			after.UseCaseSkipped,
			after.SourceOther != "",
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
