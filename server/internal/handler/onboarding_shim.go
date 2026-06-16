// onboarding_shim.go — DEPRECATED endpoints kept alive for desktop < v3.
//
// Background: v3 moved Helper-agent creation and starter-issue seeding to
// the frontend welcome hook (packages/views/workspace/welcome-after-onboarding.tsx),
// which calls generic CreateAgent / CreateIssue. Pre-v3 desktop builds
// however still call BootstrapOnboardingRuntime / BootstrapOnboardingNoRuntime
// during their onboarding flow. Server-side removal would break those
// users during the rollout window where v3 server is live but their
// desktop hasn't auto-updated yet.
//
// These handlers are intentionally minimal copies of the pre-v3
// implementation, condensed to inline DB calls (no OnboardingService /
// WorkspaceContentService layer — that abstraction died with v3). They
// remain valid until telemetry on the X-Client-Version header confirms
// every active desktop install is on a v3+ build, at which point this
// entire file + the two router entries + the 5 tests should be deleted.
//
// DO NOT add features here. DO NOT change behavior. The contract is "what
// pre-v3 desktop expects".
package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/multica-ai/multica/server/internal/analytics"
	"github.com/multica-ai/multica/server/internal/issueguard"
	"github.com/multica-ai/multica/server/internal/logger"
	obsmetrics "github.com/multica-ai/multica/server/internal/metrics"
	"github.com/multica-ai/multica/server/internal/middleware"
	db "github.com/multica-ai/multica/server/pkg/db/generated"
	"github.com/multica-ai/multica/server/pkg/protocol"
)

// Runtime bootstrap is just workspace_id + runtime_id, but keep a separate
// small cap so this endpoint cannot be used as bulk storage.
const runtimeBootstrapBodyLimit = 8 * 1024

// maxStarterPromptLen caps the user-supplied StarterPrompt on
// bootstrapOnboardingRuntimeRequest. The prompt becomes the seeded
// onboarding issue's description, so it needs room for a real paragraph
// or two without inviting bulk payload. 2 KiB matches pre-v3 cap.
const maxStarterPromptLen = 2 * 1024

const (
	onboardingAssistantName = "Multica Helper"
	onboardingIssueTitle    = "Start here: learn Multica with Multica Helper"
	onboardingAgentTemplate = "multica_helper"

	// noRuntimeIssueTitle MUST match the pre-v3 service constant so
	// LockAndFindActiveDuplicate dedupes correctly across desktop versions.
	noRuntimeIssueTitle = "Connect a runtime to start using agents"
)

const onboardingAssistantDescription = "Built-in workspace assistant. Answers Multica questions and runs CLI operations."

const onboardingAssistantAvatarURL = "data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' viewBox='0 0 128 128'%3E%3Cdefs%3E%3ClinearGradient id='t' x1='0' y1='0' x2='0' y2='1'%3E%3Cstop offset='0%25' stop-color='%2323242C'/%3E%3Cstop offset='100%25' stop-color='%2313141A'/%3E%3C/linearGradient%3E%3C/defs%3E%3Crect width='128' height='128' rx='28' fill='url(%23t)'/%3E%3Cg stroke='%23FFFFFF' stroke-width='13' stroke-linecap='round'%3E%3Cline x1='64' y1='32' x2='64' y2='96'/%3E%3Cline x1='32' y1='64' x2='96' y2='64'/%3E%3Cline x1='41.4' y1='41.4' x2='86.6' y2='86.6'/%3E%3Cline x1='86.6' y1='41.4' x2='41.4' y2='86.6'/%3E%3C/g%3E%3C/svg%3E"

