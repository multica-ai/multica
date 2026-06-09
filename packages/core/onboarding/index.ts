export type {
  OnboardingStep,
  OnboardingCompletionPath,
  QuestionnaireAnswers,
  Source,
  Role,
  TeamSize,
  UseCase,
} from "./types";
export {
  saveQuestionnaire,
  completeOnboarding,
  joinCloudWaitlist,
} from "./store";
export { ONBOARDING_STEP_ORDER } from "./step-order";
export {
  needsSourceBackfill,
  SOURCE_BACKFILL_MAX_DISMISSALS,
} from "./needs-backfill";
export { recommendTemplate, type AgentTemplateId } from "./recommend-template";
export {
  useWelcomeStore,
  type WelcomeSignal,
} from "./welcome-store";
