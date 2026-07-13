import type { ComponentType } from "react";
import {
  BookOpenText,
  Brain,
  Cog,
  FileText,
  KeyRound,
  KeySquare,
  ListTodo,
  Plug,
  Shield,
  ShieldCheck,
  Terminal,
  Webhook,
  Wrench,
} from "lucide-react";

export type RedesignTab =
  | "tasks"
  | "instructions"
  | "skills"
  | "advanced"
  | "env"
  | "infisical"
  | "custom_args"
  | "sandbox"
  | "mcp_config"
  | "integrations"
  | "tools"
  | "capabilities"
  | "memory";

export interface AgentPageTab {
  id: RedesignTab;
  label: string;
  icon: ComponentType<{ className?: string }>;
}

const BASE_TABS: AgentPageTab[] = [
  { id: "tasks", label: "Tasks", icon: ListTodo },
  { id: "instructions", label: "Instructions", icon: FileText },
  { id: "skills", label: "Skills", icon: BookOpenText },
  { id: "advanced", label: "Advanced", icon: Cog },
  { id: "integrations", label: "Integrations", icon: Webhook },
  { id: "tools", label: "Tools", icon: Wrench },
  { id: "capabilities", label: "Capabilities", icon: ShieldCheck },
  { id: "memory", label: "Memory", icon: Brain },
];

const ADVANCED_TABS: AgentPageTab[] = [
  { id: "infisical", label: "Infisical secrets", icon: KeySquare },
  { id: "sandbox", label: "Sandbox", icon: Shield },
  { id: "mcp_config", label: "MCP Config", icon: Plug },
  { id: "custom_args", label: "Custom args", icon: Terminal },
  { id: "env", label: "Env", icon: KeyRound },
];

export function agentPageTabs(options: {
  mcpConfig: boolean;
  integrations: boolean;
}): AgentPageTab[] {
  return BASE_TABS.filter((tab) => {
    if (tab.id === "mcp_config") return options.mcpConfig;
    if (tab.id === "integrations") return options.integrations;
    return true;
  });
}

export function agentPageTabIds(options: {
  mcpConfig: boolean;
  integrations: boolean;
}): RedesignTab[] {
  return agentPageTabs(options).map((tab) => tab.id);
}

export function advancedTabs(options: { mcpConfig: boolean }): AgentPageTab[] {
  return ADVANCED_TABS.filter((tab) => tab.id !== "mcp_config" || options.mcpConfig);
}

export function advancedTabIds(options: { mcpConfig: boolean }): RedesignTab[] {
  return advancedTabs(options).map((tab) => tab.id);
}
