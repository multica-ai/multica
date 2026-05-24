import { createElement } from "react";
import { ShieldCheck } from "lucide-react";
import { AgentCapabilitiesSettingsTab } from "./settings-tab";

export { AgentCapabilitiesSettingsTab } from "./settings-tab";

export const agentCapabilitiesSettingsTab = {
  value: "agent-capabilities",
  label: "Agent capabilities",
  icon: ShieldCheck,
  content: createElement(AgentCapabilitiesSettingsTab),
};
