import type { ComponentType } from "react";
import {
  BookOpenText,
  Brain,
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
  { id: "env", label: "Env", icon: KeyRound },
  { id: "infisical", label: "Infisical", icon: KeySquare },
  { id: "custom_args", label: "Custom args", icon: Terminal },
  { id: "sandbox", label: "Sandbox", icon: Shield },
  { id: "mcp_config", label: "MCP Config", icon: Plug },
  { id: "integrations", label: "Integrations", icon: Webhook },
  { id: "tools", label: "Tools", icon: Wrench },
  { id: "capabilities", label: "Capabilities", icon: ShieldCheck },
  { id: "memory", label: "Memory", icon: Brain },
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
