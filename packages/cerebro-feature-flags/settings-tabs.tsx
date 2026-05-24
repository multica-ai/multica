import { createElement } from "react";
import { Flag } from "lucide-react";
import { CerebroFeatureFlagsTab } from "./settings-tab";

// Mirrors @multica/views ExtraSettingsTab. Defined locally to avoid the
// cerebro-feature-flags <-> views dependency cycle that would otherwise force
// pnpm to topo-sort us into the same task graph as views/typecheck.
interface ExtraSettingsTab {
  value: string;
  label: string;
  icon: React.ComponentType<{ className?: string }>;
  content: React.ReactNode;
}

/**
 * Tabs the platform can splice into `<SettingsPage extraAccountTabs={...}>`.
 * Keep this entrypoint free of client hook/store re-exports so server
 * components can safely spread the array during render.
 */
export const cerebroFeatureFlagTabs: ExtraSettingsTab[] = [
  {
    value: "cerebro-features",
    label: "Cerebro features",
    icon: Flag,
    content: createElement(CerebroFeatureFlagsTab),
  },
];
