import { createElement } from "react";
import { Gauge } from "lucide-react";
import { CostOptimizationTabs } from "./cost-optimization-tabs";

// Mirrors @multica/views ExtraSettingsTab. Defined locally so this entrypoint
// stays free of a views dependency (and the topo-sort coupling it brings),
// exactly like cerebro-feature-flags/settings-tabs.
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
export const cerebroCostOptimizationTabs: ExtraSettingsTab[] = [
  {
    value: "cost-optimization",
    label: "Cost optimization",
    icon: Gauge,
    content: createElement(CostOptimizationTabs),
  },
];
