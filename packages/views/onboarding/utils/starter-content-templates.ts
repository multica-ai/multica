import type { InterpolationParams } from "@multica/i18n";
import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import type {
  ImportStarterContentPayload,
  ImportStarterIssuePayload,
} from "@multica/core/api";

// =============================================================================
// Starter content templates.
//
// Pure functions that turn the user's questionnaire answers into the request
// payload for POST /api/me/starter-content/import. No side effects, no API
// calls, no DOM — the only consumer is `StarterContentPrompt`, which passes
// the output straight to the server.
//
// Separation of concerns:
//   - Markdown/copy lives in the i18n dictionary (TypeScript, reviewed as UI)
//   - Batch creation + idempotency + assignee resolution lives in Go
//     (handler/onboarding.go → ImportStarterContent)
// =============================================================================

type TFn = (key: string, params?: InterpolationParams) => string;

interface StarterContentContext {
  docsUrl: string;
}

function templateParams(
  ctx: StarterContentContext,
  params?: InterpolationParams,
): InterpolationParams {
  return params ? { docsUrl: ctx.docsUrl, ...params } : { docsUrl: ctx.docsUrl };
}

function starterT(
  t: TFn,
  ctx: StarterContentContext,
  key: string,
  params?: InterpolationParams,
): string {
  return t(key, templateParams(ctx, params));
}

interface WelcomeIssueText {
  title: string;
  description: string;
}

// Prefix titles with 1. 2. 3. … AFTER the full list is assembled so
// conditional items (invite team / connect repo) don't break numbering.
function numberTitles(
  issues: ImportStarterIssuePayload[],
): ImportStarterIssuePayload[] {
  return issues.map((s, i) => ({ ...s, title: `${i + 1}. ${s.title}` }));
}

export function buildWelcomeIssueText(
  q: QuestionnaireAnswers,
  userName: string,
  t: TFn,
  ctx: StarterContentContext,
): WelcomeIssueText {
  const name = userName.trim() || t("sc_name_fallback");

  const header = starterT(t, ctx, "sc_welcome_header");
  const sharedInstructions = t("sc_shared_instructions", { name });
  const exploreInstructions = t("sc_explore_instructions", { name });

  switch (q.use_case) {
    case "coding":
      return {
        title: t("sc_welcome_title"),
        description: `${header}${t("sc_welcome_coding_prefix", { name })}\n\n${sharedInstructions}`,
      };
    case "planning":
      return {
        title: t("sc_welcome_title"),
        description: `${header}${t("sc_welcome_planning_prefix", { name })}\n\n${sharedInstructions}`,
      };
    case "writing_research":
      return {
        title: t("sc_welcome_title"),
        description: `${header}${t("sc_welcome_writing_prefix", { name })}\n\n${sharedInstructions}`,
      };
    case "explore":
      return {
        title: t("sc_welcome_title"),
        description: `${header}${t("sc_welcome_explore_prefix", { name })}\n\n${exploreInstructions}`,
      };
    case "other": {
      const customUseCase = (q.use_case_other ?? "").trim();
      const contextLine = customUseCase
        ? t("sc_welcome_other_context", { useCase: customUseCase })
        : t("sc_welcome_no_context");
      return {
        title: t("sc_welcome_title"),
        description: `${header}${t("sc_welcome_other_prefix", { name, contextLine })}\n\n${sharedInstructions}`,
      };
    }
    default:
      return {
        title: t("sc_welcome_title"),
        description: `${header}${t("sc_welcome_default_prefix", { name })}\n\n${sharedInstructions}`,
      };
  }
}

