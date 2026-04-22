export type OnboardingStep =
  | "welcome"
  | "questionnaire"
  | "workspace"
  | "runtime"
  | "agent"
  | "first_issue";

export type TeamSize = "solo" | "team" | "other";

export type Role =
  | "developer"
  | "product_lead"
  | "writer"
  | "founder"
  | "other";

export type UseCase =
  | "coding"
  | "planning"
  | "writing_research"
  | "explore"
  | "other";

export interface QuestionnaireAnswers {
  team_size: TeamSize | null;
  team_size_other: string | null;
  role: Role | null;
  role_other: string | null;
  use_case: UseCase | null;
  use_case_other: string | null;
}
