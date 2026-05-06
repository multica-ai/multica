import type { QuestionnaireAnswers } from "@multica/core/onboarding";
import type {
  ImportStarterContentPayload,
  ImportStarterIssuePayload,
} from "@multica/core/api";
import * as en from "./starter-content-content-en";
import * as zh from "./starter-content-content-zh";

// =============================================================================
// Starter content orchestrator.
//
// Pure functions that turn the user's questionnaire answers + locale into
// the request payload for POST /api/me/starter-content/import. No side
// effects, no API calls, no DOM — the only consumer is `StarterContentPrompt`,
// which passes the output straight to the server.
//
// Long-form markdown bodies live in sibling files keyed by locale:
//   - starter-content-content-en.ts  (English)
//   - starter-content-content-zh.ts  (Simplified Chinese)
//
// JSON locales were considered, but ~600 lines of multi-paragraph markdown
// per language are unreadable as escaped single-line strings; keeping the
// content in TS lets reviewers see the rendered shape and catch markdown
// regressions in code review.
//
// Server-side concerns (batch creation, idempotency, assignee resolution)
// live in Go: handler/onboarding.go → ImportStarterContent.
// =============================================================================

export type StarterContentLocale = "en" | "zh-Hans";

// Prefix titles with 1. 2. 3. … AFTER the full list is assembled so
// conditional items (invite team / connect repo) don't break numbering.
function numberTitles(
  issues: ImportStarterIssuePayload[],
): ImportStarterIssuePayload[] {
  return issues.map((s, i) => ({ ...s, title: `${i + 1}. ${s.title}` }));
}

function pickContent(locale: StarterContentLocale) {
  return locale === "zh-Hans" ? zh : en;
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
  locale,
}: {
  workspaceId: string;
  userName: string;
  questionnaire: QuestionnaireAnswers;
  locale: StarterContentLocale;
}): ImportStarterContentPayload {
  const content = pickContent(locale);
  const welcome = content.buildWelcomeIssueText(questionnaire, userName);
  return {
    workspace_id: workspaceId,
    project: {
      title: content.PROJECT.title,
      description: content.PROJECT.description,
      icon: "👋",
    },
    welcome_issue_template: {
      title: welcome.title,
      description: welcome.description,
      priority: "high",
    },
    agent_guided_sub_issues: numberTitles(
      content.buildAgentGuidedSubIssues(questionnaire),
    ),
    self_serve_sub_issues: numberTitles(
      content.buildSelfServeSubIssues(questionnaire),
    ),
  };
}