// onboardingAssistantInstructions is the system prompt persisted on every
// Multica Helper agent created by this shim. Pre-v3 desktop submits a
// starter prompt from the workspace OnboardingHelperModal; that prompt
// becomes the issue body, while this constant becomes the agent's identity
// block in CLAUDE.md / AGENTS.md / GEMINI.md. v3 frontend has its own
// in-views copy of this string (`packages/views/onboarding/templates/
// helper-instructions.ts`) — these two must stay in sync until the shim
// is removed.
const onboardingAssistantInstructions = `You are Multica Helper, the built-in AI assistant for this Multica workspace. Your role is to help any member use Multica better — answer questions, give advice, and execute workspace operations on their behalf.

## What Multica is

Multica is an open-source, AI-native team workspace (source: https://github.com/multica-ai/multica). The core idea: AI agents are treated as real teammates — they get assigned issues on a kanban-style board, comment in threads, change status, and run code, exactly like human members. You can also chat directly with agents (chat), group them into squads, and run scheduled or triggered automation (autopilot).

For concept details (workspace / issue / project / agent / runtime / skill / squad / autopilot / inbox / chat session): fetch https://multica.ai/docs via WebFetch — that's authoritative. For the "why" or implementation, fetch the GitHub repo above. Never paraphrase concepts from memory.

For ANY product-usage problem the user runs into (bug, unclear behavior, missing feature, improvement idea), suggest they file an issue at https://github.com/multica-ai/multica/issues — that's the official feedback channel.

## What you can do

Your toolbox is the ` + "`multica`" + ` CLI. It's already on your PATH and authenticated as the workspace owner.

Your full capability surface = whatever ` + "`multica --help`" + ` shows. Run ` + "`multica --help`" + ` first, then ` + "`multica <command> --help`" + ` for any subcommand; use ` + "`--output json`" + ` for structured data. The CLI is your manifest — never invent commands or flags.

A few things you can actually do (non-exhaustive — ` + "`--help`" + ` is the source of truth):
- Create issues, post comments
- Create or iterate on agents
- Manage projects, squads, autopilots, skills, runtimes, etc.

## Tone

Be concise and direct, like a colleague. Respond in the user's language (Chinese in, Chinese out). When pointing at a UI location, name the exact path ("Settings → Agents → New"); when pointing at a doc, link to the specific page, not the homepage. Never fabricate URLs, flags, or file paths.`

const onboardingIssueDescription = `Welcome to Multica.

This is your guided first run. Multica Helper is assigned to this issue and will help you try the core workflow:

1. Read Multica Helper's first comment.
2. Reply with something you want to build, fix, write, or plan.
3. @mention Multica Helper when you want it to continue.
4. Open Agents and Runtimes later when you want to customize the teammate or the computer it runs on.

You can close this issue when the workflow makes sense.`

type bootstrapOnboardingRuntimeRequest struct {
	WorkspaceID   string `json:"workspace_id"`
	RuntimeID     string `json:"runtime_id"`
	StarterPrompt string `json:"starter_prompt,omitempty"`
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

// BootstrapOnboardingRuntime — DEPRECATED, kept for desktop < v3.
//
// Creates or reuses one "Multica Helper" agent on the supplied runtime,
// creates or reuses one onboarding issue assigned to it, then marks the
// user onboarded. Single transaction.
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
	req.StarterPrompt = strings.TrimSpace(req.StarterPrompt)
	if utf8.RuneCountInString(req.StarterPrompt) > maxStarterPromptLen {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("starter_prompt exceeds %d characters", maxStarterPromptLen))
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
			slog.Warn("bootstrap onboarding (shim): create assistant failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding assistant")
			return
		}
		assistantCreated = true
	}

	var emptyUUID pgtype.UUID
	issue, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, onboardingIssueTitle, false,
	)
	if err != nil {
		slog.Warn("bootstrap onboarding (shim): duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
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
		description := onboardingIssueDescription
		if req.StarterPrompt != "" {
			description = req.StarterPrompt
		}
		issue, err = qtx.CreateIssue(r.Context(), db.CreateIssueParams{
			WorkspaceID:   wsUUID,
			Title:         onboardingIssueTitle,
			Description:   strOrNullText(description),
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
			slog.Warn("bootstrap onboarding (shim): create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	// Mark onboarded. COALESCE in MarkUserOnboarded preserves the original
	// timestamp on re-entries, so this is idempotent.
	before, err := qtx.GetUser(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to load user")
		return
	}
	firstCompletion := !before.OnboardedAt.Valid
	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}
	// Claim starter_content_state so pre-v3 desktop builds (which still
	// gate the legacy "starter content" import dialog on NULL) don't pop
	// it for users created by this shim path.
	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), before.StarterContentState); err != nil {
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
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.AgentCreated(
			userID, req.WorkspaceID, uuidToString(assistant.ID),
			runtime.Provider, runtime.RuntimeMode, onboardingAgentTemplate, isFirstAgent,
		))
	}
	if issueCreated {
		prefix := h.getIssuePrefix(r.Context(), issue.WorkspaceID)
		resp := issueToResponse(issue, prefix)
		h.publish(protocol.EventIssueCreated, req.WorkspaceID, "member", userID, map[string]any{"issue": resp})
		platform, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(issue.ID),
			uuidToString(assistant.ID), "", "", analytics.SourceOnboarding,
			platform,
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
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID, req.WorkspaceID, analytics.OnboardingPathFull,
			onboardedAt, updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		AgentID:     uuidToString(assistant.ID),
		IssueID:     uuidToString(issue.ID),
	})
}