export function buildAgentGuidedSubIssues(
  q: QuestionnaireAnswers,
  t: TFn,
  ctx: StarterContentContext,
): ImportStarterIssuePayload[] {
  // --- Tier 1: Core must-learn (Todo / urgent) ------------------------------
  const tier1: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: t("sc_g_trigger_title"),
      description: starterT(t, ctx, "sc_g_trigger_desc"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: t("sc_g_chat_title"),
      description: starterT(t, ctx, "sc_g_chat_desc"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: t("sc_g_context_title"),
      description: starterT(t, ctx, "sc_g_context_desc"),
    },
  ];

  // --- Tier 2: Setup (Todo / medium) ----------------------------------------
  const tier2: ImportStarterIssuePayload[] = [];

  if (q.team_size === "team") {
    tier2.push({
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: t("sc_g_invite_title"),
      description: starterT(t, ctx, "sc_g_invite_desc"),
    });
  }

  if (q.role === "developer" || q.use_case === "coding") {
    tier2.push({
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: t("sc_g_repo_title"),
      description: starterT(t, ctx, "sc_g_repo_desc"),
    });
  }

  tier2.push({
    status: "todo",
    priority: "medium",
    assign_to_self: true,
    title: t("sc_g_second_agent_title"),
    description: starterT(t, ctx, "sc_g_second_agent_desc"),
  });

  // --- Tier 3: Advanced, discover later (Backlog) ---------------------------
  const tier3: ImportStarterIssuePayload[] = [
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_g_polish_title"),
      description: starterT(t, ctx, "sc_g_polish_desc"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_g_watch_title"),
      description: starterT(t, ctx, "sc_g_watch_desc"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_g_inbox_title"),
      description: starterT(t, ctx, "sc_g_inbox_desc"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_g_autopilot_title"),
      description: starterT(t, ctx, "sc_g_autopilot_desc"),
    },
  ];

  return numberTitles([...tier1, ...tier2, ...tier3]);
}

export function buildSelfServeSubIssues(
  q: QuestionnaireAnswers,
  t: TFn,
  ctx: StarterContentContext,
): ImportStarterIssuePayload[] {
  // --- Tier 1: Unlock agent ability (Todo / high) ---------------------------
  // Without a runtime + an agent, nothing else in Multica works. These two
  // are the gates — everything below them waits on them.
  const tier1: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: t("sc_s_install_title"),
      description: starterT(t, ctx, "sc_s_install_desc"),
    },
    {
      status: "todo",
      priority: "high",
      assign_to_self: true,
      title: t("sc_s_create_agent_title"),
      description: starterT(t, ctx, "sc_s_create_agent_desc"),
    },
  ];

  // --- Tier 2: Core usage after unlock (Todo / medium) ----------------------
  const tier2: ImportStarterIssuePayload[] = [
    {
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: t("sc_s_first_task_title"),
      description: starterT(t, ctx, "sc_s_first_task_desc"),
    },
    {
      status: "todo",
      priority: "medium",
      assign_to_self: true,
      title: t("sc_s_context_title"),
      description: starterT(t, ctx, "sc_s_context_desc"),
    },
  ];

  // --- Tier 3: Advanced, discover later (Backlog) ---------------------------
  const tier3: ImportStarterIssuePayload[] = [
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_s_chat_title"),
      description: starterT(t, ctx, "sc_s_chat_desc"),
    },
  ];

  if (q.role === "developer" || q.use_case === "coding") {
    tier3.push({
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_s_repo_title"),
      description: starterT(t, ctx, "sc_s_repo_desc"),
    });
  }

  if (q.team_size === "team") {
    tier3.push({
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_s_invite_title"),
      description: starterT(t, ctx, "sc_s_invite_desc"),
    });
  }

  tier3.push(
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_s_instructions_title"),
      description: starterT(t, ctx, "sc_s_instructions_desc"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_s_watch_title"),
      description: starterT(t, ctx, "sc_s_watch_desc"),
    },
    {
      status: "backlog",
      priority: "low",
      assign_to_self: true,
      title: t("sc_s_autopilot_title"),
      description: starterT(t, ctx, "sc_s_autopilot_desc"),
    },
  );

  return numberTitles([...tier1, ...tier2, ...tier3]);
}

/**
 * Builds the full import payload. The client does NOT decide between the
 * agent-guided and self-serve branches — it always sends both sub-issue
 * arrays and a welcome-issue template (no agent_id). The SERVER picks
 * inside the import transaction based on whether any agent exists in
 * the workspace at that moment. See handler/onboarding.go.
 */
export function buildImportPayload({
  workspaceId,
  userName,
  questionnaire,
  t,
  docsUrl,
}: {
  workspaceId: string;
  userName: string;
  questionnaire: QuestionnaireAnswers;
  t: TFn;
  docsUrl: string;
}): ImportStarterContentPayload {
  const ctx = { docsUrl };
  const welcome = buildWelcomeIssueText(questionnaire, userName, t, ctx);
  return {
    workspace_id: workspaceId,
    project: {
      title: t("sc_project_title"),
      description: t("sc_project_desc"),
      icon: "👋",
    },
    welcome_issue_template: {
      title: welcome.title,
      description: welcome.description,
      priority: "high",
    },
    agent_guided_sub_issues: buildAgentGuidedSubIssues(questionnaire, t, ctx),
    self_serve_sub_issues: buildSelfServeSubIssues(questionnaire, t, ctx),
  };
}
