import { sourceSurveyDevFeature } from "../onboarding/dev-actions";
import { starPromptDevFeature } from "../star-prompt/dev-actions";
import { whatsNewDevFeature } from "../whats-new/dev-actions";
import { devCommands, type DevFeature } from "./actions";
import { errorsDevFeature, queryCacheDevFeature } from "./diagnostics-dev-actions";
import { sessionPromptDevFeature } from "./session-prompt-dev-actions";

/**
 * Every feature with dev actions, in panel order; groups appear in the order
 * their first feature does. A feature declares its actions in a
 * `dev-actions.ts` next to its own code and is listed here to show up.
 */
export const DEV_FEATURES: readonly DevFeature[] = [
  sessionPromptDevFeature,
  whatsNewDevFeature,
  starPromptDevFeature,
  sourceSurveyDevFeature,
  queryCacheDevFeature,
  errorsDevFeature,
];

export const DEV_COMMANDS = devCommands(DEV_FEATURES);

export const DEV_GROUPS = [...new Set(DEV_FEATURES.map((f) => f.group))];
