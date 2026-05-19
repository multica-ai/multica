import type { QuestionnaireAnswers, Role, UseCase } from "./types";

/**
 * Identifier for the four agent templates offered during onboarding
 * Step 6 (Agent). Keep in sync with the template registry inside
 * StepAgent in `packages/views/onboarding/steps/step-agent.tsx`.
 */
export type AgentTemplateId = "coding" | "planning" | "writing" | "assistant";

/**
 * Pick a recommended agent template based on the v2 questionnaire
 * (role × use_case). Role is the primary signal; use_case is a
 * tiebreaker for roles that legitimately split between templates
 * (engineer / product / marketing).
 *
 * Fallback chain when role is skipped or null:
 *   1. Derive from use_case alone.
 *   2. Both unknown → `assistant` (the generic default).
 *
 * Pure / deterministic — safe to call on every render.
 */
export function recommendTemplate(
  answers: Pick<QuestionnaireAnswers, "role" | "use_case">,
): AgentTemplateId {
  const role: Role | null = answers.role;
  const useCase: UseCase | null = answers.use_case;

  if (role === null) return fallbackFromUseCase(useCase);

  switch (role) {
    case "engineer":
      if (useCase === "manage_team" || useCase === "plan_research")
        return "planning";
      if (useCase === "write_publish") return "writing";
      return "coding";
    case "product":
      if (useCase === "ship_code") return "coding";
      return "planning";
    case "designer":
      return "assistant";
    case "writer":
      return "writing";
    case "marketing":
      if (useCase === "write_publish" || useCase === "plan_research")
        return "writing";
      return "planning";
    case "research":
      return "planning";
    case "founder":
    case "ops":
    case "student":
    case "other":
      return "assistant";
  }
}

function fallbackFromUseCase(useCase: UseCase | null): AgentTemplateId {
  switch (useCase) {
    case "ship_code":
      return "coding";
    case "write_publish":
      return "writing";
    case "manage_team":
    case "plan_research":
      return "planning";
    default:
      return "assistant";
  }
}
