import en from "./en.json";
import ko from "./ko.json";
import zhHans from "./zh-Hans.json";

// Resource bundle for the cerebro-agent-avatar namespace. Consumed by the
// upstream RESOURCES aggregator in `packages/views/locales/index.ts` so the
// strings live in one place. Adding a locale: drop the JSON file and add an
// entry below; the keys are typed via the AI-generation namespace in
// `packages/views/i18n/resources-types.ts`.
export const CEREBRO_AGENT_AVATAR_RESOURCES = {
  en,
  ko,
  "zh-Hans": zhHans,
} as const;

export type CerebroAgentAvatarResources = typeof en;