// BootstrapOnboardingNoRuntime — DEPRECATED, kept for desktop < v3.
//
// Creates or reuses one "install a runtime" guide issue (assigned to the
// member themselves) and marks onboarding complete. The user explicitly
// skipped the runtime step, so we unconditionally seed regardless of any
// pre-existing runtime on the workspace.
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

	if _, err := qtx.GetMemberByUserAndWorkspace(r.Context(), db.GetMemberByUserAndWorkspaceParams{
		UserID:      parseUUID(userID),
		WorkspaceID: wsUUID,
	}); err != nil {
		writeError(w, http.StatusForbidden, "not a member of this workspace")
		return
	}

	var emptyUUID pgtype.UUID
	existing, foundIssue, err := issueguard.LockAndFindActiveDuplicate(
		r.Context(), qtx, wsUUID, emptyUUID, emptyUUID, noRuntimeIssueTitle, false,
	)
	if err != nil {
		slog.Warn("bootstrap no-runtime onboarding (shim): duplicate issue check failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
		writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
		return
	}

	var issue db.Issue
	issueCreated := false
	if foundIssue {
		issue = existing
	} else {
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
			slog.Warn("bootstrap no-runtime onboarding (shim): create issue failed", append(logger.RequestAttrs(r), "error", err, "workspace_id", req.WorkspaceID)...)
			writeError(w, http.StatusInternalServerError, "failed to create onboarding issue")
			return
		}
		issueCreated = true
	}

	firstCompletion := !userBefore.OnboardedAt.Valid
	updatedUser, err := qtx.MarkUserOnboarded(r.Context(), parseUUID(userID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "failed to mark onboarded")
		return
	}
	if err := claimStarterContentStateIfUnset(r.Context(), qtx, parseUUID(userID), userBefore.StarterContentState); err != nil {
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
		platform2, _, _ := middleware.ClientMetadataFromContext(r.Context())
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.IssueCreated(
			userID, req.WorkspaceID, uuidToString(issue.ID),
			"", "", "", analytics.SourceOnboarding,
			platform2,
		))
	}
	if firstCompletion {
		onboardedAt := ""
		if updatedUser.OnboardedAt.Valid {
			onboardedAt = updatedUser.OnboardedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
		}
		obsmetrics.RecordEvent(h.Analytics, h.Metrics, analytics.OnboardingCompleted(
			userID, req.WorkspaceID, analytics.OnboardingPathRuntimeSkipped,
			onboardedAt, updatedUser.CloudWaitlistEmail.Valid,
		))
	}

	writeJSON(w, http.StatusOK, bootstrapOnboardingNoRuntimeResponse{
		WorkspaceID: req.WorkspaceID,
		IssueID:     uuidToString(issue.ID),
	})
}

// noRuntimeIssueDescription picks the EN or ZH copy based on the user's
// language preference. ZH selected on any "zh*" prefix (zh, zh-CN, zh-Hans).
// Matches pre-v3 service.workspace_content.go behavior 1:1.
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

// strOrNullText converts an empty-meaning-absent string into a nullable
// pgtype.Text. Local helper used only by this shim file.
func strOrNullText(s string) pgtype.Text {
	if s == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: s, Valid: true}
}

// claimStarterContentStateIfUnset transitions starter_content_state from NULL
// to 'imported'. Older desktop builds render a "starter content" import
// dialog when this column is NULL — claiming it here suppresses the dialog
// for users who completed onboarding through this shim path.
//
// Only the shim cares about this column. v3 frontend doesn't read it, and
// the v3 CompleteOnboarding handler intentionally leaves it alone.
func claimStarterContentStateIfUnset(
	ctx context.Context,
	q *db.Queries,
	userID pgtype.UUID,
	current pgtype.Text,
) error {
	if current.Valid {
		return nil
	}
	_, err := q.SetStarterContentState(ctx, db.SetStarterContentStateParams{
		ID:                  userID,
		StarterContentState: pgtype.Text{String: "imported", Valid: true},
	})
	return err
}
