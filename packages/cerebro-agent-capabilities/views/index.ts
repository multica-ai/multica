import { createElement } from "react";
import { ShieldCheck } from "lucide-react";
import { AgentCapabilitiesSettingsTab } from "./settings-tab";

export { AgentCapabilitiesSettingsTab } from "./settings-tab";
// TECH-3642 — per-agent capabilities detail tab (skills/tools/credentials/limits).
export {
  createAgentCapabilitiesTabs,
  CerebroCapabilitiesTab,
  type AgentCapabilitiesTabExtension,
} from "./capabilities-tab";

export const agentCapabilitiesSettingsTab = {
  value: "agent-capabilities",
  label: "Agent capabilities",
  icon: ShieldCheck,
  content: createElement(AgentCapabilitiesSettingsTab),
};
