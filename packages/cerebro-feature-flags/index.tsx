import { createElement } from "react";
import { Flag } from "lucide-react";
import { CerebroFeatureFlagsTab } from "./settings-tab";

// This file is the package's public barrel and is intentionally NOT a
// `"use client"` module. The Next.js settings route is a server component
// and spreads `cerebroFeatureFlagTabs` into its props — if this file were
// `"use client"`, the array would arrive on the server as a client
// reference (not iterable). Hooks and the tab body live in their own
// `"use client"` files (`./api`, `./store`, `./settings-tab`) and are
// re-exported from here.

// Mirrors @multica/views ExtraSettingsTab. Defined locally to avoid the
// cerebro-feature-flags ↔ views dependency cycle that would otherwise force
// pnpm to topo-sort us into the same task graph as views/typecheck.
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

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
    content: createElement(CerebroFeatureFlagsTab),
  },
];
