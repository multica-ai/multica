"use client";

import { useMemo } from "react";
import type { Agent, AgentRuntime, MemberWithUser } from "@multica/core/types";
import { providerSupportsMcpConfig } from "@multica/core/agents";
import { useFeatureEnabled } from "@multica/core/config";
import { COMPOSIO_MCP_APPS_FLAG } from "@multica/core/feature-flags";
import {
  Activity,
  AppWindow,
  Cloud,
  KeyRound,
  ListChecks,
  MessagesSquare,
  Plug,
  ScrollText,
  Server,
  Shield,
  SquareTerminal,
  UserPen,
  Workflow,
  type IconComponent,
} from "@multica/ui/icons";
import { SkillIcon } from "../../skills/lib/skill-icon";
import { InstructionsTab } from "./tabs/instructions-tab";
import { SkillsTab } from "./tabs/skills-tab";
import { EnvTab } from "./tabs/env-tab";
import { CustomArgsTab } from "./tabs/custom-args-tab";
import { McpConfigTab } from "./tabs/mcp-config-tab";
import { AgentAppsTab } from "./tabs/agent-apps-tab";
import { QoderAgentTab } from "./tabs/qoder-agent-tab";
import { RuntimeConfigTab } from "./tabs/runtime-config-tab";
import { AgentExecutionSettings, AgentProfileSettings } from "./agent-detail-inspector";
import { AgentAccessSettings } from "./agent-access-settings";
import { AgentChannelsSection } from "./agent-channels-section";

/**
 * The sections of the agent page, by their `?view=` id. `general` is the
 * profile; it keeps the id every "Settings" link already points at.
 */
export type AgentSection =
  | "overview"
  | "work"
  | "general"
  | "instructions"
  | "skills"
  | "mcp_config"
  | "composio_mcp"
  | "channels"
  | "access"
  | "runtime"
  | "runtime_config"
  | "env"
  | "custom_args";

type SectionLabelKey =
  | "overview"
  | "work"
  | "profile"
  | "instructions"
  | "skills"
  | "mcp_config"
  | "composio_mcp"
  | "channels"
  | "access"
  | "runtime"
  | "runtime_config"
  | "qoder_agent"
  | "environment"
  | "custom_args";

export interface AgentSectionDef {
  id: AgentSection;
  labelKey: SectionLabelKey;
  icon: IconComponent;
}

export interface AgentSectionGroup {
  key: "main" | "define" | "connect" | "run";
  items: AgentSectionDef[];
}

const SECTIONS = {
  overview: { id: "overview", labelKey: "overview", icon: Activity },
  work: { id: "work", labelKey: "work", icon: ListChecks },
  general: { id: "general", labelKey: "profile", icon: UserPen },
  instructions: { id: "instructions", labelKey: "instructions", icon: ScrollText },
  skills: { id: "skills", labelKey: "skills", icon: SkillIcon },
  mcp_config: { id: "mcp_config", labelKey: "mcp_config", icon: Plug },
  composio_mcp: { id: "composio_mcp", labelKey: "composio_mcp", icon: AppWindow },
  channels: { id: "channels", labelKey: "channels", icon: MessagesSquare },
  access: { id: "access", labelKey: "access", icon: Shield },
  runtime: { id: "runtime", labelKey: "runtime", icon: Server },
  runtime_config: { id: "runtime_config", labelKey: "runtime_config", icon: Workflow },
  env: { id: "env", labelKey: "environment", icon: KeyRound },
  custom_args: { id: "custom_args", labelKey: "custom_args", icon: SquareTerminal },
} satisfies Record<AgentSection, AgentSectionDef>;

/** Sections laid out beside a preview, wider than a form column. */
export const WIDE_AGENT_SECTIONS: ReadonlySet<AgentSection> = new Set(["general"]);

/**
 * The sections this agent offers, grouped as the page's nav shows them.
 * Sections the agent's runtime cannot use are left out.
 */
