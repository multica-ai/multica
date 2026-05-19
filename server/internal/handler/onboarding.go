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
	noRuntimeIssueTitle     = "Connect a runtime to start using agents"
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

func noRuntimeIssueDescription(language pgtype.Text) string {
	if language.Valid && strings.HasPrefix(language.String, "zh") {
		return zhNoRuntimeIssueDescription()
	}
	return enNoRuntimeIssueDescription()
}

func enNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"Welcome to Multica.",
		"",
		"Agents need a runtime before they can execute work. You can still use Multica as a lightweight project-management workspace while you install one.",
		"",
		"## Try Multica first",
		"",
		"Before the runtime is ready, you can:",
		"",
		"1. Create a project for your current work.",
		"2. Create a few issues and move them across backlog, todo, in_progress, and done.",
		"3. Add priorities, labels, comments, and subscriptions.",
		"4. Use Inbox to track assignments and mentions.",
		"",
		"That gives you the project-management layer first. Once a runtime is connected, agents can start working from the same issues.",
		"",
		"## Install your first agent runtime",
		"",
		"Full guide: https://multica.ai/docs/install-agent-runtime",
		"",
		"For English users, the fastest first path is Codex:",
		"",
		"1. Make sure Node.js is installed.",
		"2. Install Codex:",
		"   npm i -g @openai/codex",
		"3. Sign in:",
		"   codex",
		"4. Confirm your terminal can find it:",
		"   which codex",
		"   codex --version",
		"5. Restart the Multica daemon:",
		"   multica daemon restart",
		"   If you use the desktop app, restarting the app is enough.",
		"6. Return to Runtimes and refresh. You should see a Codex runtime online.",
		"7. Create your first agent from that runtime, then assign an issue to the agent and set status to todo.",
		"",
		"Codex reference: https://developers.openai.com/codex/cli",
		"",
		"When the runtime is connected, you can create Multica Helper for a guided first run.",
	}, "\n")
}

