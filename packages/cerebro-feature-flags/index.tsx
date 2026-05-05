import { Flag } from "lucide-react";
import type { ExtraSettingsTab } from "@multica/views/settings";
import { CerebroFeatureFlagsTab } from "./settings-tab";

export {
  useFeatureFlag,
  useFeatureFlagsQuery,
  useSetFeatureFlagMutation,
} from "./api";
export {
  useCerebroFeatureFlagsStore,
  useFlagValue,
} from "./store";
export {
  CEREBRO_FLAG_DEFAULTS,
  CEREBRO_FLAGS,
} from "./registry";
export type { CerebroFlagKey, CerebroFlagDefinition } from "./registry";
export { CerebroFeatureFlagsTab } from "./settings-tab";

/**
 * Tabs the platform can splice into `<SettingsPage extraAccountTabs={...}>`.
 * Both the desktop and web shells consume this — see routes.tsx (desktop)
 * and the [workspaceSlug]/(dashboard)/settings page (web).
 */
export const cerebroFeatureFlagTabs: ExtraSettingsTab[] = [
  {
    value: "cerebro-features",
    label: "Cerebro features",
    icon: Flag,
    content: <CerebroFeatureFlagsTab />,
  },
];