export function useAgentSectionGroups({
  runtime,
  canEdit,
}: {
  runtime: AgentRuntime | null;
  canEdit: boolean;
}): AgentSectionGroup[] {
  const composioMCPAppsEnabled = useFeatureEnabled(
    COMPOSIO_MCP_APPS_FLAG,
    false,
  );
  const provider = runtime?.provider;

  return useMemo(() => {
    const qoderRuntime = provider === "qoder_cloud";
    // Qoder agents take their skills and tools from Qoder Cloud Agents.
    const define: AgentSectionDef[] = [SECTIONS.general, SECTIONS.instructions];
    if (!qoderRuntime) {
      define.push(SECTIONS.skills);
      // A runtime row that hasn't loaded yet keeps MCP rather than
      // flickering it off and back on.
      if (!provider || providerSupportsMcpConfig(provider)) {
        define.push(SECTIONS.mcp_config);
      }
      if (composioMCPAppsEnabled && canEdit) define.push(SECTIONS.composio_mcp);
    }

    const run: AgentSectionDef[] = [SECTIONS.runtime];
    if (qoderRuntime) {
      run.push({ ...SECTIONS.runtime_config, labelKey: "qoder_agent", icon: Cloud });
    } else {
      if (provider === "openclaw") run.push(SECTIONS.runtime_config);
      // Env is the only section backed by a secret-bearing endpoint.
      // GET/PUT /api/agents/{id}/env admits the agent owner or a workspace
      // owner/admin (MUL-5438) — the same rule `canEdit` encodes — so
      // showing it to anyone else guarantees a 403 on "Reveal & edit".
      // The server stays the boundary; this only removes a dead entry point.
      if (canEdit) run.push(SECTIONS.env);
      run.push(SECTIONS.custom_args);
    }

    return [
      { key: "main", items: [SECTIONS.overview, SECTIONS.work] },
      { key: "define", items: define },
      { key: "connect", items: [SECTIONS.channels, SECTIONS.access] },
      { key: "run", items: run },
    ];
  }, [canEdit, composioMCPAppsEnabled, provider]);
}

/** The section a `?view=` names, or Overview when this agent doesn't offer it. */
export function resolveAgentSection(
  view: string | null,
  groups: AgentSectionGroup[],
): AgentSectionDef {
  for (const group of groups) {
    const match = group.items.find((item) => item.id === view);
    if (match) return match;
  }
  return SECTIONS.overview;
}

interface AgentSectionContentProps {
  section: Exclude<AgentSection, "overview" | "work">;
  agent: Agent;
  runtime: AgentRuntime | null;
  runtimes: AgentRuntime[];
  members: MemberWithUser[];
  currentUserId: string | null;
  canEdit: boolean;
  onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
  /** Reports unsaved edits, so leaving the section can ask first. */
  onDirtyChange: (dirty: boolean) => void;
}

/** The editor for one settings section of the agent page. */
export function AgentSectionContent({
  section,
  agent,
  runtime,
  runtimes,
  members,
  currentUserId,
  canEdit,
  onUpdate,
  onDirtyChange,
}: AgentSectionContentProps) {
  const save = (updates: Record<string, unknown>) => onUpdate(agent.id, updates);

  switch (section) {
    case "general":
      return <AgentProfileSettings agent={agent} canEdit={canEdit} onUpdate={onUpdate} />;
    case "instructions":
      return (
        <InstructionsTab agent={agent} onSave={save} onDirtyChange={onDirtyChange} />
      );
    case "skills":
      return (
        <SkillsTab
          agent={agent}
          runtime={runtime}
          currentUserId={currentUserId}
          canEdit={canEdit}
        />
      );
    case "mcp_config":
      return (
        <McpConfigTab
          agent={agent}
          runtime={runtime}
          currentUserId={currentUserId}
          canEdit={canEdit}
          onSave={save}
          onDirtyChange={onDirtyChange}
        />
      );
    case "composio_mcp":
      return <AgentAppsTab agent={agent} />;
    case "channels":
      return <AgentChannelsSection agent={agent} />;
    case "access":
      return (
        <AgentAccessSettings
          agent={agent}
          members={members}
          currentUserId={currentUserId}
          onDirtyChange={onDirtyChange}
          onUpdate={onUpdate}
        />
      );
    case "runtime":
      return (
        <AgentExecutionSettings
          agent={agent}
          runtime={runtime}
          runtimes={runtimes}
          members={members}
          currentUserId={currentUserId}
          canEdit={canEdit}
          onUpdate={onUpdate}
        />
      );
    case "runtime_config":
      return runtime?.provider === "qoder_cloud" ? (
        <QoderAgentTab
          canEdit={canEdit}
          key={agent.id}
          agent={agent}
          onSave={save}
          onDirtyChange={onDirtyChange}
        />
      ) : (
        <RuntimeConfigTab agent={agent} onSave={save} onDirtyChange={onDirtyChange} />
      );
    case "env":
      return <EnvTab agent={agent} onDirtyChange={onDirtyChange} />;
    case "custom_args":
      return (
        <CustomArgsTab
          agent={agent}
          runtimeDevice={runtime ?? undefined}
          onSave={save}
          onDirtyChange={onDirtyChange}
        />
      );
  }
}