func zhNoRuntimeIssueDescription() string {
	return strings.Join([]string{
		"欢迎来到 Multica。",
		"",
		"智能体需要先连上运行时才能执行工作。运行时还没准备好时，你也可以先把 Multica 当作轻量项目管理工具体验起来。",
		"",
		"## 先体验项目管理功能",
		"",
		"运行时安装前，你可以先做这些事：",
		"",
		"1. 为当前工作创建一个项目。",
		"2. 新建几个 issue，并在 backlog、todo、in_progress、done 之间流转。",
		"3. 给 issue 加优先级、标签、评论和订阅。",
		"4. 用收件箱追踪分配给你的事项和 @mention。",
		"",
		"这样你先熟悉项目管理层。连上运行时后，智能体会直接在这些 issue 上开始工作。",
		"",
		"## 安装第一个 Agent 运行时",
		"",
		"完整文档：https://multica.ai/docs/install-agent-runtime",
		"",
		"中文用户建议先装 Kimi CLI：",
		"",
		"1. 在 macOS / Linux 终端安装 Kimi CLI：",
		"   curl -LsSf https://code.kimi.com/install.sh | bash",
		"   Windows PowerShell：",
		"   Invoke-RestMethod https://code.kimi.com/install.ps1 | Invoke-Expression",
		"2. 确认终端能找到 Kimi：",
		"   kimi --version",
		"3. 在你想让 Kimi 工作的项目目录里启动一次：",
		"   kimi",
		"4. 首次启动后输入 /login，按提示完成 Kimi Code 或 API key 配置。",
		"5. 重启 Multica 守护进程：",
		"   multica daemon restart",
		"   如果你用桌面端，重启 app 即可。",
		"6. 回到 Runtimes 页面刷新。你应该能看到一个在线的 Kimi 运行时。",
		"7. 用这个运行时创建第一个智能体，再把一个 issue 分配给它，并把状态切到 todo。",
		"",
		"Kimi CLI 官方文档：https://moonshotai.github.io/kimi-cli/zh/guides/getting-started.html",
		"",
		"运行时连上后，你就可以创建 Multica Helper，开始一次有智能体参与的上手引导。",
	}, "\n")
}

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

	// Read the prior state so we can detect "was this call the one that
	// actually completed onboarding?" — MarkUserOnboarded uses COALESCE
	// and returns the preserved timestamp on repeat calls, which is not
	// the signal we need for the funnel.
	before, err := h.Queries.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid

	user, err := h.Queries.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
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
// issue assigned to it, mark onboarding complete, and suppress the older
// starter-content dialog. The flow is deliberately one issue, not a seeded
// project with many tasks.
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
	starterContentClaimed := false
	if !updatedUser.StarterContentState.Valid {
		updatedUser, err = qtx.SetStarterContentState(r.Context(), db.SetStarterContentStateParams{
			ID:                  parseUUID(userID),
			StarterContentState: pgtype.Text{String: "imported", Valid: true},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record starter content state")
			return
		}
		starterContentClaimed = true
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
	if starterContentClaimed {
		h.Analytics.Capture(analytics.StarterContentDecided(
			userID,
			req.WorkspaceID,
			"imported",
			analytics.StarterContentBranchAgentGuided,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		AgentID:     uuidToString(assistant.ID),
		IssueID:     uuidToString(issue.ID),
	})
}

// BootstrapOnboardingNoRuntime is the runtime-skipped onboarding exit:
// create or reuse one self-serve onboarding issue, mark onboarding complete,
// and suppress the older starter-content dialog. This keeps the no-runtime
// path focused on the single real blocker instead of seeding a project full
// of follow-up tasks.
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

	var emptyUUID pgtype.UUID
	issue, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(),
		qtx,
		wsUUID,
		emptyUUID,
		emptyUUID,
		noRuntimeIssueTitle,
		false,
	)
	if err != nil {
		slog.Warn("bootstrap no-runtime onboarding: duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
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
			Title:         noRuntimeIssueTitle,
			Description:   strOrNullText(noRuntimeIssueDescription(userBefore.Language)),
			Status:        "todo",
			Priority:      "high",
			AssigneeType:  pgtype.Text{String: "member", Valid: true},
			AssigneeID:    parseUUID(userID),
			CreatorType:   "member",
			CreatorID:     parseUUID(userID),
			ParentIssueID: emptyUUID,
			Position:      0,
			Number:        issueNumber,
			ProjectID:     emptyUUID,
		})
		if err != nil {
			slog.Warn("bootstrap no-runtime onboarding: create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
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
	starterContentClaimed := false
	if !updatedUser.StarterContentState.Valid {
		updatedUser, err = qtx.SetStarterContentState(r.Context(), db.SetStarterContentStateParams{
			ID:                  parseUUID(userID),
			StarterContentState: pgtype.Text{String: "imported", Valid: true},
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, "failed to record starter content state")
			return
		}
		starterContentClaimed = true
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
	if starterContentClaimed {
		h.Analytics.Capture(analytics.StarterContentDecided(
			userID,
			req.WorkspaceID,
			"imported",
			analytics.StarterContentBranchSelfServe,
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
// Why state-first, then seeding inside the same transaction:
//   - starter_content_state is the "have we asked / done this" bit, so it
//     must be set exactly once per user
//   - if we set state AFTER creation, a mid-request crash leaves duplicates
//     on retry (the original "Not idempotent" bug)
//   - if we set state BEFORE creation, a mid-request crash leaves the user
//     with 'imported' + no content
//   - inside a transaction, both commit together or neither does — and the
//     starting state check (must be NULL) guarantees the claim is atomic
//
// Content generation lives in TypeScript (the markdown templates are large
// and depend on the Q1–Q3 answers); the client POSTs the fully-rendered
// payload here, and the server's job is to (1) gate on state, (2) do the
// batch insert transactionally, (3) record the transition.

type importIssueSpec struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Priority    string `json:"priority"`
	// AssignToSelf: true for sub-issues (assigned to the current
	// user as a member). Server uses `user_id` per the app-wide
	// convention in AssigneePicker / resolveActor.
	AssignToSelf bool `json:"assign_to_self"`
}

// welcomeIssueTemplate is a PRE-rendered welcome issue — title +
// description + priority. There is no `agent_id` field on purpose:
// the server picks the target agent itself from ListAgents inside
// the transaction, so a stale or compromised client can't assign
// the welcome issue to an arbitrary agent.
type welcomeIssueTemplate struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	// Priority optional; defaults to "high" when empty.
	Priority string `json:"priority"`
}

type importStarterContentRequest struct {
	WorkspaceID string `json:"workspace_id"`

	Project struct {
		Title       string `json:"title"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
	} `json:"project"`

	// Welcome issue template — rendered regardless of branch. The
	// server creates it only when at least one agent exists in the
	// workspace; otherwise it's ignored.
	WelcomeIssueTemplate welcomeIssueTemplate `json:"welcome_issue_template"`

	// Both branches of sub-issues. The server picks which array to
	// seed based on whether the workspace has any agents at the
	// moment of the call — the client no longer decides. Sending
	// both is ~15 KB extra payload, which stays well under the
	// 64 KB MaxBytesReader cap above.
	AgentGuidedSubIssues []importIssueSpec `json:"agent_guided_sub_issues"`
	SelfServeSubIssues   []importIssueSpec `json:"self_serve_sub_issues"`
}

type importStarterContentResponse struct {
	User           UserResponse `json:"user"`
	ProjectID      string       `json:"project_id"`
	WelcomeIssueID *string      `json:"welcome_issue_id"`
}

// ImportStarterContent creates the Getting Started project, optional
// welcome issue, sub-issues, and pins — all inside a single transaction
// gated by the atomic NULL -> 'imported' state transition. Idempotent
// at the state level: any second call returns 409 with the already-set
// state, no duplicate content created.
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
	// Reject malformed UUIDs up front and reuse the parsed value for every
	// write below so a garbage workspace_id never reaches CreateProject /
	// CreateIssue.
	wsUUID, ok := parseUUIDOrBadRequest(w, req.WorkspaceID, "workspace_id")
	if !ok {
		return
	}
	req.WorkspaceID = uuidToString(wsUUID)
	if req.Project.Title == "" {
		writeError(w, http.StatusBadRequest, "project.title is required")
		return
	}

	// Start the transaction early — the state claim lives inside it so
	// concurrent imports from another tab can't both pass the check.
	tx, err := h.TxStarter.Begin(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to begin transaction")
		return
	}
	defer tx.Rollback(r.Context())
	qtx := h.Queries.WithTx(tx)

	// Claim step: user must be NULL (never asked) to proceed. A value
	// of 'imported' / 'dismissed' / 'skipped_legacy' all short-circuit
	// with 409 Conflict — the caller should close the dialog and
	// refresh the user to pick up the already-final state.
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

	// Membership check: user must belong to the target workspace.
	// `actorID` below is `parseUUID(userID)` — stored as `creator_id`
	// and `assignee_id` for `type="member"` to match the app-wide
	// convention (AssigneePicker + resolveActor). Storing `member.id`
	// would cause `useActorName.getMemberName` to resolve to "Unknown"
	// since members are looked up by `user_id`.
	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}
	actorID := parseUUID(userID)

	// --- Branch decision (server-authoritative) ---
	// Ask the DB — not the client — whether there's an agent in this
	// workspace. `ListAgents` orders by created_at ASC, so "agents[0]"
	// is deterministically the earliest-created agent. This replaces
	// the old client-supplied `welcome_issue.agent_id` trust chain.
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

	// --- Create project ---
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

	// --- Create welcome issue (only when an agent exists) ---
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

	// --- Create sub-issues (branch picked above) ---
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

	// --- Pin project (and welcome issue if present) ---
	// Non-fatal: a pin failure shouldn't prevent the onboarding bundle
	// from landing. We warn and move on. Pointers to the created rows
	// are kept around for post-commit `pin:created` fan-out so the
	// sidebar refreshes without a manual reload.
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

	// --- Flip state ---
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

	// --- Post-commit: realtime events + agent task enqueue ---
	// Realtime fan-out happens here (not inside the tx) because the DB
	// commit must land first — otherwise subscribers could receive an
	// event for state that's about to be rolled back.
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
	// Pin events. Without these, the sidebar's `pinListOptions` query
	// stays cached on the pre-import snapshot — only a hard refresh
	// surfaces the new pins. Same payload shape as `POST /pins`.
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
	// WorkspaceID is optional but strongly preferred — when present the
	// server derives the starter branch (agent_guided / self_serve) by
	// looking at the workspace's current agent list, so analytics can
	// split dismiss rate by branch the same way import is split.
	// Without it, branch defaults to self_serve (the zero-agent case).
	WorkspaceID string `json:"workspace_id,omitempty"`
}

// DismissStarterContent records the user's decision to skip starter
// content. Like Import, this is a NULL -> terminal transition; a
// second call returns 409 with the current state.
//
// Emits `starter_content_decided` with `decision=dismissed`. The
// `branch` property mirrors what ImportStarterContent would have
// written for the same workspace, so the two-sided funnel (import vs
// dismiss by branch) stays directly comparable.
func (h *Handler) DismissStarterContent(w http.ResponseWriter, r *http.Request) {
	userID, ok := requireUserID(w, r)
	if !ok {
		return
	}

	// Body is optional for backward-compat with callers that pre-date
	// the workspace-id addition. An empty body is a legal dismiss.
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

	// Resolve branch before the update so the analytics event mirrors
	// the import-side logic exactly. An unresolvable workspace (malformed
	// UUID, user not a member, or empty body) falls back to self_serve —
	// the conservative default that matches what Import would emit when
	// ListAgents returns empty.
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

// strOrNullText converts an empty-meaning-absent string into a
// nullable pgtype.Text. Empty -> SQL NULL; non-empty -> Valid.
func strOrNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}
